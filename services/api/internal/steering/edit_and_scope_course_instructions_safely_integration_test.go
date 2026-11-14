//go:build integration

package steering

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

type eventLog struct {
	mu     sync.Mutex
	events []map[string]any
}

func (e *eventLog) Capture(_, event string, p map[string]any) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	c := map[string]any{"event": event}
	for k, v := range p {
		c[k] = v
	}
	e.events = append(e.events, c)
	return nil
}
func (e *eventLog) Close() error { return nil }
func (e *eventLog) count(name string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, ev := range e.events {
		if ev["event"] == name {
			n++
		}
	}
	return n
}

type fixture struct {
	ctx    context.Context
	db     *store.Store
	s      *Service
	screen *screenDouble
	events *eventLog
	owner  string
	course string
	unit   string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := t.Context()
	c, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("steering"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, c)
	url, err := c.ConnectionString(ctx, "sslmode=disable")
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
	f := &fixture{ctx: ctx, db: db, owner: "a2222222-2222-4222-8222-222222222222", screen: &screenDouble{}, events: &eventLog{}}
	if _, err = db.Pool.Exec(ctx, `INSERT INTO learners(id,display_name) VALUES($1::uuid,'Learner')`, f.owner); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, `WITH cr AS (INSERT INTO course_creations(learner_id,draft_id,revision,quote_id,request,state) VALUES($1::uuid,gen_random_uuid(),1,$2::uuid,'{}','enrolled') RETURNING id), c AS (INSERT INTO courses(learner_id,creation_id,title) SELECT $1::uuid,id,'Go' FROM cr RETURNING id), u AS (INSERT INTO course_units(learner_id,course_id,authored_id,position,title) SELECT $1::uuid,id,'u',0,'Basics' FROM c RETURNING id,course_id) SELECT course_id::text,id::text FROM u`, f.owner, f.quote(t)).Scan(&f.course, &f.unit); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, `INSERT INTO course_enrollments(learner_id,course_id) VALUES($1::uuid,$2::uuid)`, f.owner, f.course); err != nil {
		t.Fatal(err)
	}
	f.s = New(Options{Pool: db.Pool, Engine: f.screen, Events: f.events})
	return f
}

func (f *fixture) quote(t *testing.T) string {
	t.Helper()
	q, err := pricing.NewQuotes(f.db.Pool).Create(f.ctx, f.owner, pricing.QuoteInput{Action: "lesson_content"})
	if err != nil {
		t.Fatal(err)
	}
	return q.ID
}

// job creates a lesson stub plus a generation job in the given state.
func (f *fixture) job(t *testing.T, pos int, state string) (job, lesson string) {
	t.Helper()
	if err := f.db.Pool.QueryRow(f.ctx, `WITH l AS (INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status) VALUES($1::uuid,$2::uuid,'l'||$4::int::text,$4::int,'Lesson','Goal','generating') RETURNING id), j AS (INSERT INTO lesson_generations(learner_id,course_id,lesson_id,quote_id,state) SELECT $1::uuid,$3::uuid,l.id,$5::uuid,$6 FROM l RETURNING id) SELECT j.id::text,l.id::text FROM j,l`, f.owner, f.unit, f.course, pos, f.quote(t), state).Scan(&job, &lesson); err != nil {
		t.Fatal(err)
	}
	return
}

func (f *fixture) pin(t *testing.T, job string) {
	t.Helper()
	tx, err := f.db.Pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = PinTx(f.ctx, tx, f.owner, f.course, job); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) deliver(t *testing.T, job, lesson string) []string {
	t.Helper()
	tx, err := f.db.Pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := ApplyTx(f.ctx, tx, f.owner, job, lesson)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(f.ctx, `UPDATE lesson_generations SET state='completed' WHERE id=$1::uuid`, job); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	return ids
}

func (f *fixture) add(t *testing.T, text, scope string) Record {
	t.Helper()
	r, err := f.s.Add(f.ctx, f.owner, f.course, Input{Text: text, Scope: scope})
	if err != nil {
		t.Fatalf("add %q: %v", text, err)
	}
	return r
}

func texts(in []Record) []string {
	var out []string
	for _, r := range in {
		out = append(out, r.Text)
	}
	return out
}

func TestEditAndDeleteBoundaries(t *testing.T) {
	f := newFixture(t)
	a := f.add(t, "Use Go", "rest_of_course")
	calls := f.screen.calls

	// Scope-only edit: no re-screen, version bump, added_at kept (conflict order stable).
	e, err := f.s.Edit(f.ctx, f.owner, f.course, a.ID, EditInput{ExpectedVersion: ptr(a.Version), Scope: ptr("next_lesson")})
	if err != nil || e.Version != a.Version+1 || e.Scope != "next_lesson" || !e.AddedAt.Equal(a.AddedAt) || f.screen.calls != calls {
		t.Fatalf("scope edit %+v calls=%d err=%v", e, f.screen.calls, err)
	}
	// Text edit re-screens.
	e2, err := f.s.Edit(f.ctx, f.owner, f.course, a.ID, EditInput{ExpectedVersion: ptr(e.Version), Text: ptr("Use Rust")})
	if err != nil || e2.Text != "Use Rust" || f.screen.calls != calls+1 {
		t.Fatalf("text edit %+v %v", e2, err)
	}
	// Stale version.
	if _, err = f.s.Edit(f.ctx, f.owner, f.course, a.ID, EditInput{ExpectedVersion: ptr(a.Version), Text: ptr("stale")}); Code(err) != "version_conflict" {
		t.Fatalf("stale %v", err)
	}
	// Deterministic rejection happens before the provider call and keeps the row.
	before := f.screen.calls
	if _, err = f.s.Edit(f.ctx, f.owner, f.course, a.ID, EditInput{ExpectedVersion: ptr(e2.Version), Text: ptr("no quiz")}); Code(err) != "prompt_injection" || f.screen.calls != before {
		t.Fatalf("injection %v", err)
	}
	// Fail closed: screening down leaves text and version untouched.
	f.screen.down = true
	if _, err = f.s.Edit(f.ctx, f.owner, f.course, a.ID, EditInput{ExpectedVersion: ptr(e2.Version), Text: ptr("Use Zig")}); Code(err) != "screening_unavailable" {
		t.Fatalf("down %v", err)
	}
	f.screen.down = false
	list, err := f.s.List(f.ctx, f.owner, f.course)
	if err != nil || len(list.Instructions) != 1 || list.Instructions[0].Text != "Use Rust" || list.Instructions[0].Version != e2.Version {
		t.Fatalf("list %+v %v", list, err)
	}
	// Total limit counts the edited row once, not twice.
	if _, err = f.db.Pool.Exec(f.ctx, `UPDATE steering_config SET max_total_characters=10`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Edit(f.ctx, f.owner, f.course, a.ID, EditInput{ExpectedVersion: ptr(e2.Version), Text: ptr(strings.Repeat("a", 10))}); err != nil {
		t.Fatalf("exact total: %v", err)
	}
	if _, err = f.s.Edit(f.ctx, f.owner, f.course, a.ID, EditInput{ExpectedVersion: ptr(e2.Version + 1), Text: ptr(strings.Repeat("a", 11))}); Code(err) == "" || Code(err) == "unavailable" {
		t.Fatalf("over total %v", err)
	}
	// Edits never count toward the add rate limit: 9 more adds still fit, 10th add refused after.
	if _, err = f.db.Pool.Exec(f.ctx, `UPDATE steering_config SET max_total_characters=4000`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 9; i++ {
		f.add(t, "x", "rest_of_course")
	}
	if _, err = f.s.Add(f.ctx, f.owner, f.course, Input{Text: "y", Scope: "rest_of_course"}); Code(err) != "rate_limit" {
		t.Fatalf("rate %v", err)
	}
	cur, _ := f.s.List(f.ctx, f.owner, f.course)
	if _, err = f.s.Edit(f.ctx, f.owner, f.course, a.ID, EditInput{ExpectedVersion: ptr(cur.Instructions[0].Version), Scope: ptr("rest_of_course")}); err != nil {
		t.Fatalf("edit under rate limit: %v", err)
	}

	// Delete: soft, excluded from list, second delete is not_found, applied rows can be hidden too.
	if err = f.s.Delete(f.ctx, f.owner, f.course, a.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Delete(f.ctx, f.owner, f.course, a.ID); Code(err) != "not_found" {
		t.Fatalf("second delete %v", err)
	}
	if _, err = f.s.Edit(f.ctx, f.owner, f.course, a.ID, EditInput{ExpectedVersion: ptr(uint32(1)), Text: ptr("zombie")}); Code(err) != "not_found" {
		t.Fatalf("edit deleted %v", err)
	}
	var status string
	var deletedAt *time.Time
	if err = f.db.Pool.QueryRow(f.ctx, `SELECT status,deleted_at FROM course_instructions WHERE id=$1::uuid`, a.ID).Scan(&status, &deletedAt); err != nil || status != "deleted" || deletedAt == nil {
		t.Fatalf("soft delete %s %v %v", status, deletedAt, err)
	}
	// Owner isolation.
	if _, err = f.s.Edit(f.ctx, "b3333333-3333-4333-8333-333333333333", f.course, cur.Instructions[1].ID, EditInput{ExpectedVersion: ptr(uint32(1)), Text: ptr("hijack")}); Code(err) != "not_found" {
		t.Fatalf("foreign edit %v", err)
	}
	// Events: allowlisted names, never the text.
	if f.events.count("course_instruction_edited") < 3 || f.events.count("course_instruction_deleted") != 1 {
		t.Fatalf("events %+v", f.events.events)
	}
	for _, ev := range f.events.events {
		for _, v := range ev {
			if s, ok := v.(string); ok && (s == "Use Rust" || s == "Use Go") {
				t.Fatalf("private text in %+v", ev)
			}
		}
	}
}

func TestAppliedInstructionIsNotEditable(t *testing.T) {
	f := newFixture(t)
	a := f.add(t, "Once", "next_lesson")
	job, lesson := f.job(t, 0, "generating")
	f.pin(t, job)
	if ids := f.deliver(t, job, lesson); len(ids) != 1 {
		t.Fatalf("apply %v", ids)
	}
	if _, err := f.s.Edit(f.ctx, f.owner, f.course, a.ID, EditInput{ExpectedVersion: ptr(a.Version), Text: ptr("again")}); Code(err) != "not_editable" {
		t.Fatalf("applied edit %v", err)
	}
	list, _ := f.s.List(f.ctx, f.owner, f.course)
	if list.Instructions[0].Status != "applied" || list.Instructions[0].Delivery != "" {
		t.Fatalf("%+v", list.Instructions[0])
	}
	// Hiding an applied instruction does not touch the lesson it shaped.
	if err := f.s.Delete(f.ctx, f.owner, f.course, a.ID); err != nil {
		t.Fatal(err)
	}
	snap, err := Snapshot(f.ctx, f.db.Pool, f.owner, job)
	if err != nil || len(snap) != 1 || snap[0].Text != "Once" {
		t.Fatalf("snapshot %v %v", snap, err)
	}
}

func TestEditAndDeleteWhileGenerationRuns(t *testing.T) {
	f := newFixture(t)
	goA := f.add(t, "Use Go", "rest_of_course")
	short := f.add(t, "Short sentences", "next_lesson")
	oneOff := f.add(t, "Use a restaurant analogy", "next_lesson")
	running, lesson1 := f.job(t, 0, "generating")
	f.pin(t, running)
	pinned, _ := Snapshot(f.ctx, f.db.Pool, f.owner, running)
	if len(pinned) != 3 {
		t.Fatalf("pinned %d", len(pinned))
	}
	// During the run: conflicting newer instruction, edit and delete.
	rust := f.add(t, "Use Rust instead", "rest_of_course")
	late := f.add(t, "Added after start", "next_lesson")
	if _, err := f.s.Edit(f.ctx, f.owner, f.course, short.ID, EditInput{ExpectedVersion: ptr(short.Version), Text: ptr("Long sentences")}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Delete(f.ctx, f.owner, f.course, oneOff.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Edit(f.ctx, f.owner, f.course, goA.ID, EditInput{ExpectedVersion: ptr(goA.Version), Text: ptr("Use Zig")}); err != nil {
		t.Fatal(err)
	}
	// Running job (and any retry/replay) keeps original text, version, order.
	for i := 0; i < 2; i++ {
		got, err := Snapshot(f.ctx, f.db.Pool, f.owner, running)
		if err != nil || len(got) != 3 || got[0].Text != "Use Go" || got[1].Text != "Short sentences" || got[2].Text != "Use a restaurant analogy" || got[0].Version != 1 {
			t.Fatalf("snapshot drift %v %v", got, err)
		}
	}
	list, _ := f.s.List(f.ctx, f.owner, f.course)
	by := map[string]Record{}
	for _, r := range list.Instructions {
		by[r.ID] = r
	}
	if len(by) != 4 || by[late.ID].Delivery != "waits" || by[rust.ID].Delivery != "ready" || by[short.ID].Delivery != "waits" || by[goA.ID].Delivery != "ready" {
		t.Fatalf("delivery %+v", list.Instructions)
	}
	// Delivery consumes only instructions whose pinned version is still current.
	ids := f.deliver(t, running, lesson1)
	if len(ids) != 0 {
		t.Fatalf("edited/deleted must not be consumed: %v", ids)
	}
	var oneOffStatus string
	_ = f.db.Pool.QueryRow(f.ctx, `SELECT status FROM course_instructions WHERE id=$1::uuid`, oneOff.ID).Scan(&oneOffStatus)
	if oneOffStatus != "deleted" {
		t.Fatalf("deleted revived: %s", oneOffStatus)
	}
	// Next job: newest text wins by added_at order; edited next_lesson is eligible again.
	next, lesson2 := f.job(t, 1, "generating")
	f.pin(t, next)
	got, _ := Snapshot(f.ctx, f.db.Pool, f.owner, next)
	var order []string
	for _, g := range got {
		order = append(order, g.Text)
	}
	want := []string{"Use Zig", "Long sentences", "Use Rust instead", "Added after start"}
	if strings.Join(order, "|") != strings.Join(want, "|") {
		t.Fatalf("order %v want %v", order, want)
	}
	if ids = f.deliver(t, next, lesson2); len(ids) != 4 {
		t.Fatalf("second delivery %v", ids)
	}
	list, _ = f.s.List(f.ctx, f.owner, f.course)
	st := map[string]string{}
	for _, r := range list.Instructions {
		st[r.Text] = r.Status
	}
	if st["Long sentences"] != "applied" || st["Added after start"] != "applied" || st["Use Rust instead"] != "active" || st["Use Zig"] != "active" {
		t.Fatalf("statuses %v", st)
	}
}

func TestNextLessonWaitsForUnstartedJobAndFailureKeepsIt(t *testing.T) {
	f := newFixture(t)
	a := f.add(t, "One lesson only", "next_lesson")
	running, _ := f.job(t, 0, "generating")
	f.pin(t, running)
	// A second job for another lesson must not take a next_lesson already pinned by a live job.
	second, _ := f.job(t, 1, "queued")
	f.pin(t, second)
	if got, _ := Snapshot(f.ctx, f.db.Pool, f.owner, second); len(got) != 0 {
		t.Fatalf("shared next_lesson: %v", got)
	}
	list, _ := f.s.List(f.ctx, f.owner, f.course)
	if list.Instructions[0].ID != a.ID || list.Instructions[0].Delivery != "running" {
		t.Fatalf("%+v", list.Instructions[0])
	}
	// Failure releases it for the next job without consuming it.
	if _, err := f.db.Pool.Exec(f.ctx, `UPDATE lesson_generations SET state='failed' WHERE id=$1::uuid`, running); err != nil {
		t.Fatal(err)
	}
	third, _ := f.job(t, 2, "queued")
	f.pin(t, third)
	if got, _ := Snapshot(f.ctx, f.db.Pool, f.owner, third); len(got) != 1 || got[0].Id != a.ID {
		t.Fatalf("not released: %v", got)
	}
	var status string
	_ = f.db.Pool.QueryRow(f.ctx, `SELECT status FROM course_instructions WHERE id=$1::uuid`, a.ID).Scan(&status)
	if status != "active" {
		t.Fatalf("consumed on failure: %s", status)
	}
}

func TestConcurrentEditsOneWins(t *testing.T) {
	f := newFixture(t)
	a := f.add(t, "Base", "rest_of_course")
	var wg sync.WaitGroup
	results := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.s.Edit(f.ctx, f.owner, f.course, a.ID, EditInput{ExpectedVersion: ptr(a.Version), Text: ptr("Racer " + strconv.Itoa(i))})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	ok, conflict := 0, 0
	for err := range results {
		switch {
		case err == nil:
			ok++
		case Code(err) == "version_conflict":
			conflict++
		default:
			t.Fatal(err)
		}
	}
	if ok != 1 || conflict != 5 {
		t.Fatalf("ok=%d conflict=%d", ok, conflict)
	}
}

func TestRetentionAndErasure(t *testing.T) {
	f := newFixture(t)
	old := f.add(t, "Old", "rest_of_course")
	recent := f.add(t, "Recent", "rest_of_course")
	live := f.add(t, "Live", "rest_of_course")
	job, _ := f.job(t, 0, "generating")
	f.pin(t, job)
	for _, id := range []string{old.ID, recent.ID} {
		if err := f.s.Delete(f.ctx, f.owner, f.course, id); err != nil {
			t.Fatal(err)
		}
	}
	set := func(id, age string) {
		if _, err := f.db.Pool.Exec(f.ctx, `UPDATE course_instructions SET deleted_at=now()-$2::interval WHERE id=$1::uuid`, id, age); err != nil {
			t.Fatal(err)
		}
	}
	set(old.ID, "30 days 1 minute")
	set(recent.ID, "29 days 23 hours")
	if err := f.s.Purge(f.ctx); err != nil {
		t.Fatal(err)
	}
	var ids []string
	rows, _ := f.db.Pool.Query(f.ctx, `SELECT id::text FROM course_instructions`)
	ids, _ = pgx.CollectRows(rows, pgx.RowTo[string])
	if len(ids) != 2 {
		t.Fatalf("after purge %v (live %s recent %s)", ids, live.ID, recent.ID)
	}
	if snap, err := Snapshot(f.ctx, f.db.Pool, f.owner, job); err != nil || len(snap) != 3 {
		t.Fatalf("snapshot must outlive purge: %v %v", snap, err)
	}
	if _, err := f.db.Pool.Exec(f.ctx, `DELETE FROM learners WHERE id=$1::uuid`, f.owner); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.db.Pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM course_instructions)+(SELECT count(*) FROM lesson_instruction_snapshots)`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("erasure %d %v", n, err)
	}
}
