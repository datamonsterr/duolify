//go:build integration

package sharing_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/examples"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/lessontest"
	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
)

func (m *memObjects) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, ok := m.puts[key]
	if !ok {
		return nil, fmt.Errorf("missing object %s", key)
	}
	return append([]byte(nil), raw...), nil
}

// countingEngine fails the test's premise if anything reaches the engine.
type countingEngine struct{ calls atomic.Int64 }

func (c *countingEngine) GenerateLesson(context.Context, *enginev1.GenerateLessonRequest) (*enginev1.Lesson, []*enginev1.Usage, error) {
	c.calls.Add(1)
	return nil, nil, fmt.Errorf("the engine must not be called for authored examples")
}

func TestExampleEnrollmentIsIdempotentConcurrentAndPinnedPerVersion(t *testing.T) {
	f := newSeededSharingFixture(t)
	ctx := t.Context()
	a := newLearner(t, f, "Example A")
	b := newLearner(t, f, "Example B")
	ev := &capture{}
	s1, objects1 := enrollService(t, f, ev, nil)
	s2, objects2 := enrollService(t, f, ev, nil) // a second API instance

	if _, err := s1.EnrollExample(ctx, a, "no-such-example"); err != sharing.ErrExampleNotFound {
		t.Fatalf("unknown slug: %v", err)
	}
	// Installation is idempotent and identical on two instances.
	for i := 0; i < 2; i++ {
		if err := s1.InstallExampleObjects(ctx); err != nil {
			t.Fatal(err)
		}
		if err := s2.InstallExampleObjects(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if objects1.all() != objects2.all() || len(objects1.puts) == 0 {
		t.Fatal("two instances must install identical objects")
	}

	// Concurrent first enrollments from two instances return one course.
	ids := make([]string, 6)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc := []*sharing.Service{s1, s2}[i%2]
			got, err := svc.EnrollExample(ctx, a, "javascript-essentials")
			if err != nil {
				t.Error(err)
				return
			}
			ids[i] = got.CourseID
		}()
	}
	wg.Wait()
	for _, id := range ids {
		if id == "" || id != ids[0] {
			t.Fatalf("enrollments differ: %v", ids)
		}
	}
	first := ids[0]
	if n := count(t, f, `SELECT count(*) FROM course_copies WHERE learner_id=$1 AND example_slug='javascript-essentials'`, a); n != 1 {
		t.Fatalf("copies = %d", n)
	}
	var version int
	var source, hash string
	var sourceCourse, owner, draft *string
	if err := f.DB.Pool.QueryRow(ctx, `SELECT version,source,content_hash,source_course_id::text,source_owner_id::text,draft_id::text FROM course_copies WHERE copy_course_id=$1`, first).Scan(&version, &source, &hash, &sourceCourse, &owner, &draft); err != nil {
		t.Fatal(err)
	}
	if version != 1 || source != "example" || len(hash) != 64 || sourceCourse != nil || owner != nil || draft != nil {
		t.Fatalf("pin: %d %s %s %v %v %v", version, source, hash, sourceCourse, owner, draft)
	}
	if st := stubStates(t, f, first); strings.Join(st, ",") != "current,locked,locked,locked,locked,locked" {
		t.Fatalf("gate %v", st)
	}
	if n := count(t, f, `SELECT count(*) FROM lesson_manifests m JOIN course_lesson_stubs l ON l.id=m.lesson_id JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1 AND starts_with(m.manifest_key,'lessons/examples/javascript-essentials/v1/')`, first); n != 6 {
		t.Fatalf("manifests = %d", n)
	}
	if n := count(t, f, `SELECT count(*) FROM lesson_retry_pools p JOIN course_lesson_stubs l ON l.id=p.lesson_stub_id JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1`, first); n < 12 {
		t.Fatalf("retry pools = %d", n)
	}
	if n := count(t, f, `SELECT count(*) FROM lesson_slides s JOIN course_lesson_stubs l ON l.id=s.lesson_stub_id JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1 AND cardinality(s.topic_ids)>0`, first); n != 24 {
		t.Fatalf("slides with topics = %d", n)
	}
	var active string
	if err := f.DB.Pool.QueryRow(ctx, `SELECT active_course_id::text FROM learner_profiles WHERE learner_id=$1`, a).Scan(&active); err != nil || active != first {
		t.Fatalf("active course %q %v", active, err)
	}
	// Every manifest digest matches the stored slide bytes.
	rows, err := f.DB.Pool.Query(ctx, `SELECT m.manifest_key FROM lesson_manifests m JOIN course_lesson_stubs l ON l.id=m.lesson_id JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1`, first)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for rows.Next() {
		var k string
		if err = rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k)
	}
	rows.Close()
	for _, key := range keys {
		var m player.ObjectManifest
		raw, _ := objects1.Get(ctx, key)
		if err = json.Unmarshal(raw, &m); err != nil || len(m.Slides) == 0 {
			t.Fatalf("manifest %s: %v", key, err)
		}
		for _, sl := range m.Slides {
			html, _ := objects1.Get(ctx, sl.HTMLKey)
			if fmt.Sprintf("%x", sha256.Sum256(html)) != sl.HTMLSHA256 {
				t.Fatalf("digest mismatch %s", sl.HTMLKey)
			}
		}
	}

	// A newer authored version ships: the first learner keeps v1, a new learner gets v2.
	v2 := func(slug string) (examples.Course, bool) {
		c, ok := examples.Get(slug)
		return c.Derive(2, c.Title+" (revised)"), ok
	}
	s3 := s1.WithExamples(v2)
	again, err := s3.EnrollExample(ctx, a, "javascript-essentials")
	if err != nil || again.CourseID != first || again.Version != 1 {
		t.Fatalf("existing enrollment must stay on v1: %+v %v", again, err)
	}
	got2, err := s3.EnrollExample(ctx, b, "javascript-essentials")
	if err != nil || got2.CourseID == first || got2.Version != 2 {
		t.Fatalf("new enrollment must get v2: %+v %v", got2, err)
	}
	for _, key := range keys { // v1 objects remain readable for the pinned learner
		if _, err = objects1.Get(ctx, key); err != nil {
			t.Fatal(err)
		}
	}

	// Deleting the copy follows normal course deletion; enrolling again makes a fresh copy.
	if _, err = f.DB.Pool.Exec(ctx, `UPDATE courses SET deleted_at=now() WHERE id=$1`, first); err != nil {
		t.Fatal(err)
	}
	fresh, err := s1.EnrollExample(ctx, a, "javascript-essentials")
	if err != nil || fresh.CourseID == first || !fresh.Created {
		t.Fatalf("re-enroll after delete: %+v %v", fresh, err)
	}

	// Erasure removes the copy with the learner.
	if _, err = f.DB.Pool.Exec(ctx, `DELETE FROM learners WHERE id=$1`, b); err != nil {
		t.Fatal(err)
	}
	if n := count(t, f, `SELECT count(*) FROM course_copies WHERE learner_id=$1`, b); n != 0 {
		t.Fatalf("copies left after erasure: %d", n)
	}
}

// TestExampleCourseIsPlayableAtZeroCreditsWithoutTheEngine drives the real HTTP stack: a brand-new
// learner with no Credits enrolls, reads the manifest and every slide of all six lessons and passes
// every lesson test; the Credit ledger never moves and the engine is never called.
func TestExampleCourseIsPlayableAtZeroCreditsWithoutTheEngine(t *testing.T) {
	f := newSeededSharingFixture(t)
	ctx := t.Context()
	objects := &memObjects{puts: map[string][]byte{}}
	engine := &countingEngine{}
	mail := new(accounts.MailSink)
	auth, err := accounts.New(f.DB.Pool, strings.Repeat("example-key", 4), mail)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := pricing.Load(ctx, f.DB.Queries)
	if err != nil {
		t.Fatal(err)
	}
	financial := credits.NewServiceWithCatalogue(f.DB.Pool, nil, cat, nil)
	exampleEvents := &capture{}
	shared := sharing.NewService(f.DB.Pool, exampleEvents, sharing.DefaultConfig()).WithObjects(objects, asset)
	lessons := lessontest.New(lessontest.Options{Objects: objects, AssetOrigin: asset, Pool: f.DB.Pool, Engine: engine, Financial: financial})
	deps := httpapi.Dependencies{Accounts: auth, LessonTestTracerEnabled: true, Sharing: shared,
		Player: player.New(player.Options{Pool: f.DB.Pool, Objects: objects, AssetOrigin: asset, ManifestCacheTTL: -1}),
		Quotes: pricing.NewQuotes(f.DB.Pool), Credits: financial, Pricing: pricing.NewRuntimeCatalogue(cat, f.DB.Queries, f.DB.Pool, nil)}
	server := httptest.NewServer(httpapi.NewWithLessonTest(deps, lessons, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()
	call := func(method, path, token string, body any) (int, http.Header, map[string]any) {
		var r io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			r = bytes.NewReader(raw)
		}
		req, _ := http.NewRequestWithContext(ctx, method, server.URL+path, r)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer func() { _ = resp.Body.Close() }()
		var m map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&m)
		return resp.StatusCode, resp.Header, m
	}
	email := "example-learner@example.test"
	call("POST", "/v1/auth/code", "", map[string]any{"email": email, "age_confirmed": true})
	_, _, out := call("POST", "/v1/auth/verify", "", map[string]any{"email": email, "age_confirmed": true, "code": mail.Code(email)})
	token := out["access_token"].(string)
	var learner string
	if err = f.DB.Pool.QueryRow(ctx, `SELECT learner_id::text FROM auth_identities WHERE email=$1`, email).Scan(&learner); err != nil {
		t.Fatal(err)
	}

	if code, _, _ := call("GET", "/v1/courses/examples", "", nil); code != 401 {
		t.Fatalf("catalogue without session: %d", code)
	}
	code, header, list := call("GET", "/v1/courses/examples", token, nil)
	if code != 200 || len(list["examples"].([]any)) != 2 || !strings.Contains(header.Get("Cache-Control"), "private") || header.Get("ETag") == "" {
		t.Fatalf("catalogue: %d %v %v", code, header, list)
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/v1/courses/examples", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("If-None-Match", header.Get("ETag"))
	revalidated, e := server.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	_ = revalidated.Body.Close()
	if revalidated.StatusCode != 304 {
		t.Fatalf("revalidation: %d", revalidated.StatusCode)
	}
	if code, _, _ = call("POST", "/v1/courses/examples/nope/enroll", token, nil); code != 404 {
		t.Fatalf("unknown slug: %d", code)
	}
	code, _, enrolled := call("POST", "/v1/courses/examples/product-management-foundations/enroll", token, map[string]any{"surface": "courses"})
	courseID, _ := enrolled["course_id"].(string)
	if code != 200 || courseID == "" {
		t.Fatalf("enroll: %d %v", code, enrolled)
	}
	if _, _, again := call("POST", "/v1/courses/examples/product-management-foundations/enroll", token, nil); again["course_id"] != courseID {
		t.Fatalf("second enroll: %v", again)
	}
	if got := exampleEvents.named("example_course_enrolled"); len(got) != 1 || got[0]["course_id"] != courseID || got[0]["slug"] != "product-management-foundations" || got[0]["surface"] != "courses" || got[0]["source"] != "api" {
		t.Fatalf("example enrollment analytics: %#v", got)
	}

	course, _ := examples.Get("product-management-foundations")
	answers := map[string]examples.Question{} // by prompt, test and variants
	for _, u := range course.Units {
		for _, l := range u.Lessons {
			for _, q := range l.Test {
				answers[q.Prompt] = q
			}
			for _, p := range l.VariantPool {
				for _, v := range p.Variants {
					answers[v.Prompt] = v
				}
			}
		}
	}
	rows, err := f.DB.Pool.Query(ctx, `SELECT l.id::text FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1 ORDER BY u.position,l.position`, courseID)
	if err != nil {
		t.Fatal(err)
	}
	var lessonIDs []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		lessonIDs = append(lessonIDs, id)
	}
	rows.Close()
	if len(lessonIDs) != 6 {
		t.Fatalf("lessons = %d", len(lessonIDs))
	}
	for i, id := range lessonIDs {
		code, _, manifest := call("GET", "/v1/lessons/"+id+"/manifest", token, nil)
		slides, _ := manifest["slides"].([]any)
		if code != 200 || len(slides) < 3 {
			t.Fatalf("lesson %d manifest: %d %v", i+1, code, manifest)
		}
		for _, sl := range slides {
			slideID := sl.(map[string]any)["slide_id"].(string)
			req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/v1/lessons/"+id+"/slides/"+slideID, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			resp, e := server.Client().Do(req)
			if e != nil || resp.StatusCode != 200 {
				t.Fatalf("lesson %d slide %s: %v %v", i+1, slideID, e, resp)
			}
			_ = resp.Body.Close()
		}
		code, _, test := call("GET", "/v1/lessons/"+id+"/test", token, nil)
		if code != 200 {
			t.Fatalf("lesson %d test: %d %v", i+1, code, test)
		}
		passed := false
		for guard := 0; guard < 20 && !passed; guard++ {
			var current map[string]any
			if guard == 0 {
				current = test["questions"].([]any)[0].(map[string]any)
			}
			_ = current
			// Answer every question of the test (and any retry variant) correctly, in order.
			for _, raw := range test["questions"].([]any) {
				q := raw.(map[string]any)
				want, ok := answers[q["prompt"].(string)]
				if !ok {
					t.Fatalf("lesson %d: question %q is not authored content", i+1, q["prompt"])
				}
				idx := -1
				for ci, c := range q["choices"].([]any) {
					if c.(string) == want.Choices[want.AnswerIndex] {
						idx = ci
					}
				}
				code, _, res := call("POST", "/v1/lessons/"+id+"/test/answer", token, map[string]any{"question_id": q["question_id"], "choice_index": idx})
				if code != 200 {
					t.Fatalf("lesson %d answer: %d %v", i+1, code, res)
				}
				if res["passed"] == true {
					passed = true
				}
			}
			break
		}
		if !passed {
			t.Fatalf("lesson %d was not passed", i+1)
		}
		var status string
		if err = f.DB.Pool.QueryRow(ctx, `SELECT status FROM course_lesson_stubs WHERE id=$1`, id).Scan(&status); err != nil || status != "done" {
			t.Fatalf("lesson %d status %q %v", i+1, status, err)
		}
		if i+1 < len(lessonIDs) {
			if err = f.DB.Pool.QueryRow(ctx, `SELECT status FROM course_lesson_stubs WHERE id=$1`, lessonIDs[i+1]).Scan(&status); err != nil || status != "current" {
				t.Fatalf("lesson %d did not unlock the next one: %q %v", i+1, status, err)
			}
		}
	}
	if n := count(t, f, `SELECT count(*) FROM credit_entries WHERE learner_id=$1`, learner); n != 0 {
		t.Fatalf("credit ledger moved: %d entries", n)
	}
	if n := count(t, f, `SELECT count(*) FROM credit_jobs WHERE learner_id=$1`, learner); n != 0 {
		t.Fatalf("credit jobs created: %d", n)
	}
	if n := engine.calls.Load(); n != 0 {
		t.Fatalf("engine calls = %d", n)
	}
	_ = backuptest.Fixture{}
}
