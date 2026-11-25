//go:build integration

package retention

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func newDB(t *testing.T) *store.Store {
	t.Helper()
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("purge"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)
	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err = migrations.Up(ctx, url); err != nil {
		t.Fatal(err)
	}
	db, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

// seed inserts n rows per table aged ageDays days. Foreign keys are skipped on one connection
// (the test user is a superuser): the sweep only reads created_at.
func seed(ctx context.Context, t *testing.T, db *store.Store, tag string, n, ageDays int) {
	t.Helper()
	conn, err := db.Pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err = conn.Exec(ctx, `SET session_replication_role = replica`); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = conn.Exec(ctx, `SET session_replication_role = DEFAULT`) }()
	age := fmt.Sprintf("%d days", ageDays)
	if _, err = conn.Exec(ctx, `INSERT INTO lesson_playback_sync_events(action_key, learner_id, lesson_id, version, slide_id, time_ms, occurred_at, created_at)
SELECT $1||'-'||g, gen_random_uuid(), gen_random_uuid(), 1, 's', 0, now(), now() - $2::interval FROM generate_series(1,$3) g`, tag, age, n); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, `INSERT INTO lesson_test_answer_operations(operation_key, session_id, learner_id, lesson_id, question_id, choice_index, response_payload, created_at)
SELECT $1||'-'||g, gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), 'q', 0, '{}', now() - $2::interval FROM generate_series(1,$3) g`, tag, age, n); err != nil {
		t.Fatal(err)
	}
}

func count(ctx context.Context, t *testing.T, db *store.Store, table string) int {
	t.Helper()
	var n int
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPurgeKeepsYoungerRowsAndLoopsBatches(t *testing.T) {
	ctx := t.Context()
	db := newDB(t)
	// Sync events: 31 days old is purged, 29 kept. Operations: 8 days old purged, 6 kept.
	seed(ctx, t, db, "old31", 12, 31)
	seed(ctx, t, db, "mid8", 7, 8)
	seed(ctx, t, db, "young", 5, 6)

	svc := New(db.Pool, Config{BatchSize: 5}, nil)
	res, err := svc.PurgeExpiredRows(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.PlaybackSyncEvents != 12 || res.AnswerOperations != 19 {
		t.Fatalf("unexpected result %+v", res)
	}
	if got := count(ctx, t, db, "lesson_playback_sync_events"); got != 12 {
		t.Fatalf("sync events left %d, want 12 (mid8 and young)", got)
	}
	if got := count(ctx, t, db, "lesson_test_answer_operations"); got != 5 {
		t.Fatalf("operations left %d, want 5 (young)", got)
	}
	// Idempotent: a second sweep deletes nothing.
	if res, err = svc.PurgeExpiredRows(ctx); err != nil || res.PlaybackSyncEvents != 0 || res.AnswerOperations != 0 {
		t.Fatalf("second sweep %+v %v", res, err)
	}
}

func TestBatchBoundaryExactMultipleTerminates(t *testing.T) {
	ctx := t.Context()
	db := newDB(t)
	seed(ctx, t, db, "old", 10, 40)
	res, err := New(db.Pool, Config{BatchSize: 5}, nil).PurgeExpiredRows(ctx)
	if err != nil || res.PlaybackSyncEvents != 10 || res.AnswerOperations != 10 {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestConcurrentSweepsDeleteEachRowOnceWithoutError(t *testing.T) {
	ctx := t.Context()
	db := newDB(t)
	seed(ctx, t, db, "old", 400, 40)
	var wg sync.WaitGroup
	results := make([]Result, 4)
	errs := make([]error, 4)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = New(db.Pool, Config{BatchSize: 7}, nil).PurgeExpiredRows(ctx)
		}()
	}
	wg.Wait()
	var events, ops int64
	for i, err := range errs {
		if err != nil {
			t.Fatalf("sweep %d: %v", i, err)
		}
		events += results[i].PlaybackSyncEvents
		ops += results[i].AnswerOperations
	}
	if events != 400 || ops != 400 {
		t.Fatalf("rows deleted exactly once across sweeps: events=%d ops=%d", events, ops)
	}
	if count(ctx, t, db, "lesson_playback_sync_events")+count(ctx, t, db, "lesson_test_answer_operations") != 0 {
		t.Fatal("rows left")
	}
}

func TestPurgeUsesCreatedAtIndexes(t *testing.T) {
	ctx := t.Context()
	db := newDB(t)
	for _, table := range []string{"lesson_playback_sync_events", "lesson_test_answer_operations"} {
		var n int
		if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE tablename=$1 AND indexdef LIKE '%(created_at)%'`, table).Scan(&n); err != nil || n == 0 {
			t.Fatalf("%s needs a created_at index: %d %v", table, n, err)
		}
	}
}

func TestTeachBackSweepClearsAbandonedExplanationsAndKeepsTheTurn(t *testing.T) {
	ctx, db := t.Context(), newDB(t)
	conn, err := db.Pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SET session_replication_role = replica`); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = conn.Exec(ctx, `SET session_replication_role = DEFAULT`) }()
	for _, age := range []string{"31 days", "1 day"} {
		if _, err := conn.Exec(ctx, `
INSERT INTO lesson_teach_backs(learner_id, lesson_stub_id, turn, messages, response, updated_at)
VALUES(gen_random_uuid(), gen_random_uuid(), 1, '[{"role":"user","content":"Private explanation"}]',
       '{"turn":1,"followUp":"What changes?"}', now() - $1::interval)`, age); err != nil {
			t.Fatal(err)
		}
	}
	svc := New(db.Pool, Config{BatchSize: 1}, nil)
	for range 2 {
		if _, err := svc.PurgeExpiredRows(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var preserved, remainingText int
	if err := db.Pool.QueryRow(ctx, `
SELECT count(*) FILTER (WHERE turn=1 AND response->>'followUp'='What changes?'),
       count(*) FILTER (WHERE messages <> '[]'::jsonb)
FROM lesson_teach_backs`).Scan(&preserved, &remainingText); err != nil {
		t.Fatal(err)
	}
	if preserved != 2 || remainingText != 1 {
		t.Fatalf("preserved turns=%d and explanations=%d, want 2 and 1", preserved, remainingText)
	}
}

func TestPurgeRemovesIdleGuidedChatDrafts(t *testing.T) {
	ctx, db := t.Context(), newDB(t)
	pool := db.Pool
	var learner string
	if err := pool.QueryRow(ctx, `INSERT INTO learners(display_name) VALUES('Draft retention') RETURNING id::text`).Scan(&learner); err != nil {
		t.Fatal(err)
	}
	for script, expiry := range map[string]string{"onboarding.setup": "now() + interval '5 days'", "course.brief": "now() - interval '1 day'"} {
		if _, err := pool.Exec(ctx, `INSERT INTO guided_chat_drafts(learner_id, script_id, script_version, step_id, answers, expires_at) VALUES($1::uuid,$2,1,'goal','{}'::jsonb,`+expiry+`)`, learner, script); err != nil {
			t.Fatal(err)
		}
	}
	res, err := New(pool, Config{}, nil).PurgeExpiredRows(ctx)
	if err != nil || res.GuidedChatDrafts != 1 {
		t.Fatalf("purge: %+v %v", res, err)
	}
	var left int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM guided_chat_drafts WHERE script_id='onboarding.setup'`).Scan(&left); err != nil || left != 1 {
		t.Fatalf("live draft purged: %d %v", left, err)
	}
}
