package backup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"time"

	"filippo.io/age"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Budgets from the data-backup spec: at most 24h of data loss, 4h to recover.
const (
	RPOBudget = 24 * time.Hour
	RTOBudget = 4 * time.Hour
)

// CheckKind groups drill checks for reporting.
type CheckKind string

// Check kinds a recovery must pass.
const (
	KindSchema  CheckKind = "schema"
	KindData    CheckKind = "data"
	KindObjects CheckKind = "objects"
	KindPrivacy CheckKind = "privacy"
)

// Check is one registered recovery assertion. Register feature checks as
// migrations arrive; bootstrap row counts alone are not recovery evidence.
type Check struct {
	Name string
	Kind CheckKind
	Run  func(context.Context, *pgxpool.Pool) error
}

// CheckResult never carries error text: restored rows may contain private data.
type CheckResult struct {
	Name string
	Kind CheckKind
	OK   bool
}

// FinancialReplay is the extension point for E7-11's quarterly financial replay.
// E7-11 supplies canonical TEST-provider verification and an idempotent second pass.
// A nil hook reports "not-configured" for general-only scheduled drills.
type FinancialReplay func(context.Context, *pgxpool.Pool) error

// DrillStore is the read-only surface of the restorer identity.
type DrillStore interface {
	List(context.Context, string) ([]Object, error)
	GetCurrent(context.Context, string) ([]byte, error)
}

// Restorer loads a decrypted dump into an isolated throwaway Postgres and
// returns its pool and a cleanup that destroys it.
type Restorer func(ctx context.Context, dump []byte) (*pgxpool.Pool, func(), error)

// DrillOptions wires the drill. Hooks marked test-only are nil in production.
type DrillOptions struct {
	Store    DrillStore
	Identity age.Identity
	Restore  Restorer
	// Source is "ci" for the ready-PR pipeline or "scheduled" for the real-data drill.
	Source string
	Events interface {
		Capture(string, string, map[string]any) error
	}
	OptOut bool
	Now    func() time.Time
	// ExpectedMigrations is the embedded migration registry the restore must contain.
	ExpectedMigrations []int64
	ReplayDeletions    func(context.Context, *pgxpool.Pool) error
	RebuildRedis       func(context.Context, *pgxpool.Pool) (int, error)
	// Checks are feature checks (object references, business invariants).
	Checks []Check
	// ForbiddenValues are known PII strings that must be absent after anonymization.
	ForbiddenValues []string
	FinancialReplay FinancialReplay
	BeforeAnonymize func(context.Context, *pgxpool.Pool) error // test-only
	Inspect         func(context.Context, *pgxpool.Pool) error // test-only, runs on the anonymized database
}

// Report is the drill outcome. RTO is the measured drill duration.
type Report struct {
	OK              bool
	DumpKey         string
	SchemaVersion   int64
	RPO, RTO        time.Duration
	Checks          []CheckResult
	RedisRebuilt    int
	FinancialReplay string
}

// NewestDump picks the newest general dump by the date in its key.
func NewestDump(objects []Object) (key string, date time.Time, schema int64, ok bool) {
	for _, object := range objects {
		class, d, valid := ClassifyKey(object.Key)
		if !valid || class != General || (ok && !d.After(date)) {
			continue
		}
		var n int64
		if _, err := fmt.Sscanf(object.Key[len("2006-01-02/"):], "schema-%d.dump.age", &n); err != nil || n < 1 {
			continue
		}
		key, date, schema, ok = object.Key, d, n, true
	}
	return key, date, schema, ok
}

// RPO is the age of the newest dump measured from its key date; never negative.
func RPO(now, dumpDate time.Time) time.Duration { return max(now.Sub(dumpDate), 0) }

// RPOWithinBudget and RTOWithinBudget are inclusive.
func RPOWithinBudget(d time.Duration) bool { return d <= RPOBudget }

// RTOWithinBudget is inclusive.
func RTOWithinBudget(d time.Duration) bool { return d <= RTOBudget }

var errDrill = errors.New("restore drill failed")

// RunDrill restores the newest dump into a throwaway database. The restored
// copy is anonymized before any check, replay or hook touches it.
func RunDrill(ctx context.Context, o DrillOptions) (report Report, result error) {
	started := time.Now()
	defer func() {
		report.RTO = time.Since(started)
		if result == nil && !RTOWithinBudget(report.RTO) {
			result = errors.New("restore drill exceeded RTO")
		}
		report.OK = result == nil
		o.emit(report.OK, report.RTO)
	}()
	if o.Store == nil || o.Identity == nil || o.Restore == nil || o.Now == nil {
		return report, errors.New("restore drill is not configured")
	}
	objects, err := o.Store.List(ctx, "")
	if err != nil {
		return report, errors.New("restore drill listing failed")
	}
	key, date, schema, ok := NewestDump(objects)
	if !ok {
		return report, errors.New("restore drill found no dump")
	}
	report.DumpKey, report.SchemaVersion, report.RPO = key, schema, RPO(o.Now(), date)
	if !RPOWithinBudget(report.RPO) {
		return report, errors.New("restore drill RPO exceeds 24h")
	}
	ciphertext, err := o.Store.GetCurrent(ctx, key)
	if err != nil {
		return report, errors.New("restore drill download failed")
	}
	reader, err := age.Decrypt(bytes.NewReader(ciphertext), o.Identity)
	if err != nil {
		return report, errors.New("restore drill decryption failed")
	}
	dump, err := io.ReadAll(reader)
	if err != nil || !bytes.HasPrefix(dump, []byte("PGDMP")) {
		return report, errors.New("restore drill archive is not a custom pg_dump")
	}
	pool, cleanup, err := o.Restore(ctx, dump)
	if err != nil {
		return report, errors.New("restore drill restore failed")
	}
	defer cleanup()
	if o.BeforeAnonymize != nil {
		if err := o.BeforeAnonymize(ctx, pool); err != nil {
			return report, errors.New("restore drill setup hook failed")
		}
	}
	if err := Anonymize(ctx, pool); err != nil {
		return report, err
	}
	failed := false
	step := func(name string, kind CheckKind, run func() error) {
		ok := run() == nil
		failed = failed || !ok
		report.Checks = append(report.Checks, CheckResult{Name: name, Kind: kind, OK: ok})
	}
	// Recovery invariants are judged on the dump as restored; tombstones then
	// legitimately remove erased learners, and Redis is rebuilt from the result.
	for _, c := range defaultChecks(schema, o.ExpectedMigrations) {
		step(c.Name, c.Kind, func() error { return c.Run(ctx, pool) })
	}
	step("deletion tombstones", KindPrivacy, func() error {
		if o.ReplayDeletions == nil {
			return errDrill
		}
		return o.ReplayDeletions(ctx, pool)
	})
	step("redis rebuild", KindData, func() error {
		if o.RebuildRedis == nil {
			return errDrill
		}
		n, err := o.RebuildRedis(ctx, pool)
		report.RedisRebuilt = n
		return err
	})
	checks := append([]Check{}, o.Checks...)
	checks = append(checks, privacyChecks(o.ForbiddenValues)...)
	for _, c := range checks {
		step(c.Name, c.Kind, func() error { return c.Run(ctx, pool) })
	}
	report.FinancialReplay = "not-configured"
	if o.FinancialReplay != nil {
		report.FinancialReplay = "passed"
		step("financial replay", KindData, func() error { return o.FinancialReplay(ctx, pool) })
		if failed {
			report.FinancialReplay = "failed"
		}
	}
	if o.Inspect != nil {
		if err := o.Inspect(ctx, pool); err != nil {
			return report, errors.New("restore drill inspection failed")
		}
	}
	if failed {
		return report, errors.New("restore drill checks failed")
	}
	return report, nil
}

// emit sends only the catalogued operations events: no keys, errors or content.
func (o DrillOptions) emit(ok bool, duration time.Duration) {
	if o.OptOut || o.Events == nil {
		return
	}
	version := "development"
	if info, present := debug.ReadBuildInfo(); present && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
	props := func() map[string]any {
		source := o.Source
		if source == "scheduled" {
			source = "api"
		}
		return map[string]any{"source": source, "platform": "web", "app_version": version, "duration_ms": duration.Milliseconds(), "ok": ok}
	}
	const system = "00000000-0000-4000-8000-000000000011"
	if !ok {
		_ = o.Events.Capture(system, "backup_failed", props())
	}
	_ = o.Events.Capture(system, "restore_drill_completed", props())
}

func scalar(ctx context.Context, pool *pgxpool.Pool, query string, args ...any) (int64, error) {
	var n int64
	err := pool.QueryRow(ctx, query, args...).Scan(&n)
	return n, err
}

func defaultChecks(keySchema int64, expected []int64) []Check {
	checks := []Check{
		{"dump schema version matches key", KindSchema, func(ctx context.Context, p *pgxpool.Pool) error {
			n, err := scalar(ctx, p, "SELECT coalesce(max(version_id),0) FROM goose_db_version WHERE is_applied")
			if err != nil || n != keySchema {
				return errDrill
			}
			return nil
		}},
		{"river migrations", KindSchema, func(ctx context.Context, p *pgxpool.Pool) error {
			if n, err := scalar(ctx, p, "SELECT count(*) FROM river_migration"); err != nil || n == 0 {
				return errDrill
			}
			return nil
		}},
		{"constraints validated", KindSchema, func(ctx context.Context, p *pgxpool.Pool) error {
			if n, err := scalar(ctx, p, "SELECT count(*) FROM pg_constraint WHERE NOT convalidated"); err != nil || n != 0 {
				return errDrill
			}
			return nil
		}},
		{"credit ledger", KindData, func(ctx context.Context, p *pgxpool.Pool) error {
			n, err := scalar(ctx, p, "SELECT count(*) FROM credit_entries c LEFT JOIN learners l ON l.id=c.learner_id WHERE l.id IS NULL OR c.amount=0")
			if err != nil || n != 0 {
				return errDrill
			}
			return nil
		}},
	}
	if expected != nil {
		checks = append(checks, Check{"migration registry", KindSchema, func(ctx context.Context, p *pgxpool.Pool) error {
			rows, err := p.Query(ctx, "SELECT version_id FROM goose_db_version WHERE is_applied AND version_id>0")
			if err != nil {
				return errDrill
			}
			applied, err := pgx.CollectRows(rows, pgx.RowTo[int64])
			if err != nil {
				return errDrill
			}
			have := map[int64]bool{}
			for _, v := range applied {
				have[v] = true
			}
			for _, v := range expected {
				if !have[v] {
					return errDrill
				}
			}
			return nil
		}})
	}
	return checks
}

// ObjectReferences checks that every key a restored row set implies exists in
// application storage. keys may be derived from rows or from a fixed registry.
func ObjectReferences(name string, keys func(context.Context, *pgxpool.Pool) ([]string, error), exists func(context.Context, string) (bool, error)) Check {
	return Check{name, KindObjects, func(ctx context.Context, p *pgxpool.Pool) error {
		list, err := keys(ctx, p)
		if err != nil || len(list) == 0 {
			return errDrill
		}
		for _, key := range list {
			if ok, err := exists(ctx, key); err != nil || !ok {
				return errDrill
			}
		}
		return nil
	}}
}

type textColumn struct{ table, column string }

func privacyChecks(forbidden []string) []Check {
	columns := func(ctx context.Context, p *pgxpool.Pool) ([]textColumn, error) {
		rows, err := p.Query(ctx, `SELECT table_name, column_name FROM information_schema.columns c
 WHERE table_schema='public' AND data_type IN ('text','character varying','jsonb','json','ARRAY')
 AND table_name NOT LIKE 'river%' AND table_name NOT LIKE 'goose%'
 AND EXISTS (SELECT 1 FROM information_schema.tables t WHERE t.table_schema='public' AND t.table_name=c.table_name AND t.table_type='BASE TABLE')`)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(r pgx.CollectableRow) (textColumn, error) {
			var c textColumn
			return c, r.Scan(&c.table, &c.column)
		})
	}
	return []Check{
		{"pii column classification", KindPrivacy, func(ctx context.Context, p *pgxpool.Pool) error {
			cols, err := columns(ctx, p)
			if err != nil {
				return errDrill
			}
			for _, c := range cols {
				if ClassifyColumn(c.table, c.column) == Unclassified {
					return errDrill
				}
			}
			return nil
		}},
		{"restored output contains no pii", KindPrivacy, func(ctx context.Context, p *pgxpool.Pool) error {
			cols, err := columns(ctx, p)
			if err != nil {
				return errDrill
			}
			const lookahead = `[A-Za-z0-9._%+-]+@(?!` + `anonymized\.invalid)[A-Za-z0-9.-]+\.[A-Za-z]{2,}`
			for _, c := range cols {
				ref := pgx.Identifier{c.table}.Sanitize() + "." + pgx.Identifier{c.column}.Sanitize()
				n, err := scalar(ctx, p, "SELECT count(*) FROM "+pgx.Identifier{c.table}.Sanitize()+" WHERE "+ref+"::text ~* $1", lookahead)
				if err != nil || n != 0 {
					return errDrill
				}
				for _, value := range forbidden {
					n, err = scalar(ctx, p, "SELECT count(*) FROM "+pgx.Identifier{c.table}.Sanitize()+" WHERE position($1 in "+ref+"::text) > 0", value)
					if err != nil || n != 0 {
						return errDrill
					}
				}
			}
			return nil
		}},
	}
}
