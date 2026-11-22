//go:build integration

package accounts_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/accountstest"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const publishedPrefix = "lessons/published/11111111-1111-4111-8111-111111111111/"

// erasureHarness signs learners up by email code and erases them through the HTTP API.
type erasureHarness struct {
	t    *testing.T
	ctx  context.Context
	pool *pgxpool.Pool
	auth *accounts.Service
	mail *accounts.MailSink
}

func newErasureHarness(t *testing.T) *erasureHarness {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	t.Cleanup(cancel)
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("erasurecov"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	mail := new(accounts.MailSink)
	auth, err := accounts.New(db.Pool, strings.Repeat("test-key", 5), mail)
	if err != nil {
		t.Fatal(err)
	}
	auth.ConfigureDeletionLedger(accountstest.NewDeletionLedger())
	auth.ConfigureWebOrigins([]string{webOrigin})
	return &erasureHarness{t: t, ctx: ctx, pool: db.Pool, auth: auth, mail: mail}
}

func (h *erasureHarness) call(method, path string, body any, token string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequestWithContext(h.ctx, method, path, strings.NewReader(string(b)))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.auth.ServeHTTP(w, req)
	return w
}

// signUp returns the access token and learner id of a new email learner.
func (h *erasureHarness) signUp(email string) (token, learner string) {
	h.t.Helper()
	if w := h.call("POST", "/v1/auth/code", map[string]any{"email": email, "age_confirmed": true}, ""); w.Code != 202 {
		h.t.Fatal(w.Code, w.Body)
	}
	w := h.call("POST", "/v1/auth/verify", map[string]any{"email": email, "code": h.mail.Code(email), "age_confirmed": true}, "")
	if w.Code != 200 {
		h.t.Fatal(w.Code, w.Body)
	}
	var tokens accounts.Tokens
	if err := json.Unmarshal(w.Body.Bytes(), &tokens); err != nil {
		h.t.Fatal(err)
	}
	if err := h.pool.QueryRow(h.ctx, "SELECT learner_id::text FROM auth_identities WHERE email=$1", email).Scan(&learner); err != nil {
		h.t.Fatal(err)
	}
	return tokens.AccessToken, learner
}

func (h *erasureHarness) erase(token string) {
	h.t.Helper()
	if w := h.call("POST", "/v1/me/deletion-request", map[string]string{"confirmation": "DELETE"}, token); w.Code != 200 {
		h.t.Fatal("erase:", w.Code, w.Body)
	}
}

// seedGenerated creates a course with one generated lesson whose manifest lives under a private
// generated prefix, and returns that prefix.
func (h *erasureHarness) seedGenerated(learner string) string {
	h.t.Helper()
	var quote, creation, course, unit, stub, generated string
	must := func(err error) {
		h.t.Helper()
		if err != nil {
			h.t.Fatal(err)
		}
	}
	p, ctx := h.pool, h.ctx
	must(p.QueryRow(ctx, `INSERT INTO pricing_quotes(learner_id, owner_reference, pricing_version, snapshot, created_at, expires_at) VALUES($1::uuid,'seed-'||gen_random_uuid(),(SELECT max(version) FROM pricing_versions),'{}',now(),now()+interval '10 minutes') RETURNING id::text`, learner).Scan(&quote))
	must(p.QueryRow(ctx, `INSERT INTO course_creations(learner_id, draft_id, revision, quote_id, request, state) VALUES($1::uuid, gen_random_uuid(), 1, $2::uuid, '{}', 'enrolled') RETURNING id::text`, learner, quote).Scan(&creation))
	must(p.QueryRow(ctx, `INSERT INTO courses(learner_id, creation_id, title) VALUES($1::uuid,$2::uuid,'c') RETURNING id::text`, learner, creation).Scan(&course))
	must(p.QueryRow(ctx, `INSERT INTO course_units(course_id, learner_id, authored_id, position, title) VALUES($1::uuid,$2::uuid,'u',0,'u') RETURNING id::text`, course, learner).Scan(&unit))
	must(p.QueryRow(ctx, `INSERT INTO course_lesson_stubs(unit_id, learner_id, authored_id, position, title, goal, status) VALUES($1::uuid,$2::uuid,'l',0,'l','g','locked') RETURNING id::text`, unit, learner).Scan(&stub))
	must(p.QueryRow(ctx, `INSERT INTO generated_lessons(lesson_stub_id, course_id, learner_id, title, manifest) VALUES($1::uuid,$2::uuid,$3::uuid,'l','{}') RETURNING id::text`, stub, course, learner).Scan(&generated))
	prefix := "lessons/generated/" + generated + "/"
	_, err := p.Exec(ctx, `INSERT INTO lesson_manifests(lesson_id, learner_id, version, manifest_key) VALUES($1::uuid,$2::uuid,1,$3)`, stub, learner, prefix+"manifest.json")
	must(err)
	// A second lesson, an enrolled copy of a published snapshot, whose manifest is shared content.
	var copyStub string
	must(p.QueryRow(ctx, `INSERT INTO course_lesson_stubs(unit_id, learner_id, authored_id, position, title, goal, status) VALUES($1::uuid,$2::uuid,'c',1,'c','g','locked') RETURNING id::text`, unit, learner).Scan(&copyStub))
	_, err = p.Exec(ctx, `INSERT INTO lesson_manifests(lesson_id, learner_id, version, manifest_key) VALUES($1::uuid,$2::uuid,1,$3)`, copyStub, learner, publishedPrefix+"0-0/manifest.json")
	must(err)
	return prefix
}

// fakeObjectStore lists and deletes from a fixed key set; failDelete simulates an R2 outage.
type fakeObjectStore struct {
	mu         sync.Mutex
	keys       []string
	deleted    []string
	failDelete bool
}

func (f *fakeObjectStore) List(_ context.Context, prefix string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, k := range f.keys {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return out, nil
}

func (f *fakeObjectStore) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failDelete {
		return errors.New("storage down")
	}
	f.deleted = append(f.deleted, key)
	return nil
}

func (f *fakeObjectStore) PurgePrefix(ctx context.Context, prefix string) error {
	keys, err := f.List(ctx, prefix)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := f.Delete(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

func TestErasedAccountPrivateObjectsAreDeletedAfterRetentionIntegration(t *testing.T) {
	h := newErasureHarness(t)
	ctx := h.ctx
	token, learner := h.signUp("erase-objects@example.test")
	_, other := h.signUp("other-learner@example.test")

	own := h.seedGenerated(learner)
	otherPrefix := h.seedGenerated(other)
	// A character and an export-adjacent row exist in Postgres only; shared and published objects
	// must survive the erasure: a published snapshot an enrolled copy points at, a content-hash
	// narration asset another learner still references, and a shared character rig.
	published := publishedPrefix
	narration := "lessons/narration/v1/" + strings.Repeat("ab", 32) + "/audio.wav"
	rig := "lessons/character-rigs/v1/" + strings.Repeat("cd", 32) + ".svg"
	objects := &fakeObjectStore{keys: []string{
		own + "manifest.json", own + "slide-0.html",
		otherPrefix + "manifest.json",
		published + "0-0/manifest.json", narration, rig,
	}}
	svc := courses.New(courses.Options{Pool: h.pool, Objects: objects})

	h.erase(token)

	var pending int
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM course_object_purges WHERE purged_at IS NULL`).Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("expected exactly the learner's private prefix to be ledgered, got %d (err %v)", pending, err)
	}
	var exists bool
	if err := h.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM course_object_purges WHERE prefix LIKE 'lessons/published/%' OR prefix LIKE 'lessons/narration/%')`).Scan(&exists); err != nil || exists {
		t.Fatal("shared object prefixes must never be ledgered", err)
	}

	now := time.Now().UTC()
	if n, err := svc.SweepPurgedObjects(ctx, now); err != nil || n != 0 || len(objects.deleted) != 0 {
		t.Fatalf("objects deleted inside the dump window: n=%d err=%v deleted=%v", n, err, objects.deleted)
	}
	later := now.Add(accounts.ObjectPurgeRetention + time.Hour)
	// A storage outage surfaces and leaves the row pending for the next run.
	objects.failDelete = true
	if _, err := svc.SweepPurgedObjects(ctx, later); err == nil {
		t.Fatal("storage failure must surface")
	}
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM course_object_purges WHERE purged_at IS NULL`).Scan(&pending); err != nil || pending != 1 {
		t.Fatal("row must stay pending after a storage failure", pending, err)
	}
	objects.failDelete = false
	if n, err := svc.SweepPurgedObjects(ctx, later); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	slices.Sort(objects.deleted)
	if want := []string{own + "manifest.json", own + "slide-0.html"}; !slices.Equal(objects.deleted, want) {
		t.Fatalf("deleted %v, want exactly %v (other learners, published snapshots, narration and rigs stay)", objects.deleted, want)
	}
	if n, err := svc.SweepPurgedObjects(ctx, later); err != nil || n != 0 {
		t.Fatalf("second sweep must be a no-op: n=%d err=%v", n, err)
	}
}

func TestRestoredTombstoneErasureAlsoLedgersPrivateObjectsIntegration(t *testing.T) {
	h := newErasureHarness(t)
	_, learner := h.signUp("restored-objects@example.test")
	prefix := h.seedGenerated(learner)
	if _, err := h.pool.Exec(h.ctx, "INSERT INTO account_deletion_tombstones(learner_reference) VALUES($1)", accounts.LearnerReference(learner)); err != nil {
		t.Fatal(err)
	}
	if err := accounts.ApplyDeletionTombstones(h.ctx, h.pool); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FROM course_object_purges WHERE prefix=$1`, prefix).Scan(&n); err != nil || n != 1 {
		t.Fatal("restored erasure did not ledger the prefix", n, err)
	}
}

// scriptedPerson is a fake PostHog client: nothing here ever reaches PostHog.
type scriptedPerson struct {
	mu       sync.Mutex
	down     bool
	notFound bool
	calls    []string
}

func (s *scriptedPerson) DeletePerson(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, id)
	switch {
	case s.down:
		return errors.New("posthog unreachable")
	case s.notFound:
		return accounts.ErrPersonNotFound
	}
	return nil
}

func TestPostHogPersonDeletionIsQueuedThenDrainedIntegration(t *testing.T) {
	h := newErasureHarness(t)
	ctx := h.ctx
	queued := func() int {
		var n int
		if err := h.pool.QueryRow(ctx, "SELECT count(*) FROM posthog_person_deletion_requests").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	token, learner := h.signUp("posthog-erase@example.test")
	h.erase(token)
	if queued() != 1 {
		t.Fatal("erasure must queue exactly one PostHog request")
	}

	// Not configured: nothing is called, the request waits.
	if off := accounts.NewPostHogPersonEraser(h.pool, nil); off.PersonDeletionEnabled() {
		t.Fatal("enabled without a deleter")
	} else if n, err := off.ProcessPersonDeletions(ctx); n != 0 || err != nil || queued() != 1 {
		t.Fatal(n, err)
	}

	// Outage: the request stays, the attempt is counted, and the error carries no learner id.
	fake := &scriptedPerson{down: true}
	eraser := accounts.NewPostHogPersonEraser(h.pool, fake)
	n, err := eraser.ProcessPersonDeletions(ctx)
	if n != 0 || err == nil || strings.Contains(err.Error(), learner) {
		t.Fatal("outage not reported with counts only:", n, err)
	}
	var attempts int
	if err = h.pool.QueryRow(ctx, "SELECT attempts FROM posthog_person_deletion_requests").Scan(&attempts); err != nil || attempts != 1 || queued() != 1 {
		t.Fatal("attempt not counted", attempts, err)
	}

	// PostHog back after the retry delay: the job drains it; a second run is a no-op.
	if _, err = h.pool.Exec(ctx, "UPDATE posthog_person_deletion_requests SET last_attempt_at=now()-interval '11 minutes'"); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.down = false
	fake.mu.Unlock()
	if n, err = eraser.ProcessPersonDeletions(ctx); n != 1 || err != nil || queued() != 0 {
		t.Fatal(n, err)
	}
	before := len(fake.calls)
	if n, err = eraser.ProcessPersonDeletions(ctx); n != 0 || err != nil || len(fake.calls) != before {
		t.Fatal("repeat run was not a no-op", n, err)
	}

	// "Already gone" at PostHog is success.
	token2, _ := h.signUp("posthog-gone@example.test")
	h.erase(token2)
	fake.notFound = true
	if n, err = eraser.ProcessPersonDeletions(ctx); n != 1 || err != nil || queued() != 0 {
		t.Fatal("person not found must count as done", n, err)
	}
}

func TestPostHogRetryBackoffDoesNotStarveNewRequestsIntegration(t *testing.T) {
	h := newErasureHarness(t)
	ctx := h.ctx
	if _, err := h.pool.Exec(ctx, `INSERT INTO posthog_person_deletion_requests(distinct_id,requested_at,attempts,last_attempt_at) SELECT gen_random_uuid(),now()-interval '2 days',5,now() FROM generate_series(1,50)`); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := h.pool.QueryRow(ctx, `INSERT INTO posthog_person_deletion_requests(distinct_id) VALUES(gen_random_uuid()) RETURNING distinct_id::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	fake := &scriptedPerson{}
	n, err := accounts.NewPostHogPersonEraser(h.pool, fake).ProcessPersonDeletions(ctx)
	if err != nil || n != 1 || len(fake.calls) != 1 || fake.calls[0] != id {
		t.Fatalf("new request starved or backoff ignored: n=%d calls=%d err=%v", n, len(fake.calls), err)
	}
}
