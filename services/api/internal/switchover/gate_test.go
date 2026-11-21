package switchover

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeRepl struct {
	mu      sync.Mutex
	calls   []string
	waitLSN func(context.Context) error
	onSync  func() error
}

func (f *fakeRepl) record(s string) { f.mu.Lock(); f.calls = append(f.calls, s); f.mu.Unlock() }
func (f *fakeRepl) log() []string   { f.mu.Lock(); defer f.mu.Unlock(); return slices.Clone(f.calls) }
func (f *fakeRepl) SetReadOnly(context.Context) (string, error) {
	f.record("readonly")
	return "0/10", nil
}
func (f *fakeRepl) ClearReadOnly(context.Context) error { f.record("clear"); return nil }
func (f *fakeRepl) WaitLSN(ctx context.Context, lsn string) error {
	f.record("wait " + lsn)
	if f.waitLSN != nil {
		return f.waitLSN(ctx)
	}
	return nil
}
func (f *fakeRepl) SyncSequences(context.Context) error {
	f.record("sync")
	if f.onSync != nil {
		return f.onSync()
	}
	return nil
}

type fakeQueue struct {
	mu      sync.Mutex
	running bool
	events  []string
}

func (q *fakeQueue) Start(context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.running = true
	q.events = append(q.events, "start")
	return nil
}
func (q *fakeQueue) Stop(context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.running = false
	q.events = append(q.events, "stop")
	return nil
}
func (q *fakeQueue) isRunning() bool { q.mu.Lock(); defer q.mu.Unlock(); return q.running }

func newGate(t *testing.T, opts Options, repl *fakeRepl) *Gate {
	t.Helper()
	opts.SourceURL = "postgres://u:p@old:5432/app"
	opts.TargetURL = "postgres://u:p@new:5432/app"
	g, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	g.repl = repl
	return g
}

func TestSwitchStopsQueuesSyncsAndSwapsPool(t *testing.T) {
	repl := &fakeRepl{}
	g := newGate(t, Options{HoldMax: 2 * time.Second}, repl)
	q := &fakeQueue{}
	if err := g.Start(t.Context(), q); err != nil || !q.isRunning() {
		t.Fatalf("start: %v running=%v", err, q.isRunning())
	}
	repl.onSync = func() error {
		if q.isRunning() {
			t.Error("queue must be stopped before sequences sync")
		}
		return nil
	}
	if err := g.Switch(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := repl.log(); !slices.Equal(got, []string{"readonly", "wait 0/10", "sync"}) {
		t.Fatalf("steps = %v", got)
	}
	if !g.Switched() || g.State() != StateOpen || !q.isRunning() {
		t.Fatalf("switched=%v state=%s running=%v", g.Switched(), g.State(), q.isRunning())
	}
	if err := g.Switch(t.Context()); !errors.Is(err, ErrState) {
		t.Fatalf("second switch = %v, want ErrState", err)
	}
}

func TestSwitchFailureReleasesBackToOldDatabase(t *testing.T) {
	repl := &fakeRepl{waitLSN: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}
	g := newGate(t, Options{HoldMax: 2 * time.Second, LagTimeout: 50 * time.Millisecond}, repl)
	q := &fakeQueue{}
	_ = g.Start(t.Context(), q)
	started := time.Now()
	err := g.Switch(t.Context())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("lag timeout ignored: %v", time.Since(started))
	}
	if got := repl.log(); got[len(got)-1] != "clear" {
		t.Fatalf("read-only not cleared: %v", got)
	}
	if g.Switched() || g.State() != StateOpen || !q.isRunning() {
		t.Fatalf("not released to old pool: switched=%v state=%s running=%v", g.Switched(), g.State(), q.isRunning())
	}
	if err := g.wait(t.Context()); err != nil {
		t.Fatalf("requests still held: %v", err)
	}
}

func TestSwitchIsBoundedByHoldMax(t *testing.T) {
	repl := &fakeRepl{waitLSN: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}
	g := newGate(t, Options{HoldMax: 150 * time.Millisecond, LagTimeout: time.Minute}, repl)
	started := time.Now()
	if err := g.Switch(t.Context()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(started); d > time.Second {
		t.Fatalf("hold lasted %v", d)
	}
}

func TestNewWorkIsHeldUntilRelease(t *testing.T) {
	repl := &fakeRepl{}
	release := make(chan struct{})
	repl.waitLSN = func(context.Context) error { <-release; return nil }
	g := newGate(t, Options{HoldMax: 5 * time.Second}, repl)
	done := make(chan error, 1)
	go func() { done <- g.Switch(t.Context()) }()
	for g.State() != StateSwitching {
		time.Sleep(time.Millisecond)
	}
	// A request admitted after the gate closed has no connection yet and must wait.
	waited := make(chan error, 1)
	go func() { waited <- g.wait(g.Admit(t.Context())) }()
	select {
	case err := <-waited:
		t.Fatalf("held request ran early: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-waited; err != nil {
		t.Fatalf("held request failed: %v", err)
	}
}

func TestHeldRequestHonoursItsOwnContext(t *testing.T) {
	g := newGate(t, Options{Standby: true}, &fakeRepl{})
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if err := g.wait(g.Admit(ctx)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}

func TestStandbyStartsClosedWithQueuesOffAndOpensOnTrigger(t *testing.T) {
	g := newGate(t, Options{Standby: true}, &fakeRepl{})
	q := &fakeQueue{}
	if err := g.Start(t.Context(), q); err != nil {
		t.Fatal(err)
	}
	if g.State() != StateStandby || q.isRunning() {
		t.Fatalf("state=%s running=%v", g.State(), q.isRunning())
	}
	if err := g.Switch(t.Context()); !errors.Is(err, ErrState) {
		t.Fatalf("switch in standby = %v", err)
	}
	waited := make(chan error, 1)
	go func() { waited <- g.wait(g.Admit(t.Context())) }()
	select {
	case <-waited:
		t.Fatal("standby released a request")
	case <-time.After(30 * time.Millisecond):
	}
	if err := g.Open(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-waited; err != nil || g.State() != StateOpen || !q.isRunning() {
		t.Fatalf("err=%v state=%s running=%v", err, g.State(), q.isRunning())
	}
	if err := g.Open(t.Context()); !errors.Is(err, ErrState) {
		t.Fatalf("open twice = %v", err)
	}
}

func TestReadOnlyModeRejectsWritesAndStopsQueues(t *testing.T) {
	g := newGate(t, Options{}, &fakeRepl{})
	q := &fakeQueue{}
	_ = g.Start(t.Context(), q)
	h := g.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	call := func(method string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, "/v1/lessons/x/sync", nil))
		return w
	}
	if call("POST").Code != http.StatusNoContent {
		t.Fatal("writes must pass while open")
	}
	if err := g.EnterReadOnly(t.Context()); err != nil {
		t.Fatal(err)
	}
	if g.State() != StateReadOnly || q.isRunning() {
		t.Fatalf("state=%s running=%v", g.State(), q.isRunning())
	}
	if w := call("GET"); w.Code != http.StatusNoContent {
		t.Fatalf("read = %d", w.Code)
	}
	w := call("POST")
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") == "" {
		t.Fatalf("write = %d retry-after=%q", w.Code, w.Header().Get("Retry-After"))
	}
	if err := g.EnterReadOnly(t.Context()); !errors.Is(err, ErrState) {
		t.Fatalf("enter twice = %v", err)
	}
	if err := g.LeaveReadOnly(t.Context()); err != nil {
		t.Fatal(err)
	}
	if call("POST").Code != http.StatusNoContent || !q.isRunning() || g.State() != StateOpen {
		t.Fatal("leaving read-only must resume writes and queues")
	}
}

func TestOperatorOnlyTrigger(t *testing.T) {
	repl := &fakeRepl{}
	g := newGate(t, Options{}, repl)
	cases := []struct {
		name      string
		authorize func(*http.Request) (bool, error)
		want      int
	}{
		{"no authorizer fails closed", nil, http.StatusForbidden},
		{"non-operator", func(*http.Request) (bool, error) { return false, nil }, http.StatusForbidden},
		{"authorizer error", func(*http.Request) (bool, error) { return false, errors.New("db down") }, http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		h := g.Handler(tc.authorize)
		for _, route := range []struct{ method, path string }{
			{"POST", "/admin/v1/switchover/switch"}, {"POST", "/admin/v1/switchover/open"}, {"POST", "/admin/v1/switchover/readonly"},
			{"POST", "/admin/v1/switchover/resume"}, {"GET", "/admin/v1/switchover/status"},
		} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(route.method, route.path, nil))
			if w.Code != tc.want {
				t.Errorf("%s %s %s = %d, want %d", tc.name, route.method, route.path, w.Code, tc.want)
			}
		}
	}
	if len(repl.log()) != 0 || g.State() != StateOpen {
		t.Fatalf("denied callers changed the gate: %v %s", repl.log(), g.State())
	}
	allow := g.Handler(func(*http.Request) (bool, error) { return true, nil })
	w := httptest.NewRecorder()
	allow.ServeHTTP(w, httptest.NewRequest("POST", "/admin/v1/switchover/switch", nil))
	if w.Code != http.StatusOK || !g.Switched() {
		t.Fatalf("operator switch = %d %s", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	allow.ServeHTTP(w, httptest.NewRequest("GET", "/admin/v1/switchover/status", nil))
	if !strings.Contains(w.Body.String(), `"switched":true`) {
		t.Fatalf("status = %s", w.Body)
	}
	w = httptest.NewRecorder()
	allow.ServeHTTP(w, httptest.NewRequest("POST", "/admin/v1/switchover/switch", nil))
	if w.Code != http.StatusConflict {
		t.Fatalf("repeat switch = %d", w.Code)
	}
}
