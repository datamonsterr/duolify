//go:build integration

package sharing_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/lessontest"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
)

// lessonDouble is an in-process EngineService GenerateLesson double on bufconn; it never builds
// or launches the Rust engine. `down` simulates every provider failing.
type lessonDouble struct {
	pb.UnimplementedEngineServiceServer
	calls atomic.Int32
	down  atomic.Bool
}

func (d *lessonDouble) GenerateLesson(r *pb.GenerateLessonRequest, s grpc.ServerStreamingServer[pb.GenerateLessonEvent]) error {
	d.calls.Add(1)
	if d.down.Load() {
		return status.Error(14, "all providers failed")
	}
	usage := &pb.Usage{Provider: "fixture", Model: "fixture", Attempt: 1, Delivered: true, InputTokens: 500, OutputTokens: 200, LatencyMs: 40, Component: "content"}
	lesson := &pb.Lesson{
		Slides: []*pb.Slide{{SlideId: "g1", Html: `<div id="root"><h1>Eclipses</h1><p>Shadows line up.</p></div>`, DurationMs: 3000, Narration: "Shadows line up.", TopicIds: []string{"eclipse"}}},
		Test:   []*pb.Question{{QuestionId: "gq1", TopicId: "eclipse", Prompt: "What lines up?", Choices: []string{"Shadows", "Clouds"}, AnswerIndex: 0, Explanation: "GEN-SECRET"}},
		VariantPool: []*pb.TopicVariants{{TopicId: "eclipse", Variants: []*pb.Question{
			{QuestionId: "gv1", TopicId: "eclipse", VariantOf: "gq1", Prompt: "Alignment of?", Choices: []string{"Shadows", "Wind"}, AnswerIndex: 0, Explanation: "x"},
			{QuestionId: "gv2", TopicId: "eclipse", VariantOf: "gq1", Prompt: "Eclipses need?", Choices: []string{"Alignment", "Rain"}, AnswerIndex: 0, Explanation: "y"},
		}}},
	}
	if err := s.Send(&pb.GenerateLessonEvent{Event: &pb.GenerateLessonEvent_Lesson{Lesson: lesson}}); err != nil {
		return err
	}
	return s.Send(&pb.GenerateLessonEvent{Event: &pb.GenerateLessonEvent_Usage{Usage: usage}})
}

// rpcLessons adapts the generated client to lessontest.LessonEngine like the production client.
type rpcLessons struct{ c pb.EngineServiceClient }

func (r rpcLessons) GenerateLesson(ctx context.Context, req *pb.GenerateLessonRequest) (*pb.Lesson, []*pb.Usage, error) {
	stream, err := r.c.GenerateLesson(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	var lesson *pb.Lesson
	var attempts []*pb.Usage
	for {
		ev, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, attempts, err
		}
		switch e := ev.Event.(type) {
		case *pb.GenerateLessonEvent_Lesson:
			lesson = e.Lesson
		case *pb.GenerateLessonEvent_Usage:
			attempts = append(attempts, e.Usage)
		}
	}
	if lesson == nil {
		return nil, attempts, errors.New("no lesson delivered")
	}
	return lesson, attempts, nil
}

func startLessonEngine(t *testing.T) (*lessonDouble, lessontest.LessonEngine) {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	d := &lessonDouble{}
	pb.RegisterEngineServiceServer(server, d)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///lessons", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return d, rpcLessons{pb.NewEngineServiceClient(conn)}
}

type genEnv struct {
	f       *backuptest.Fixture
	ev      *capture
	s       *sharing.Service
	lessons *lessontest.Service
	fin     *credits.Service
	engine  *lessonDouble
	token   string
	course  string
}

func newGenEnv(t *testing.T) *genEnv {
	t.Helper()
	ctx := t.Context()
	f := backuptest.New(t)
	ev := &capture{}
	consent(t, f, owner, false)
	cat, err := pricing.Load(ctx, f.DB.Queries)
	if err != nil {
		t.Fatal(err)
	}
	fin := credits.NewServiceWithCatalogue(f.DB.Pool, nil, cat, analytics.WithConsent(ev, analytics.DatabaseConsent(f.DB.Pool)))
	double, engine := startLessonEngine(t)
	lessons := lessontest.New(lessontest.Options{Pool: f.DB.Pool, Engine: engine, Financial: fin, Events: analytics.WithConsent(ev, analytics.DatabaseConsent(f.DB.Pool))})
	s, _ := enrollService(t, f, ev, nil)
	s = s.WithGeneration(sharing.GenerationDeps{Quotes: pricing.NewQuotes(f.DB.Pool), Wallet: fin, Lessons: lessons})
	course := seedReusable(t, f, "Astronomy")
	return &genEnv{f: f, ev: ev, s: s, lessons: lessons, fin: fin, engine: double, token: publish(t, s, course), course: course}
}

// learnerAtTheEnd enrolls a new learner and passes both published lessons.
func (e *genEnv) learnerAtTheEnd(t *testing.T, name string) (learner, copyID string) {
	t.Helper()
	learner = newLearner(t, e.f, name)
	got, err := e.s.Enroll(t.Context(), learner, e.token)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range e.lessonIDs(t, got.CourseID)[:2] {
		e.pass(t, learner, id)
	}
	return learner, got.CourseID
}

func (e *genEnv) lessonIDs(t *testing.T, course string) []string {
	t.Helper()
	rows, err := e.f.DB.Pool.Query(t.Context(), `SELECT l.id::text FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1 ORDER BY u.position, l.position`, course)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func (e *genEnv) call(t *testing.T, learner, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var r bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&r).Encode(body)
	}
	req := httptest.NewRequestWithContext(accounts.WithLearnerID(t.Context(), learner), method, path, &r)
	rec := httptest.NewRecorder()
	e.lessons.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func (e *genEnv) pass(t *testing.T, learner, lesson string) {
	t.Helper()
	code, started := e.call(t, learner, http.MethodGet, "/v1/lessons/"+lesson+"/test", nil)
	if code != http.StatusOK {
		t.Fatalf("start test: %d %v", code, started)
	}
	for _, q := range started["questions"].([]any) {
		if code, out := e.call(t, learner, http.MethodPost, "/v1/lessons/"+lesson+"/test/answer", map[string]any{"question_id": q.(map[string]any)["question_id"], "choice_index": 0}); code != http.StatusOK {
			t.Fatalf("answer: %d %v", code, out)
		}
	}
}

func (e *genEnv) fund(t *testing.T, learner string, amount int) {
	t.Helper()
	if _, err := e.f.DB.Pool.Exec(t.Context(), `INSERT INTO credit_entries(learner_id, amount, kind, reference) VALUES($1,$2,'purchase','e15-04-fund-'||$1::text)`, learner, amount); err != nil {
		t.Fatal(err)
	}
	_ = e.fin.InvalidateCache(t.Context(), learner)
}

func (e *genEnv) available(t *testing.T, learner string) int64 {
	t.Helper()
	w, err := e.fin.GetWallet(t.Context(), learner)
	if err != nil {
		t.Fatal(err)
	}
	return w.Available
}

func TestGenerateMissingPublishedLessonsIntegration(t *testing.T) {
	e := newGenEnv(t)
	ctx, pool := t.Context(), e.f.DB.Pool

	var hashBefore string
	if err := pool.QueryRow(ctx, `SELECT content_hash FROM publication_drafts WHERE course_id=$1 AND verdict='pass' ORDER BY number DESC LIMIT 1`, e.course).Scan(&hashBefore); err != nil {
		t.Fatal(err)
	}

	// A learner who has not passed the last published lesson cannot even get a quote.
	early := newLearner(t, e.f, "Early")
	earlyCopy, err := e.s.Enroll(ctx, early, e.token)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("gate_previous_pass_comes_first", func(t *testing.T) {
		st, err := e.s.MissingStatus(ctx, early, earlyCopy.CourseID)
		if err != nil || st.Ready != 2 || st.Total != 3 || st.State != sharing.StateLocked {
			t.Fatalf("status: %+v %v", st, err)
		}
		if _, err = e.s.QuoteMissing(ctx, early, earlyCopy.CourseID); !errors.Is(err, sharing.ErrPreviousNotPassed) {
			t.Fatalf("quote before pass: %v", err)
		}
		if n := count(t, e.f, `SELECT count(*) FROM pricing_quotes WHERE learner_id=$1 AND snapshot->>'action'='lesson_content'`, early); n != 0 {
			t.Fatalf("a quote was stored: %d", n)
		}
	})

	learner, copyID := e.learnerAtTheEnd(t, "Reader")
	third := e.lessonIDs(t, copyID)[2]

	t.Run("status_shows_ready_over_total_and_a_current_price_version_quote", func(t *testing.T) {
		st, err := e.s.MissingStatus(ctx, learner, copyID)
		if err != nil || st.Ready != 2 || st.Total != 3 || st.Missing != 1 || st.State != sharing.StateReady || st.NextLessonID != third {
			t.Fatalf("status: %+v %v", st, err)
		}
		q, err := e.s.QuoteMissing(ctx, learner, copyID)
		if err != nil {
			t.Fatal(err)
		}
		if q.PricingVersion == "" {
			t.Fatal("quote has no price version")
		}
		if q.LessonID != third || q.ContentCredits <= 0 || q.AudioCredits < 0 || q.TotalCredits != q.ContentCredits+q.AudioCredits || q.Sufficient {
			t.Fatalf("quote: %+v", q)
		}
	})

	t.Run("refusals_happen_before_the_engine_and_charge_nothing", func(t *testing.T) {
		q, err := e.s.QuoteMissing(ctx, learner, copyID)
		if err != nil {
			t.Fatal(err)
		}
		in := sharing.GenerateMissingInput{LessonID: third, QuoteID: q.QuoteID, Consent: false}
		if _, err = e.s.GenerateMissing(ctx, learner, copyID, in); !errors.Is(err, sharing.ErrConsentRequired) {
			t.Fatalf("no consent: %v", err)
		}
		in.Consent = true
		if _, err = e.s.GenerateMissing(ctx, learner, copyID, in); !errors.Is(err, sharing.ErrInsufficientCredits) {
			t.Fatalf("poor learner: %v", err)
		}
		// Somebody else's copy, the owner's source course and an unknown id are neutral.
		for _, c := range []struct{ who, course string }{{early, copyID}, {owner, e.course}, {learner, e.course}, {learner, "not-a-uuid"}} {
			if _, err = e.s.GenerateMissing(ctx, c.who, c.course, in); !errors.Is(err, sharing.ErrCopyNotFound) {
				t.Fatalf("%s on %s: %v", c.who, c.course, err)
			}
		}
		// Expired, foreign and superseded quotes are stale.
		if _, err = pool.Exec(ctx, `UPDATE pricing_quotes SET expires_at=now()-interval '1 minute', created_at=now()-interval '2 minutes' WHERE id=$1`, q.QuoteID); err != nil {
			t.Fatal(err)
		}
		if _, err = e.s.GenerateMissing(ctx, learner, copyID, in); !errors.Is(err, sharing.ErrQuoteStale) {
			t.Fatalf("expired quote: %v", err)
		}
		in.QuoteID = "00000000-0000-4000-8000-000000000000"
		if _, err = e.s.GenerateMissing(ctx, learner, copyID, in); !errors.Is(err, sharing.ErrQuoteStale) {
			t.Fatalf("unknown quote: %v", err)
		}
		if got := e.engine.calls.Load(); got != 0 {
			t.Fatalf("engine called %d times by refused requests", got)
		}
		for _, table := range []string{"lesson_generations", "credit_jobs", "copy_generation_consents"} {
			if n := count(t, e.f, `SELECT count(*) FROM `+table+` WHERE learner_id=$1`, learner); n != 0 {
				t.Fatalf("%s rows after refusals: %d", table, n)
			}
		}
		if n := count(t, e.f, `SELECT count(*) FROM river_job WHERE kind='lesson_generation'`); n != 0 {
			t.Fatalf("a job was queued: %d", n)
		}
	})

	t.Run("a_superseded_price_version_is_stale", func(t *testing.T) {
		e.fund(t, learner, 3000)
		q, err := e.s.QuoteMissing(ctx, learner, copyID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `UPDATE pricing_quotes SET snapshot=jsonb_set(snapshot,'{pricing_version}','"retired-version"') WHERE id=$1`, q.QuoteID); err != nil {
			t.Fatal(err)
		}
		_, err = e.s.GenerateMissing(ctx, learner, copyID, sharing.GenerateMissingInput{LessonID: third, QuoteID: q.QuoteID, Consent: true})
		if !errors.Is(err, sharing.ErrQuoteStale) {
			t.Fatalf("superseded: %v", err)
		}
	})

	t.Run("failure_releases_the_reservation_and_a_retry_succeeds", func(t *testing.T) {
		before := e.available(t, learner)
		e.engine.down.Store(true)
		q, err := e.s.QuoteMissing(ctx, learner, copyID)
		if err != nil || !q.Sufficient {
			t.Fatalf("quote: %+v %v", q, err)
		}
		gen, err := e.s.GenerateMissing(ctx, learner, copyID, sharing.GenerateMissingInput{LessonID: third, QuoteID: q.QuoteID, Consent: true})
		if err != nil {
			t.Fatal(err)
		}
		_ = e.lessons.RunLessonGeneration(ctx, gen.ID) // terminal failure; the error is the cause
		var state, code string
		if err := pool.QueryRow(ctx, `SELECT state, COALESCE(error_code,'') FROM lesson_generations WHERE id=$1`, gen.ID).Scan(&state, &code); err != nil || state != "failed" {
			t.Fatalf("generation: %s %s %v", state, code, err)
		}
		if got := e.available(t, learner); got != before {
			t.Fatalf("learner charged for a failed generation: %d -> %d", before, got)
		}
		if n := count(t, e.f, `SELECT count(*) FROM credit_jobs WHERE learner_id=$1 AND state IN ('reserved','running')`, learner); n != 0 {
			t.Fatalf("reservation left open: %d", n)
		}
		if n := count(t, e.f, `SELECT count(*) FROM generated_lessons WHERE lesson_stub_id=$1`, third); n != 0 {
			t.Fatal("failed generation stored content")
		}
		ids := e.lessonIDs(t, copyID)
		var s1, s3 string
		_ = pool.QueryRow(ctx, `SELECT (SELECT status FROM course_lesson_stubs WHERE id=$1),(SELECT status FROM course_lesson_stubs WHERE id=$2)`, ids[0], third).Scan(&s1, &s3)
		if s1 != "done" || s3 != "ready_to_generate" {
			t.Fatalf("stub states %s/%s", s1, s3)
		}
	})

	t.Run("consented_generation_is_queued_then_settled_to_the_learner_only", func(t *testing.T) {
		e.engine.down.Store(false)
		callsBefore := e.engine.calls.Load()
		before := e.available(t, learner)
		q, err := e.s.QuoteMissing(ctx, learner, copyID)
		if err != nil || !q.Sufficient || q.Balance != before {
			t.Fatalf("quote: %+v %v", q, err)
		}
		in := sharing.GenerateMissingInput{LessonID: third, QuoteID: q.QuoteID, Consent: true}
		gen, err := e.s.GenerateMissing(ctx, learner, copyID, in)
		if err != nil || gen.ID == "" {
			t.Fatalf("generate: %+v %v", gen, err)
		}
		// River gating: queued, not started; a duplicate request is refused.
		if n := count(t, e.f, `SELECT count(*) FROM river_job WHERE kind='lesson_generation' AND args->>'id'=$1`, gen.ID); n != 1 {
			t.Fatalf("river jobs: %d", n)
		}
		if e.engine.calls.Load() != callsBefore {
			t.Fatal("engine called before the worker ran")
		}
		if _, err = e.s.GenerateMissing(ctx, learner, copyID, in); !errors.Is(err, sharing.ErrGenerationInProgress) {
			t.Fatalf("duplicate: %v", err)
		}
		st, _ := e.s.MissingStatus(ctx, learner, copyID)
		if st.State != sharing.StateGenerating {
			t.Fatalf("status while queued: %+v", st)
		}
		var consentQuote, consentVersion string
		if err := pool.QueryRow(ctx, `SELECT quote_id::text, pricing_version FROM copy_generation_consents WHERE generation_id=$1 AND learner_id=$2 AND copy_course_id=$3 AND lesson_id=$4`, gen.ID, learner, copyID, third).Scan(&consentQuote, &consentVersion); err != nil || consentQuote != q.QuoteID || consentVersion != q.PricingVersion {
			t.Fatalf("consent record: %s %s %v", consentQuote, consentVersion, err)
		}

		if err := e.lessons.RunLessonGeneration(ctx, gen.ID); err != nil {
			t.Fatal(err)
		}
		if got := e.engine.calls.Load() - callsBefore; got != 1 {
			t.Fatalf("engine calls: %d", got)
		}
		var stubStatus string
		if err := pool.QueryRow(ctx, `SELECT status FROM course_lesson_stubs WHERE id=$1`, third).Scan(&stubStatus); err != nil || stubStatus != "current" {
			t.Fatalf("stub after settle: %s %v", stubStatus, err)
		}
		if n := count(t, e.f, `SELECT count(*) FROM generated_lessons WHERE lesson_stub_id=$1 AND course_id=$2 AND learner_id=$3`, third, copyID, learner); n != 1 {
			t.Fatalf("copy-local lesson rows: %d", n)
		}
		// Reservation ownership: the generating learner's job and spend, nobody else's.
		if n := count(t, e.f, `SELECT count(*) FROM credit_jobs WHERE learner_id=$1 AND state='settled'`, learner); n != 1 {
			t.Fatalf("learner settled jobs: %d", n)
		}
		if n := count(t, e.f, `SELECT count(*) FROM credit_jobs WHERE learner_id<>$1`, learner); n != 0 {
			t.Fatalf("other accounts reserved: %d", n)
		}
		if n := count(t, e.f, `SELECT count(*) FROM credit_entries WHERE learner_id=$1 AND amount<>0`, owner); n != 0 {
			t.Fatalf("flow-back or owner charge: %d", n)
		}
		if got := e.available(t, learner); got >= before {
			t.Fatalf("balance did not drop: %d -> %d", before, got)
		}
	})

	t.Run("snapshot_owner_course_and_other_copies_are_unchanged", func(t *testing.T) {
		var hashAfter string
		if err := pool.QueryRow(ctx, `SELECT content_hash FROM publication_drafts WHERE course_id=$1 AND verdict='pass' ORDER BY number DESC LIMIT 1`, e.course).Scan(&hashAfter); err != nil || hashAfter != hashBefore {
			t.Fatalf("published hash changed: %s -> %s %v", hashBefore, hashAfter, err)
		}
		if n := count(t, e.f, `SELECT count(*) FROM generated_lessons g JOIN course_lesson_stubs l ON l.id=g.lesson_stub_id JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1`, e.course); n != 2 {
			t.Fatalf("owner course generated lessons: %d", n)
		}
		var stillMissing int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1 AND NOT EXISTS(SELECT 1 FROM generated_lessons g WHERE g.lesson_stub_id=l.id)`, earlyCopy.CourseID).Scan(&stillMissing)
		if stillMissing != 1 {
			t.Fatalf("another copy gained content: %d missing", stillMissing)
		}
		var drafts int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM publication_drafts WHERE course_id=$1`, e.course).Scan(&drafts)
		if drafts != 1 {
			t.Fatalf("a forked update appeared: %d drafts", drafts)
		}
	})

	t.Run("analytics_are_uuid_only_and_respect_opt_out", func(t *testing.T) {
		events := e.ev.named("published_lesson_generated_on_demand")
		if len(events) != 1 {
			t.Fatalf("events: %d", len(events))
		}
		ev := events[0]
		if ev["_id"] != learner || ev["course_id"] != copyID || ev["lesson_id"] != third || ev["version"] == nil {
			t.Fatalf("event: %v", ev)
		}
		raw, _ := json.Marshal(ev)
		for _, banned := range []string{"Eclipses", "Shadows", "<h1>", "GEN-SECRET", "Explain eclipses"} {
			if strings.Contains(string(raw), banned) {
				t.Fatalf("event leaks %q: %s", banned, raw)
			}
		}
		if len(e.ev.named("lesson_generated")) < 1 || len(e.ev.named("$ai_generation")) < 1 {
			t.Fatal("reused pipeline events missing")
		}

		quiet, quietCopy := e.learnerAtTheEnd(t, "Quiet")
		consent(t, e.f, quiet, true)
		e.fund(t, quiet, 3000)
		q, err := e.s.QuoteMissing(ctx, quiet, quietCopy)
		if err != nil {
			t.Fatal(err)
		}
		gen, err := e.s.GenerateMissing(ctx, quiet, quietCopy, sharing.GenerateMissingInput{LessonID: e.lessonIDs(t, quietCopy)[2], QuoteID: q.QuoteID, Consent: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := e.lessons.RunLessonGeneration(ctx, gen.ID); err != nil {
			t.Fatal(err)
		}
		if got := len(e.ev.named("published_lesson_generated_on_demand")); got != 1 {
			t.Fatalf("opt-out ignored: %d events", got)
		}
	})
}
