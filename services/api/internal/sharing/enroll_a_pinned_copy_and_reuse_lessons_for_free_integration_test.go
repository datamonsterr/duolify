//go:build integration

package sharing_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/lessontest"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
)

const asset = "https://assets.example.test"

// memObjects is an in-memory immutable object store double (no RustFS).
type memObjects struct {
	mu   sync.Mutex
	puts map[string][]byte
}

func (m *memObjects) Put(_ context.Context, key string, raw []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.puts[key]; ok && !bytes.Equal(old, raw) {
		return errors.New("immutable key rewritten")
	}
	m.puts[key] = append([]byte(nil), raw...)
	return nil
}

func (m *memObjects) all() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := make([]string, 0, len(m.puts))
	for k := range m.puts {
		keys = append(keys, k)
	}
	sort.Strings(keys) // never depend on map order
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + "\n" + string(m.puts[k]) + "\n")
	}
	return b.String()
}

func enrollService(t *testing.T, f *backuptest.Fixture, ev *capture, mutate func(*sharing.Config)) (*sharing.Service, *memObjects) {
	t.Helper()
	cfg := sharing.DefaultConfig()
	if mutate != nil {
		mutate(&cfg)
	}
	objects := &memObjects{puts: map[string][]byte{}}
	s := sharing.NewService(f.DB.Pool, ev, cfg).WithAgeSource(adult(true)).WithObjects(objects, asset)
	s.ConfigureScreening(startEngine(t), fixedRates{})
	return s, objects
}

func newLearner(t *testing.T, f *backuptest.Fixture, name string) string {
	t.Helper()
	var id string
	if err := f.DB.Pool.QueryRow(t.Context(), `INSERT INTO learners(display_name) VALUES($1) RETURNING id::text`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// seedReusable gives the owner a course with two generated lessons and one still to generate.
func seedReusable(t *testing.T, f *backuptest.Fixture, title string) string {
	t.Helper()
	ctx, pool := t.Context(), f.DB.Pool
	var quote, creation, courseID, unit, l1, l2 string
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO pricing_quotes(learner_id,owner_reference,pricing_version,snapshot,created_at,expires_at) SELECT $1::uuid,encode(sha256(convert_to($1::text,'UTF8')),'hex'),version,'{}',now(),now()+interval '1 hour' FROM pricing_quote_policies ORDER BY version LIMIT 1 RETURNING id::text`, owner).Scan(&quote))
	must(pool.QueryRow(ctx, `INSERT INTO course_creations(learner_id,draft_id,revision,quote_id,request,state) VALUES($1,gen_random_uuid(),1,$2,'{}','enrolled') RETURNING id::text`, owner, quote).Scan(&creation))
	must(pool.QueryRow(ctx, `INSERT INTO courses(learner_id,creation_id,title,description) VALUES($1,$2,$3,'Look up.') RETURNING id::text`, owner, creation, title).Scan(&courseID))
	_, err := pool.Exec(ctx, `INSERT INTO course_enrollments(learner_id,course_id) VALUES($1,$2)`, owner, courseID)
	must(err)
	must(pool.QueryRow(ctx, `INSERT INTO course_units(learner_id,course_id,authored_id,position,title) VALUES($1,$2,'u1',0,'The Moon') RETURNING id::text`, owner, courseID).Scan(&unit))
	must(pool.QueryRow(ctx, `INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status) VALUES($1,$2,'l1',0,'Phases','Name phases','done') RETURNING id::text`, owner, unit).Scan(&l1))
	must(pool.QueryRow(ctx, `INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status) VALUES($1,$2,'l2',1,'Tides','Explain tides','done') RETURNING id::text`, owner, unit).Scan(&l2))
	_, err = pool.Exec(ctx, `INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status) VALUES($1,$2,'l3',2,'Eclipses','Explain eclipses','locked')`, owner, unit)
	must(err)
	for i, stub := range []string{l1, l2} {
		topic := []string{"moon", "tides"}[i]
		_, err = pool.Exec(ctx, `INSERT INTO generated_lessons(lesson_stub_id,course_id,learner_id,title,manifest) VALUES($1,$2,$3,$4,'{}'::jsonb)`, stub, courseID, owner, []string{"Phases", "Tides"}[i])
		must(err)
		_, err = pool.Exec(ctx, `INSERT INTO lesson_slides(lesson_stub_id,learner_id,position,slide_id,html,duration_ms,narration) VALUES($1,$2,0,'s'||$3::text,$4,3000,'Narration text.')`, stub, owner, i, "<h1>Slide "+topic+"</h1><p>Fact.</p>")
		must(err)
		_, err = pool.Exec(ctx, `INSERT INTO lesson_questions(lesson_stub_id,learner_id,position,topic_id,prompt,choices,answer_index,explanation) VALUES($1,$2,0,$3,'Which one?','["Right","Wrong"]'::jsonb,0,'SECRET-EXPLANATION')`, stub, owner, topic)
		must(err)
		_, err = pool.Exec(ctx, `INSERT INTO lesson_retry_pools(lesson_stub_id,learner_id,topic_id,variants) VALUES($1,$2,$3,'[{"prompt":"Again?","choices":["Right","Wrong"],"answer_index":0,"explanation":"SECRET-EXPLANATION"}]'::jsonb)`, stub, owner, topic)
		must(err)
	}
	return courseID
}

func publish(t *testing.T, s *sharing.Service, courseID string) string {
	t.Helper()
	d, err := s.BuildDraft(t.Context(), owner, courseID)
	if err != nil || d.Verdict != sharing.VerdictPass {
		t.Fatalf("draft: %v %+v", err, d)
	}
	token, err := s.PublishLink(t.Context(), owner, courseID)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func count(t *testing.T, f *backuptest.Fixture, q string, args ...any) int {
	t.Helper()
	var n int
	if err := f.DB.Pool.QueryRow(t.Context(), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func stubStates(t *testing.T, f *backuptest.Fixture, course string) []string {
	t.Helper()
	rows, err := f.DB.Pool.Query(t.Context(), `SELECT l.status FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1 ORDER BY u.position,l.position`, course)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err = rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func TestEnrollAPinnedCopyAndReuseLessonsForFree(t *testing.T) {
	f := backuptest.New(t)
	f.Seed(t)
	ctx := t.Context()
	ev := &capture{}
	s, objects := enrollService(t, f, ev, nil)
	src := seedReusable(t, f, "Astronomy")
	token := publish(t, s, src)

	var firstCopy string
	t.Run("one_pinned_copy_with_free_lessons_and_no_answer_keys", func(t *testing.T) {
		got, err := s.Enroll(ctx, stranger, token)
		if err != nil {
			t.Fatal(err)
		}
		firstCopy = got.CourseID
		if !got.Created || got.Version != 1 || got.TotalLessons != 3 || got.ReadyLessons != 2 || got.NeedsGenerationLessons != 1 {
			t.Fatalf("unexpected result %+v", got)
		}
		if got.CourseID == src {
			t.Fatal("copy must be a new learner-owned course")
		}
		var learner, source, draft string
		var version int
		if err = f.DB.Pool.QueryRow(ctx, `SELECT cc.learner_id::text,cc.source_course_id::text,cc.draft_id::text,cc.version FROM course_copies cc WHERE cc.copy_course_id=$1`, got.CourseID).Scan(&learner, &source, &draft, &version); err != nil {
			t.Fatal(err)
		}
		if learner != stranger || source != src || version != 1 || draft == "" {
			t.Fatalf("pin wrong: %s %s %s %d", learner, source, draft, version)
		}
		if n := count(t, f, `SELECT count(*) FROM courses WHERE id=$1 AND learner_id=$2 AND visibility='private'`, got.CourseID, stranger); n != 1 {
			t.Fatal("copy must be private and learner-owned")
		}
		if n := count(t, f, `SELECT count(*) FROM course_enrollments WHERE course_id=$1 AND learner_id=$2`, got.CourseID, stranger); n != 1 {
			t.Fatal("learner must be enrolled in the copy")
		}
		if st := stubStates(t, f, got.CourseID); strings.Join(st, ",") != "current,locked,locked" {
			t.Fatalf("gate states %v", st)
		}
		if n := count(t, f, `SELECT count(*) FROM generated_lessons WHERE course_id=$1 AND learner_id=$2`, got.CourseID, stranger); n != 2 {
			t.Fatalf("generated lessons copied: %d", n)
		}
		if n := count(t, f, `SELECT count(*) FROM lesson_manifests m JOIN course_lesson_stubs l ON l.id=m.lesson_id JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1 AND m.learner_id=$2`, got.CourseID, stranger); n != 2 {
			t.Fatalf("manifests: %d", n)
		}
		all := objects.all()
		for _, banned := range []string{"SECRET-EXPLANATION", "answer_index", "Which one?", "Wrong"} {
			if strings.Contains(all, banned) {
				t.Fatalf("manifest objects leak %q", banned)
			}
		}
		// Zero credits: nothing quoted, reserved or generated for the learner.
		for _, q := range []string{`SELECT count(*) FROM pricing_quotes WHERE learner_id=$1`, `SELECT count(*) FROM credit_jobs WHERE learner_id=$1`, `SELECT count(*) FROM lesson_generations WHERE learner_id=$1`} {
			if n := count(t, f, q, stranger); n != 0 {
				t.Fatalf("%s = %d, want 0", q, n)
			}
		}
		if n := len(ev.named("course_enrolled_published")); n != 1 {
			t.Fatalf("course_enrolled_published emitted %d times", n)
		}
		props := ev.named("course_enrolled_published")[0]
		raw, _ := json.Marshal(props)
		if strings.Contains(string(raw), token) || !strings.Contains(string(raw), `"link"`) {
			t.Fatalf("event props wrong: %s", raw)
		}
	})

	t.Run("enrolling_twice_is_idempotent", func(t *testing.T) {
		again, err := s.Enroll(ctx, stranger, token)
		if err != nil || again.Created || again.CourseID != firstCopy {
			t.Fatalf("second enroll: %v %+v", err, again)
		}
		if n := count(t, f, `SELECT count(*) FROM course_copies WHERE learner_id=$1 AND source_course_id=$2`, stranger, src); n != 1 {
			t.Fatalf("copies: %d", n)
		}
		if n := len(ev.named("course_enrolled_published")); n != 1 {
			t.Fatalf("event emitted again: %d", n)
		}
	})

	t.Run("concurrent_enrolls_make_one_copy", func(t *testing.T) {
		fresh := newLearner(t, f, "Racer")
		var wg sync.WaitGroup
		ids := make([]string, 6)
		for i := range ids {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if got, err := s.Enroll(ctx, fresh, token); err == nil {
					ids[i] = got.CourseID
				}
			}()
		}
		wg.Wait()
		for _, id := range ids {
			if id == "" || id != ids[0] {
				t.Fatalf("racing enrolls returned %v", ids)
			}
		}
		if n := count(t, f, `SELECT count(*) FROM course_copies WHERE learner_id=$1`, fresh); n != 1 {
			t.Fatalf("copies after race: %d", n)
		}
	})

	t.Run("unlock_requires_the_predecessor_pass_and_costs_nothing", func(t *testing.T) {
		lessons := lessontest.New(lessontest.Options{Pool: f.DB.Pool, Events: ev})
		call := func(method, path string, body any) (int, map[string]any) {
			var r bytes.Buffer
			if body != nil {
				_ = json.NewEncoder(&r).Encode(body)
			}
			req := httptest.NewRequest(method, path, &r).WithContext(accounts.WithLearnerID(ctx, stranger))
			rec := httptest.NewRecorder()
			lessons.ServeHTTP(rec, req)
			var out map[string]any
			_ = json.Unmarshal(rec.Body.Bytes(), &out)
			return rec.Code, out
		}
		var l1, l2 string
		if err := f.DB.Pool.QueryRow(ctx, `SELECT (SELECT l.id::text FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1 AND l.position=0),(SELECT l.id::text FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1 AND l.position=1)`, firstCopy).Scan(&l1, &l2); err != nil {
			t.Fatal(err)
		}
		// Lesson 2 is gated: no test session before lesson 1 is passed.
		if code, _ := call(http.MethodGet, "/v1/lessons/"+l2+"/test", nil); code < 400 {
			t.Fatalf("locked reused lesson started a test: %d", code)
		}
		code, started := call(http.MethodGet, "/v1/lessons/"+l1+"/test", nil)
		if code != http.StatusOK {
			t.Fatalf("start test: %d %v", code, started)
		}
		for _, q := range started["questions"].([]any) {
			id := q.(map[string]any)["question_id"]
			if code, out := call(http.MethodPost, "/v1/lessons/"+l1+"/test/answer", map[string]any{"question_id": id, "choice_index": 0}); code != http.StatusOK {
				t.Fatalf("answer: %d %v", code, out)
			}
		}
		if st := stubStates(t, f, firstCopy); strings.Join(st, ",") != "done,current,locked" {
			t.Fatalf("after pass: %v", st)
		}
		if n := count(t, f, `SELECT count(*) FROM credit_jobs WHERE learner_id=$1`, stranger); n != 0 {
			t.Fatalf("credits touched: %d", n)
		}
		if n := len(ev.named("published_lesson_reused")); n < 1 {
			t.Fatal("published_lesson_reused not emitted on unlock")
		}
		for _, e := range ev.named("published_lesson_reused") {
			raw, _ := json.Marshal(e)
			if strings.Contains(string(raw), "<h1>") || strings.Contains(string(raw), "Phases") {
				t.Fatalf("event leaks content: %s", raw)
			}
		}
	})

	t.Run("state_is_learner_specific_and_creator_sees_counts_only", func(t *testing.T) {
		other := newLearner(t, f, "Other")
		got, err := s.Enroll(ctx, other, token)
		if err != nil || !got.Created || got.CourseID == firstCopy {
			t.Fatalf("second learner: %v %+v", err, got)
		}
		if st := stubStates(t, f, got.CourseID); strings.Join(st, ",") != "current,locked,locked" {
			t.Fatalf("other learner inherited progress: %v", st)
		}
		if n := count(t, f, `SELECT count(*) FROM lesson_completion_outbox WHERE learner_id=$1`, other); n != 0 {
			t.Fatal("progress leaked between learners")
		}
		if n := count(t, f, `SELECT count(*) FROM course_enrollments WHERE course_id=$1 AND learner_id=$2`, firstCopy, owner); n != 0 {
			t.Fatal("owner must not be enrolled in a learner copy")
		}
		p, err := s.GetPreview(ctx, token, "")
		if err != nil || p.EnrollmentCount < 3 {
			t.Fatalf("aggregate count: %v %d", err, p.EnrollmentCount)
		}
		// The owner's own course is untouched.
		if st := stubStates(t, f, src); strings.Join(st, ",") != "done,done,locked" {
			t.Fatalf("source changed: %v", st)
		}
	})

	t.Run("copies_stay_pinned_when_the_owner_edits_and_republishes", func(t *testing.T) {
		if _, err := f.DB.Pool.Exec(ctx, `UPDATE lesson_slides SET html='<h1>Changed</h1>' WHERE lesson_stub_id IN (SELECT l.id FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1)`, src); err != nil {
			t.Fatal(err)
		}
		if d, err := s.BuildDraft(ctx, owner, src); err != nil || d.Number != 2 {
			t.Fatalf("v2 draft: %v %+v", err, d)
		}
		late := newLearner(t, f, "Late")
		got, err := s.Enroll(ctx, late, token)
		if err != nil || got.Version != 2 {
			t.Fatalf("new learners pin the latest version: %v %+v", err, got)
		}
		if n := count(t, f, `SELECT count(*) FROM lesson_slides s JOIN course_lesson_stubs l ON l.id=s.lesson_stub_id JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1 AND s.html LIKE '%Changed%'`, firstCopy); n != 0 {
			t.Fatal("existing copy changed after republish")
		}
		var v int
		_ = f.DB.Pool.QueryRow(ctx, `SELECT version FROM course_copies WHERE copy_course_id=$1`, firstCopy).Scan(&v)
		if v != 1 {
			t.Fatalf("existing copy repinned to %d", v)
		}
	})
}

func TestEnrollRefusalsAreNeutral(t *testing.T) {
	f := backuptest.New(t)
	f.Seed(t)
	ctx := t.Context()
	ev := &capture{}
	s, _ := enrollService(t, f, ev, func(c *sharing.Config) { c.MaxEnrollmentsPerDay = 2 })
	src := seedReusable(t, f, "Refusals")
	token := publish(t, s, src)

	kept, err := s.Enroll(ctx, stranger, token)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RevokeLink(ctx, owner, src); err != nil {
		t.Fatal(err)
	}
	fresh := newLearner(t, f, "Fresh")
	for name, tok := range map[string]string{"revoked": token, "unknown": strings.Repeat("A", 27), "empty": ""} {
		if _, err = s.Enroll(ctx, fresh, tok); !errors.Is(err, sharing.ErrLinkNotFound) {
			t.Fatalf("%s link: got %v, want ErrLinkNotFound", name, err)
		}
	}
	if n := count(t, f, `SELECT count(*) FROM course_copies WHERE learner_id=$1`, fresh); n != 0 {
		t.Fatal("refused enrollment created a copy")
	}
	// Existing copies keep working after revocation: same rows, still enrolled, still playable.
	if n := count(t, f, `SELECT count(*) FROM course_enrollments e JOIN courses c ON c.id=e.course_id WHERE e.course_id=$1 AND e.learner_id=$2 AND c.deleted_at IS NULL`, kept.CourseID, stranger); n != 1 {
		t.Fatal("revoking removed an existing copy")
	}

	t.Run("blocked_viewer_gets_the_same_refusal", func(t *testing.T) {
		src2 := seedReusable(t, f, "Blocked")
		tok2 := publish(t, s, src2)
		seedBlockedLearner(t, f, stranger2, owner)
		if _, err := s.Enroll(ctx, stranger2, tok2); !errors.Is(err, sharing.ErrLinkNotFound) {
			t.Fatalf("blocked enroll: %v", err)
		}
		if n := count(t, f, `SELECT count(*) FROM course_copies WHERE learner_id=$1`, stranger2); n != 0 {
			t.Fatal("blocked viewer got a copy")
		}
	})

	t.Run("owner_opening_their_own_link_gets_their_course", func(t *testing.T) {
		src3 := seedReusable(t, f, "Mine")
		tok3 := publish(t, s, src3)
		got, err := s.Enroll(ctx, owner, tok3)
		if err != nil || got.Created || got.CourseID != src3 {
			t.Fatalf("owner enroll: %v %+v", err, got)
		}
		if n := count(t, f, `SELECT count(*) FROM course_copies WHERE learner_id=$1`, owner); n != 0 {
			t.Fatal("owner got a copy of their own course")
		}
	})

	t.Run("daily_enrollment_limit", func(t *testing.T) {
		busy := newLearner(t, f, "Busy")
		var toks []string
		for _, name := range []string{"Limit A", "Limit B", "Limit C"} {
			toks = append(toks, publish(t, s, seedReusable(t, f, name)))
		}
		for i := 0; i < 2; i++ {
			if _, err := s.Enroll(ctx, busy, toks[i]); err != nil {
				t.Fatalf("enroll %d: %v", i, err)
			}
		}
		if _, err := s.Enroll(ctx, busy, toks[2]); !errors.Is(err, sharing.ErrRateLimited) {
			t.Fatalf("limit: got %v, want ErrRateLimited", err)
		}
		// An existing copy is still returned at the limit.
		if got, err := s.Enroll(ctx, busy, toks[0]); err != nil || got.Created {
			t.Fatalf("existing at limit: %v %+v", err, got)
		}
	})

	t.Run("owner_deleting_the_course_keeps_the_copy_working", func(t *testing.T) {
		src4 := seedReusable(t, f, "Deleted")
		tok4 := publish(t, s, src4)
		who := newLearner(t, f, "Keeper")
		got, err := s.Enroll(ctx, who, tok4)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.DB.Pool.Exec(ctx, `DELETE FROM courses WHERE id=$1`, src4); err != nil {
			t.Fatal(err)
		}
		if n := count(t, f, `SELECT count(*) FROM course_copies WHERE copy_course_id=$1 AND source_course_id IS NULL AND version=1`, got.CourseID); n != 1 {
			t.Fatal("copy lost its pin when the source was deleted")
		}
		if n := count(t, f, `SELECT count(*) FROM generated_lessons WHERE course_id=$1`, got.CourseID); n != 2 {
			t.Fatal("copy lost lessons when the source was deleted")
		}
	})
}
