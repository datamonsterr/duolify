// Package switchover moves the API and worker to a second Postgres without request errors.
//
// One Gate wraps the single pgxpool.Pool the process shares. Pool hooks decide who may check out a
// connection, so no call site changes; the same *pgxpool.Pool keeps serving after the swap because new
// connections are dialled to whichever database the gate currently points at.
package switchover

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// State is the gate's mode.
type State string

// Gate states. Switching covers every transition that holds new work.
const (
	StateOpen      State = "open"
	StateStandby   State = "standby"
	StateSwitching State = "switching"
	StateReadOnly  State = "readonly"
)

// ErrState reports an operation that is not allowed in the gate's current state.
var ErrState = errors.New("switchover: operation not allowed in this state")

// Queue is a background job runner the gate stops and restarts; *river.Client satisfies it.
type Queue = interface {
	Start(context.Context) error
	Stop(context.Context) error
}

// Options configures a Gate. Zero values take the documented defaults.
type Options struct {
	SourceURL    string // database the pool starts on
	TargetURL    string // database to switch to; empty disables Switch
	Subscription string // logical replication slot/subscription name (default duolify_switchover)
	// SequenceMargin is added to every publisher sequence value on the new database (default 1000).
	SequenceMargin int64
	// HoldMax bounds one transition: new work is held at most this long (default 15 s).
	HoldMax time.Duration
	// LagTimeout bounds the wait for the subscriber to reach the publisher LSN (default 10 s).
	LagTimeout time.Duration
	// Standby starts the gate closed with queues off until Open.
	Standby bool
	Logger  *slog.Logger
}

type hold int

const (
	holdNone   hold = iota
	holdDrain       // new requests wait; in-flight requests and background callers continue
	holdClosed      // only callers already holding a connection continue
)

// ticket follows one request through its context.
type ticket struct {
	bypass  bool
	holding int // connections this request currently has checked out
}

type ticketKey struct{}

type queued struct {
	q   Queue
	ctx context.Context
}

// Gate holds, drains and redirects database work. Create it with New.
type Gate struct {
	opts   Options
	log    *slog.Logger
	repl   replicator
	target *pgx.ConnConfig

	mu       sync.Mutex
	state    State
	hold     hold
	wake     chan struct{}
	conns    map[*pgx.Conn]*ticket
	waiting  int // requests parked in prepare; they hold a pool slot but no work
	queues   []queued
	queuesOn bool
	active   *pgx.ConnConfig
	readOnly bool // new pool connections open read-only (plan B)
	switched bool
	pool     *pgxpool.Pool
}

// New validates both URLs. A standby gate starts closed.
func New(opts Options) (*Gate, error) {
	if opts.Subscription == "" {
		opts.Subscription = "duolify_switchover"
	}
	if opts.SequenceMargin <= 0 {
		opts.SequenceMargin = 1000
	}
	if opts.HoldMax <= 0 {
		opts.HoldMax = 15 * time.Second
	}
	if opts.LagTimeout <= 0 {
		opts.LagTimeout = 10 * time.Second
	}
	source, err := parse(opts.SourceURL)
	if err != nil {
		return nil, fmt.Errorf("switchover source: %w", err)
	}
	g := &Gate{opts: opts, log: opts.Logger, active: source, wake: make(chan struct{}), conns: map[*pgx.Conn]*ticket{}, state: StateOpen, queuesOn: !opts.Standby}
	if g.log == nil {
		g.log = slog.Default()
	}
	if opts.TargetURL != "" {
		if g.target, err = parse(opts.TargetURL); err != nil {
			return nil, fmt.Errorf("switchover target: %w", err)
		}
		g.repl = &pgReplicator{source: opts.SourceURL, target: opts.TargetURL, subscription: opts.Subscription, margin: opts.SequenceMargin}
	}
	if opts.Standby {
		g.state, g.hold = StateStandby, holdClosed
	}
	return g, nil
}

func parse(url string) (*pgx.ConnConfig, error) {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return nil, errors.New("invalid database URL")
	}
	cfg.ConnectTimeout = 3 * time.Second
	return cfg, nil
}

// State returns the current mode.
func (g *Gate) State() State { g.mu.Lock(); defer g.mu.Unlock(); return g.state }

// Switched reports whether the pool has moved to the target database.
func (g *Gate) Switched() bool { g.mu.Lock(); defer g.mu.Unlock(); return g.switched }

// Attach gives the gate the pool whose connections it resets on every transition.
func (g *Gate) Attach(pool *pgxpool.Pool) { g.mu.Lock(); g.pool = pool; g.mu.Unlock() }

// PoolConfig installs the gate's hooks. Pass it to store.New before the pool is created.
func (g *Gate) PoolConfig(cfg *pgxpool.Config) {
	cfg.ConnConfig.Tracer = g
	cfg.PrepareConn = g.prepare
	cfg.BeforeConnect = func(_ context.Context, cc *pgx.ConnConfig) error {
		g.mu.Lock()
		defer g.mu.Unlock()
		cc.Config = g.active.Config
		cc.RuntimeParams = map[string]string{}
		for k, v := range g.active.RuntimeParams {
			cc.RuntimeParams[k] = v
		}
		if g.readOnly {
			cc.RuntimeParams["default_transaction_read_only"] = "on"
		}
		return nil
	}
}

// Ping runs a readiness probe that is never held, so a standby stack can report ready.
func (g *Gate) Ping(ctx context.Context, ping func(context.Context) error) error {
	return ping(Bypass(ctx))
}

// Bypass marks work that must never be held (operator endpoints, probes).
func Bypass(ctx context.Context) context.Context {
	return context.WithValue(ctx, ticketKey{}, &ticket{bypass: true})
}

// Admit stamps a request so the gate can tell new work from in-flight work.
func (g *Gate) Admit(ctx context.Context) context.Context {
	if _, ok := ctx.Value(ticketKey{}).(*ticket); ok {
		return ctx
	}
	return context.WithValue(ctx, ticketKey{}, &ticket{})
}

func (g *Gate) allowedLocked(t *ticket) bool {
	switch {
	case g.hold == holdNone, t != nil && (t.bypass || t.holding > 0):
		return true
	case g.state == StateStandby && t == nil:
		return true // process start-up and background loops must run so the stack can listen and be opened
	case g.hold == holdDrain:
		return t == nil // background callers (queue workers) finish; stamped requests are new work
	}
	return false
}

func connKey(cfg *pgx.ConnConfig) string {
	return fmt.Sprintf("%s:%d/%s/%s/%s", cfg.Host, cfg.Port, cfg.Database, cfg.User, cfg.RuntimeParams["default_transaction_read_only"])
}

func (g *Gate) activeKeyLocked() string {
	cfg := *g.active
	cfg.RuntimeParams = map[string]string{}
	if g.readOnly {
		cfg.RuntimeParams["default_transaction_read_only"] = "on"
	}
	return connKey(&cfg)
}

// wait blocks while the gate holds the caller's work.
func (g *Gate) wait(ctx context.Context) error {
	t, _ := ctx.Value(ticketKey{}).(*ticket)
	for {
		g.mu.Lock()
		if g.allowedLocked(t) {
			g.mu.Unlock()
			return nil
		}
		ch := g.wake
		g.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// prepare is the pgxpool PrepareConn hook: hold new work, drop connections to the wrong database.
func (g *Gate) prepare(ctx context.Context, conn *pgx.Conn) (bool, error) {
	t, _ := ctx.Value(ticketKey{}).(*ticket)
	waited := false
	for {
		g.mu.Lock()
		if g.allowedLocked(t) {
			if connKey(conn.Config()) != g.activeKeyLocked() {
				g.mu.Unlock()
				return false, nil // dialled before a swap: destroy, the pool dials the current database
			}
			g.conns[conn] = t
			if t != nil {
				t.holding++
			}
			g.mu.Unlock()
			break
		}
		ch := g.wake
		g.waiting++
		g.mu.Unlock()
		waited = true
		select {
		case <-ch:
		case <-ctx.Done():
		}
		g.mu.Lock()
		g.waiting--
		g.mu.Unlock()
		if ctx.Err() != nil {
			return true, ctx.Err()
		}
	}
	if waited {
		// Sessions may have been terminated while the request was held.
		if err := conn.Ping(ctx); err != nil {
			g.forget(conn)
			return false, nil
		}
	}
	return true, nil
}

func (g *Gate) forget(conn *pgx.Conn) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if t, ok := g.conns[conn]; ok {
		delete(g.conns, conn)
		if t != nil {
			t.holding--
		}
	}
}

// TraceQueryStart and TraceQueryEnd satisfy pgx.QueryTracer; the gate only needs TraceRelease.
func (g *Gate) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return ctx
}

// TraceQueryEnd is a no-op.
func (g *Gate) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// TraceRelease forgets a connection when its holder gives it back.
func (g *Gate) TraceRelease(_ *pgxpool.Pool, data pgxpool.TraceReleaseData) { g.forget(data.Conn) }

func (g *Gate) setLocked(state State, h hold) {
	g.state, g.hold = state, h
	close(g.wake)
	g.wake = make(chan struct{})
}

func (g *Gate) setHold(h hold) {
	g.mu.Lock()
	g.hold = h
	close(g.wake)
	g.wake = make(chan struct{})
	g.mu.Unlock()
}

// waitIdle returns once no request holds a pooled connection (transactions and session locks included).
func (g *Gate) waitIdle(ctx context.Context) error {
	for {
		// The pool's own count, not ours: queue drivers hijack connections and never release them.
		g.mu.Lock()
		n := len(g.conns)
		if g.pool != nil {
			n = int(g.pool.Stat().AcquiredConns()) - g.waiting
		}
		g.mu.Unlock()
		if n <= 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for %d in-flight connections: %w", n, ctx.Err())
		case <-time.After(2 * time.Millisecond):
		}
	}
}

func (g *Gate) reset() {
	g.mu.Lock()
	pool := g.pool
	g.mu.Unlock()
	if pool != nil {
		pool.Reset()
	}
}

// Start registers a queue and starts it unless the gate keeps queues off (standby, read-only).
func (g *Gate) Start(ctx context.Context, q Queue) error {
	g.mu.Lock()
	g.queues = append(g.queues, queued{q, ctx})
	on := g.queuesOn
	g.mu.Unlock()
	if !on {
		return nil
	}
	return q.Start(ctx)
}

func (g *Gate) stopQueues(ctx context.Context) error {
	g.mu.Lock()
	g.queuesOn = false
	queues := append([]queued(nil), g.queues...)
	g.mu.Unlock()
	var errs []error
	for _, item := range queues {
		if err := item.q.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("soft-stop queue: %w", err))
		}
	}
	return errors.Join(errs...)
}

func (g *Gate) startQueues(ctx context.Context) error {
	// Queue clients ping the pool on start; background callers pass a draining gate, stamped requests stay held.
	g.setHold(holdDrain)
	g.mu.Lock()
	g.queuesOn = true
	queues := append([]queued(nil), g.queues...)
	g.mu.Unlock()
	var errs []error
	for _, item := range queues {
		// A client whose soft stop timed out is still draining; let it finish first.
		if s, ok := item.q.(interface{ Stopped() <-chan struct{} }); ok {
			if ch := s.Stopped(); ch != nil {
				select {
				case <-ch:
				case <-ctx.Done():
				}
			}
		}
		if err := item.q.Start(item.ctx); err != nil {
			errs = append(errs, fmt.Errorf("restart queue: %w", err))
		}
	}
	return errors.Join(errs...)
}

func (g *Gate) begin(allowed State) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state != allowed {
		return fmt.Errorf("%w: gate is %s", ErrState, g.state)
	}
	g.setLocked(StateSwitching, holdDrain)
	return nil
}

func (g *Gate) finish(state State) {
	g.mu.Lock()
	g.setLocked(state, holdNone)
	g.mu.Unlock()
}

// quiesce holds new work, soft-stops queues and waits for in-flight connections to be returned.
func (g *Gate) quiesce(ctx context.Context) error {
	if err := g.stopQueues(ctx); err != nil {
		return err
	}
	g.setHold(holdClosed)
	return g.waitIdle(ctx)
}

// Switch runs the cutover: hold new work, drain, soft-stop queues, set the old database read-only,
// wait for the subscriber, sync sequences, swap the pool, restart queues and release. Any failed check
// releases back to the old database with read-only cleared.
func (g *Gate) Switch(ctx context.Context) error {
	if g.repl == nil {
		return fmt.Errorf("%w: no target database configured", ErrState)
	}
	g.mu.Lock()
	already := g.switched
	g.mu.Unlock()
	if already {
		return fmt.Errorf("%w: already switched", ErrState)
	}
	if err := g.begin(StateOpen); err != nil {
		return err
	}
	started := time.Now()
	gateCtx, cancel := context.WithTimeout(ctx, g.opts.HoldMax)
	defer cancel()
	readOnlySet := false
	err := func() error {
		if err := g.quiesce(gateCtx); err != nil {
			return err
		}
		g.reset()
		lsn, err := g.repl.SetReadOnly(gateCtx)
		readOnlySet = true
		if err != nil {
			return fmt.Errorf("set old database read-only: %w", err)
		}
		lagCtx, lagCancel := context.WithTimeout(gateCtx, g.opts.LagTimeout)
		defer lagCancel()
		if err = g.repl.WaitLSN(lagCtx, lsn); err != nil {
			return fmt.Errorf("replication lag: %w", err)
		}
		if err = g.repl.SyncSequences(gateCtx); err != nil {
			return fmt.Errorf("sync sequences: %w", err)
		}
		return nil
	}()
	recovery, recoverCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer recoverCancel()
	if err != nil {
		if readOnlySet {
			if clearErr := g.repl.ClearReadOnly(recovery); clearErr != nil {
				err = errors.Join(err, fmt.Errorf("clear read-only: %w", clearErr))
			}
		}
		g.reset()
		if startErr := g.startQueues(recovery); startErr != nil {
			err = errors.Join(err, startErr)
		}
		g.finish(StateOpen)
		g.log.Warn("database switchover aborted; serving from the old database", "error", err, "held_for", time.Since(started))
		return fmt.Errorf("switchover aborted: %w", err)
	}
	g.mu.Lock()
	g.active, g.switched = g.target, true
	g.mu.Unlock()
	g.reset()
	startErr := g.startQueues(recovery)
	g.finish(StateOpen)
	g.log.Info("database switchover complete", "held_for", time.Since(started))
	return startErr
}

// Open leaves standby: the stack's database is the live one, so start queues and release held work.
func (g *Gate) Open(ctx context.Context) error {
	g.mu.Lock()
	if g.state != StateStandby {
		defer g.mu.Unlock()
		return fmt.Errorf("%w: gate is %s", ErrState, g.state)
	}
	pool := g.pool
	g.mu.Unlock()
	if pool != nil {
		var recovering bool
		if err := pool.QueryRow(Bypass(ctx), "SELECT pg_is_in_recovery()").Scan(&recovering); err != nil {
			return fmt.Errorf("check database: %w", err)
		}
		if recovering {
			return errors.New("switchover: database is a replica, not writable")
		}
	}
	startErr := g.startQueues(ctx)
	g.finish(StateOpen)
	return startErr
}

// EnterReadOnly is plan B: reads keep working, writes get 503 + Retry-After and queues stop.
func (g *Gate) EnterReadOnly(ctx context.Context) error {
	if err := g.begin(StateOpen); err != nil {
		return err
	}
	gateCtx, cancel := context.WithTimeout(ctx, g.opts.HoldMax)
	defer cancel()
	if err := g.quiesce(gateCtx); err != nil {
		recovery, recoverCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer recoverCancel()
		err = errors.Join(err, g.startQueues(recovery))
		g.finish(StateOpen)
		return fmt.Errorf("read-only mode aborted: %w", err)
	}
	g.mu.Lock()
	g.readOnly = true
	g.mu.Unlock()
	g.reset()
	g.finish(StateReadOnly)
	return nil
}

// LeaveReadOnly resumes writes and restarts queues.
func (g *Gate) LeaveReadOnly(ctx context.Context) error {
	g.mu.Lock()
	if g.state != StateReadOnly {
		defer g.mu.Unlock()
		return fmt.Errorf("%w: gate is %s", ErrState, g.state)
	}
	g.readOnly = false
	g.mu.Unlock()
	g.reset()
	startErr := g.startQueues(ctx)
	g.finish(StateOpen)
	return startErr
}

// FromEnv builds a Gate from SWITCHOVER_* variables; the pool starts on databaseURL.
func FromEnv(getenv func(string) string, databaseURL string, logger *slog.Logger) (*Gate, error) {
	opts := Options{SourceURL: databaseURL, TargetURL: getenv("SWITCHOVER_TARGET_DATABASE_URL"), Subscription: getenv("SWITCHOVER_SUBSCRIPTION"), Logger: logger}
	switch mode := getenv("SWITCHOVER_MODE"); mode {
	case "", "off":
	case "standby":
		opts.Standby = true
	default:
		return nil, fmt.Errorf("SWITCHOVER_MODE must be empty or standby")
	}
	if v := getenv("SWITCHOVER_SEQUENCE_MARGIN"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return nil, errors.New("SWITCHOVER_SEQUENCE_MARGIN must be a non-negative integer")
		}
		opts.SequenceMargin = n
	}
	for name, dst := range map[string]*time.Duration{"SWITCHOVER_HOLD_MAX": &opts.HoldMax, "SWITCHOVER_LAG_TIMEOUT": &opts.LagTimeout} {
		if v := getenv(name); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil || d <= 0 {
				return nil, fmt.Errorf("%s must be a positive duration", name)
			}
			*dst = d
		}
	}
	if opts.HoldMax > 15*time.Second {
		return nil, errors.New("SWITCHOVER_HOLD_MAX must not exceed 15s")
	}
	return New(opts)
}
