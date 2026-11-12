//go:build integration

package chat_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/chat"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	backend "github.com/datamonsterr/duolify/services/api/internal/engine"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// tutorEngine scripts the Engine by the last learner message.
type tutorEngine struct {
	enginev1.UnimplementedEngineServiceServer
	mu       sync.Mutex
	requests []*enginev1.ChatRequest
	calls    atomic.Int32
}

func (e *tutorEngine) last() *enginev1.ChatRequest {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.requests[len(e.requests)-1]
}

func (e *tutorEngine) Chat(r *enginev1.ChatRequest, stream enginev1.EngineService_ChatServer) error {
	e.calls.Add(1)
	e.mu.Lock()
	e.requests = append(e.requests, r)
	e.mu.Unlock()
	text := r.Messages[len(r.Messages)-1].Content
	send := func(s string) error {
		return stream.Send(&enginev1.ChatEvent{Event: &enginev1.ChatEvent_Delta{Delta: s}})
	}
	switch {
	case strings.HasPrefix(text, "fail-first"):
		return status.Error(codes.Unavailable, "provider down")
	case strings.HasPrefix(text, "interrupt"):
		if err := send("Moons "); err != nil {
			return err
		}
		if err := send("orbit "); err != nil {
			return err
		}
		return status.Error(codes.Unavailable, "provider died")
	}
	if strings.HasPrefix(text, "slow") {
		time.Sleep(150 * time.Millisecond)
	}
	for _, part := range []string{"Planets ", "orbit ", "stars."} {
		if err := send(part); err != nil {
			return err
		}
	}
	return stream.Send(&enginev1.ChatEvent{Event: &enginev1.ChatEvent_Usage{Usage: &enginev1.Usage{Provider: r.Limits.Provider, Model: r.Limits.Model, InputTokens: 120, OutputTokens: 30}}})
}

type recorder struct {
	mu     sync.Mutex
	events []map[string]any
}

func (r *recorder) Capture(learnerID, event string, p map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := map[string]any{"_event": event, "_learner": learnerID}
	for k, v := range p {
		cp[k] = v
	}
	r.events = append(r.events, cp)
	return nil
}
func (r *recorder) Close() error { return nil }
func (r *recorder) named(name string) []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []map[string]any
	for _, e := range r.events {
		if e["_event"] == name {
			out = append(out, e)
		}
	}
	return out
}

type sseEvent struct {
	Name string
	Data map[string]any
}

func parseSSE(raw string) []sseEvent {
	var out []sseEvent
	for _, block := range strings.Split(strings.TrimSpace(raw), "\n\n") {
		var e sseEvent
		for _, line := range strings.Split(block, "\n") {
			if v, ok := strings.CutPrefix(line, "event: "); ok {
				e.Name = v
			}
			if v, ok := strings.CutPrefix(line, "data: "); ok {
				_ = json.Unmarshal([]byte(v), &e.Data)
			}
		}
		if e.Name != "" {
			out = append(out, e)
		}
	}
	return out
}

func TestStreamAQuotedCourseTutorReplyIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("tutor"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	defer db.Close()
	mail := new(accounts.MailSink)
	auth, err := accounts.New(db.Pool, strings.Repeat("test-key", 5), mail)
	if err != nil {
		t.Fatal(err)
	}
	lis, err := new(net.ListenConfig).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fixture := new(tutorEngine)
	rpc := grpc.NewServer()
	enginev1.RegisterEngineServiceServer(rpc, fixture)
	go func() { _ = rpc.Serve(lis) }()
	defer rpc.Stop()
	engine, err := backend.New("http://"+lis.Addr().String(), 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = engine.Close() }()
	events := new(recorder)
	cat, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	financial := credits.NewServiceWithCatalogue(db.Pool, nil, cat, events)
	courseService := courses.New(courses.Options{Pool: db.Pool, Environment: "test", Financial: financial, Events: events})
	tutor := chat.New(chat.Options{Pool: db.Pool, Engine: engine, Financial: financial, Profiles: courseService, Events: events})
	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{Accounts: auth, Quotes: pricing.NewQuotes(db.Pool), Credits: financial, Tutor: tutor}, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()

	call := func(method, path string, body any, token string) (int, string) {
		t.Helper()
		raw, _ := json.Marshal(body)
		req, err := http.NewRequestWithContext(ctx, method, server.URL+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		out, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(out)
	}
	asMap := func(raw string) map[string]any {
		var m map[string]any
		_ = json.Unmarshal([]byte(raw), &m)
		return m
	}
	login := func(email string) (string, string) {
		if code, _ := call("POST", "/v1/auth/code", map[string]any{"email": email, "age_confirmed": true}, ""); code != 202 {
			t.Fatal(code)
		}
		code, raw := call("POST", "/v1/auth/verify", map[string]any{"email": email, "age_confirmed": true, "code": mail.Code(email)}, "")
		if code != 200 {
			t.Fatal(code, raw)
		}
		token := asMap(raw)["access_token"].(string)
		claims, err := auth.ParseAccess(token)
		if err != nil {
			t.Fatal(err)
		}
		return token, claims.Subject
	}
	fund := func(owner string, amount int) {
		t.Helper()
		if _, err := db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version) VALUES($1,$2,'purchase',$3,$4)`, owner, amount, "fund-"+owner+fmt.Sprint(time.Now().UnixNano()), cat.Config().Version); err != nil {
			t.Fatal(err)
		}
	}
	// drain spends the learner's daily free grant so only purchased credits remain.
	drain := func(owner string) {
		t.Helper()
		if _, err := financial.EnsureDailyGrant(ctx, owner, time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version,provenance) SELECT $1,-COALESCE(sum(amount),0),'spend','drain',$2,jsonb_build_object('bucket','daily_free','date',to_char(now() AT TIME ZONE 'UTC','YYYY-MM-DD')) FROM credit_entries WHERE learner_id=$1`, owner, cat.Config().Version); err != nil {
			t.Fatal(err)
		}
	}
	// seedCourse enrolls the learner and returns the course id and two lesson ids.
	seedCourse := func(owner, title, unitTitle string) (string, [2]string) {
		t.Helper()
		var quote, creation, course string
		if err := db.Pool.QueryRow(ctx, `INSERT INTO pricing_quotes(learner_id,owner_reference,pricing_version,snapshot,created_at,expires_at) VALUES($1::uuid,'seed-ref',$2,'{}',now(),now()+interval '10 minutes') RETURNING id::text`, owner, cat.Config().Version).Scan(&quote); err != nil {
			t.Fatal(err)
		}
		if err := db.Pool.QueryRow(ctx, `INSERT INTO course_creations(learner_id,draft_id,revision,quote_id,request,state) VALUES($1::uuid,gen_random_uuid(),1,$2::uuid,'{}','enrolled') RETURNING id::text`, owner, quote).Scan(&creation); err != nil {
			t.Fatal(err)
		}
		var lessons [2]string
		if err := db.Pool.QueryRow(ctx, `WITH c AS (INSERT INTO courses(learner_id,creation_id,title,description) VALUES($1::uuid,$2::uuid,$3,'Sky course') RETURNING id),
		 u AS (INSERT INTO course_units(learner_id,course_id,authored_id,position,title) SELECT $1::uuid,c.id,'u1',0,$4 FROM c RETURNING id),
		 e AS (INSERT INTO course_enrollments(learner_id,course_id) SELECT $1::uuid,c.id FROM c),
		 l1 AS (INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status) SELECT $1::uuid,u.id,'l1',0,'Moon phases','Observe the moon','ready_to_generate' FROM u RETURNING id),
		 l2 AS (INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status) SELECT $1::uuid,u.id,'l2',1,'Planets','Name the planets','locked' FROM u RETURNING id)
		 SELECT (SELECT id::text FROM c),(SELECT id::text FROM l1),(SELECT id::text FROM l2)`, owner, creation, title, unitTitle).Scan(&course, &lessons[0], &lessons[1]); err != nil {
			t.Fatal(err)
		}
		return course, lessons
	}
	summarize := func(owner, course, lesson, summary string, ago time.Duration) {
		t.Helper()
		if _, err := db.Pool.Exec(ctx, `INSERT INTO lesson_completion_summaries(learner_id,course_id,lesson_id,summary,completed_at) VALUES($1,$2,$3,$4,$5)`, owner, course, lesson, summary, time.Now().Add(-ago)); err != nil {
			t.Fatal(err)
		}
	}
	// turn quotes, accepts and sends one message, returning status, raw body and the SSE events.
	turn := func(token, course, class, message, key string) (int, string, []sseEvent, string) {
		t.Helper()
		code, raw := call("POST", "/v1/courses/"+course+"/tutor/quote", map[string]any{"class": class, "message": message}, token)
		if code != 200 {
			return code, raw, nil, ""
		}
		quote := asMap(raw)["id"].(string)
		if c, r := call("POST", "/v1/quotes/"+quote+"/accept", map[string]any{}, token); c != 200 {
			t.Fatal(c, r)
		}
		code, raw = call("POST", "/v1/courses/"+course+"/tutor/messages", map[string]any{"quote_id": quote, "idempotency_key": key, "class": class, "message": message}, token)
		return code, raw, parseSSE(raw), quote
	}
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := db.Pool.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	jobState := func(quote string) (string, int) {
		t.Helper()
		var state string
		var spent int
		if err := db.Pool.QueryRow(ctx, `SELECT state,COALESCE((result->>'content_spent')::int,0) FROM credit_jobs WHERE quote_id::text=$1`, quote).Scan(&state, &spent); err != nil {
			t.Fatal(err)
		}
		return state, spent
	}

	t.Run("quoted turn streams, persists and settles once with grounded context", func(t *testing.T) {
		token, owner := login("tutor-a@example.test")
		_, otherOwner := login("tutor-b@example.test")
		if _, err := db.Pool.Exec(ctx, `UPDATE learner_profiles SET goal='Read star charts',level='beginner',topic='Astronomy' WHERE learner_id=$1`, owner); err != nil {
			t.Fatal(err)
		}
		fund(owner, 100)
		course, lessons := seedCourse(owner, "Astronomy 101", "Sky basics")
		summarize(owner, course, lessons[1], "Second lesson recap", time.Hour)
		summarize(owner, course, lessons[0], "First lesson recap", 48*time.Hour)
		otherCourse, otherLessons := seedCourse(otherOwner, "Secret Course", "Other unit")
		summarize(otherOwner, otherCourse, otherLessons[0], "OTHER LEARNER SECRET", time.Hour)

		code, raw := call("POST", "/v1/courses/"+course+"/tutor/quote", map[string]any{"class": "short", "message": "What is a moon?"}, token)
		quote := asMap(raw)
		if code != 200 || quote["action"] != "tutor_short" || quote["pricing_version"] == "" || quote["content_ceiling"] == nil {
			t.Fatalf("versioned class quote: %d %s", code, raw)
		}
		code, raw, evs, quoteID := turn(token, course, "short", "What is a moon?", "key-1")
		if code != 200 {
			t.Fatal(code, raw)
		}
		var names, text []string
		for _, e := range evs {
			names = append(names, e.Name)
			if e.Name == "delta" {
				text = append(text, e.Data["text"].(string))
			}
		}
		if strings.Join(names, ",") != "thread,delta,delta,delta,done" || strings.Join(text, "") != "Planets orbit stars." {
			t.Fatalf("framing %v %q", names, text)
		}
		req := fixture.last()
		if req.CurrentLesson != nil || req.Outline.GetTitle() != "Astronomy 101" || len(req.Outline.Units) != 1 || req.Learner.GetGoals() != "Read star charts" || req.Learner.GetLearnerId() != owner {
			t.Fatalf("course-level context %+v", req)
		}
		if len(req.History) != 2 || !strings.Contains(req.History[0], "First lesson recap") || !strings.Contains(req.History[1], "Second lesson recap") {
			t.Fatalf("history must be oldest first: %v", req.History)
		}
		if state, spent := jobState(quoteID); state != "settled" || spent == 0 {
			t.Fatalf("settled once: %s %d", state, spent)
		}
		var users, assistants int
		_ = db.Pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE role='user'),count(*) FILTER (WHERE role='assistant' AND status='complete' AND content='Planets orbit stars.') FROM tutor_messages WHERE learner_id=$1`, owner).Scan(&users, &assistants)
		if users != 1 || assistants != 1 {
			t.Fatal("persisted messages", users, assistants)
		}
		// Replaying the same key neither calls the Engine nor charges again.
		before := fixture.calls.Load()
		code, raw = call("POST", "/v1/courses/"+course+"/tutor/messages", map[string]any{"quote_id": quoteID, "idempotency_key": "key-1", "class": "short", "message": "What is a moon?"}, token)
		replay := parseSSE(raw)
		if code != 200 || fixture.calls.Load() != before || replay[len(replay)-1].Data["replayed"] != true {
			t.Fatal("idempotent replay", code, raw)
		}
		if count(`SELECT count(*) FROM tutor_messages WHERE learner_id=$1`, owner) != 2 {
			t.Fatal("replay duplicated messages")
		}
		// Thread read.
		code, raw = call("GET", "/v1/courses/"+course+"/tutor/thread", nil, token)
		thread := asMap(raw)
		if code != 200 || len(thread["messages"].([]any)) != 2 {
			t.Fatal("thread", code, raw)
		}
		// Analytics: identifiers only, never text.
		if n := len(events.named("tutor_message_sent")); n != 1 {
			t.Fatal("tutor_message_sent", n)
		}
		done := events.named("tutor_reply_completed")
		if len(done) != 1 || done[0]["course_id"] != course {
			t.Fatalf("tutor_reply_completed %v", done)
		}
		for _, e := range events.events {
			if b, _ := json.Marshal(e); strings.Contains(string(b), "What is a moon") || strings.Contains(string(b), "Planets orbit") {
				t.Fatalf("private text in analytics %s", b)
			}
		}
		if err := financial.DispatchOutbox(ctx); err != nil {
			t.Fatal(err)
		}
		if len(events.named("$ai_generation")) == 0 {
			t.Fatal("missing $ai_generation")
		}
		// Per-thread deletion removes messages and excludes them from later context.
		threadID := thread["thread"].(map[string]any)["id"].(string)
		if c, _ := call("DELETE", "/v1/tutor/threads/"+threadID, nil, token); c != 204 && c != 200 {
			t.Fatal("delete", c)
		}
		if count(`SELECT count(*) FROM tutor_messages WHERE thread_id::text=$1`, threadID) != 0 {
			t.Fatal("messages survived delete")
		}
	})

	t.Run("another learner cannot read or delete a thread and context never leaks", func(t *testing.T) {
		tokenA, ownerA := login("iso-a@example.test")
		tokenB, ownerB := login("iso-b@example.test")
		fund(ownerA, 100)
		fund(ownerB, 100)
		courseA, lessonsA := seedCourse(ownerA, "A Course", "A unit")
		courseB, lessonsB := seedCourse(ownerB, "B Course", "B unit")
		summarize(ownerA, courseA, lessonsA[0], "ALPHA-SUMMARY", time.Hour)
		summarize(ownerB, courseB, lessonsB[0], "BRAVO-SUMMARY", time.Hour)
		if code, raw, _, _ := turn(tokenA, courseA, "short", "alpha question", "iso-a"); code != 200 {
			t.Fatal(code, raw)
		}
		if code, raw, _, _ := turn(tokenB, courseB, "short", "bravo question", "iso-b"); code != 200 {
			t.Fatal(code, raw)
		}
		blob, _ := json.Marshal(fixture.last())
		if strings.Contains(string(blob), "ALPHA") || strings.Contains(string(blob), "alpha question") || strings.Contains(string(blob), ownerA) {
			t.Fatalf("learner B's engine context contains learner A data: %s", blob)
		}
		var threadA string
		_ = db.Pool.QueryRow(ctx, `SELECT id::text FROM tutor_threads WHERE learner_id=$1`, ownerA).Scan(&threadA)
		for _, c := range []struct{ method, path string }{
			{"GET", "/v1/tutor/threads/" + threadA + "/messages"},
			{"DELETE", "/v1/tutor/threads/" + threadA},
			{"GET", "/v1/courses/" + courseA + "/tutor/thread"},
		} {
			if code, raw := call(c.method, c.path, nil, tokenB); code != 404 || strings.Contains(raw, "alpha question") {
				t.Fatalf("%s %s: %d %s", c.method, c.path, code, raw)
			}
		}
		calls := fixture.calls.Load()
		if code, _, _, _ := turn(tokenB, courseA, "short", "steal", "iso-steal"); code != 404 || fixture.calls.Load() != calls {
			t.Fatal("foreign course send must be not found without engine call", code)
		}
		if count(`SELECT count(*) FROM tutor_messages WHERE thread_id::text=$1`, threadA) != 2 {
			t.Fatal("owner's thread changed")
		}
		if code, _ := call("GET", "/v1/tutor/threads/"+threadA+"/messages", nil, ""); code != 401 {
			t.Fatal("unauthenticated read", code)
		}
	})

	t.Run("insufficient credits refuse before the engine and keep nothing", func(t *testing.T) {
		token, owner := login("poor@example.test")
		course, _ := seedCourse(owner, "Poor", "Unit")
		drain(owner)
		fund(owner, 4)
		calls := fixture.calls.Load()
		code, raw, _, quote := turn(token, course, "short", "need credits", "poor-1")
		out := asMap(raw)
		if code != 402 || out["error"] != "insufficient_credits" || out["draft_kept"] != true || fixture.calls.Load() != calls {
			t.Fatalf("refusal: %d %s calls=%d", code, raw, fixture.calls.Load()-calls)
		}
		if count(`SELECT count(*) FROM tutor_messages WHERE learner_id=$1`, owner)+count(`SELECT count(*) FROM tutor_turns WHERE learner_id=$1`, owner) != 0 {
			t.Fatal("refused turn persisted")
		}
		if count(`SELECT count(*) FROM credit_jobs WHERE quote_id::text=$1 AND state NOT IN ('released')`, quote) != 0 {
			t.Fatal("refused turn left a hold")
		}
		// After topping up, the same key and quote are accepted.
		fund(owner, 100)
		code, raw = call("POST", "/v1/courses/"+course+"/tutor/messages", map[string]any{"quote_id": quote, "idempotency_key": "poor-1", "class": "short", "message": "need credits"}, token)
		if code != 200 {
			t.Fatal("retry after top-up", code, raw)
		}
	})

	t.Run("concurrent sends cannot double charge or exceed the balance", func(t *testing.T) {
		token, owner := login("race@example.test")
		course, _ := seedCourse(owner, "Race", "Unit")
		drain(owner)
		fund(owner, 11) // two short turns at 5 credits, never three
		quoteFor := func(msg string) string {
			_, raw := call("POST", "/v1/courses/"+course+"/tutor/quote", map[string]any{"class": "short", "message": msg}, token)
			id := asMap(raw)["id"].(string)
			if c, r := call("POST", "/v1/quotes/"+id+"/accept", map[string]any{}, token); c != 200 {
				t.Fatal(c, r)
			}
			return id
		}
		calls := fixture.calls.Load()
		var wg sync.WaitGroup
		var ok, refused atomic.Int32
		for i := range 6 {
			id := quoteFor("slow race")
			wg.Add(1)
			go func() {
				defer wg.Done()
				code, _ := call("POST", "/v1/courses/"+course+"/tutor/messages", map[string]any{"quote_id": id, "idempotency_key": fmt.Sprint("race-", i), "class": "short", "message": "slow race"}, token)
				switch code {
				case 200:
					ok.Add(1)
				case 402, 429: // insufficient credits or the free concurrent-stream limit
					refused.Add(1)
				default:
					t.Errorf("unexpected status %d", code)
				}
			}()
		}
		wg.Wait()
		if ok.Load() < 1 || ok.Load() > 2 || ok.Load()+refused.Load() != 6 || int32(fixture.calls.Load()-calls) != ok.Load() {
			t.Fatalf("ok=%d refused=%d engine=%d", ok.Load(), refused.Load(), fixture.calls.Load()-calls)
		}
		// Sequential turns drain the wallet exactly: never a third paid turn from 11 credits.
		total := int(ok.Load())
		for i := range 4 {
			id := quoteFor("seq")
			code, _ := call("POST", "/v1/courses/"+course+"/tutor/messages", map[string]any{"quote_id": id, "idempotency_key": fmt.Sprint("seq-", i), "class": "short", "message": "seq"}, token)
			if code == 200 {
				total++
			} else if code != 402 {
				t.Fatalf("sequential status %d", code)
			}
		}
		var balance int
		_ = db.Pool.QueryRow(ctx, `SELECT COALESCE(sum(amount),0) FROM credit_entries WHERE learner_id=$1`, owner).Scan(&balance)
		if total != 2 || balance != 1 {
			t.Fatalf("exactly two charges of 5 from 11: turns=%d balance=%d", total, balance)
		}
		// Same idempotency key and quote in parallel: one engine call, one charge.
		_, raw := call("POST", "/v1/courses/"+course+"/tutor/quote", map[string]any{"class": "short", "message": "slow dup"}, token)
		id := asMap(raw)["id"].(string)
		fund(owner, 50)
		_, _ = call("POST", "/v1/quotes/"+id+"/accept", map[string]any{}, token)
		calls = fixture.calls.Load()
		var dup sync.WaitGroup
		for range 4 {
			dup.Add(1)
			go func() {
				defer dup.Done()
				_, _ = call("POST", "/v1/courses/"+course+"/tutor/messages", map[string]any{"quote_id": id, "idempotency_key": "dup", "class": "short", "message": "slow dup"}, token)
			}()
		}
		dup.Wait()
		if fixture.calls.Load()-calls != 1 || count(`SELECT count(*) FROM tutor_messages WHERE learner_id=$1 AND role='assistant' AND content='Planets orbit stars.'`, owner) != total+1 {
			t.Fatalf("duplicate key charged/answered more than once: engine=%d", fixture.calls.Load()-calls)
		}
	})

	t.Run("mid-stream failure persists one interrupted prefix and settles it", func(t *testing.T) {
		token, owner := login("interrupt@example.test")
		fund(owner, 100)
		course, _ := seedCourse(owner, "Interrupt", "Unit")
		code, raw, evs, quote := turn(token, course, "short", "interrupt please", "int-1")
		last := evs[len(evs)-1]
		if code != 200 || last.Name != "error" || last.Data["interrupted"] != true || last.Data["message_id"] == nil {
			t.Fatalf("interrupt framing: %d %s", code, raw)
		}
		var content, statusText string
		if err := db.Pool.QueryRow(ctx, `SELECT content,status FROM tutor_messages WHERE learner_id=$1 AND role='assistant'`, owner).Scan(&content, &statusText); err != nil || content != "Moons orbit " || statusText != "interrupted" {
			t.Fatalf("one interrupted prefix: %q %q %v", content, statusText, err)
		}
		if state, spent := jobState(quote); state != "settled" || spent == 0 {
			t.Fatalf("partial settlement: %s %d", state, spent)
		}
		calls := fixture.calls.Load()
		code, raw = call("POST", "/v1/courses/"+course+"/tutor/messages", map[string]any{"quote_id": quote, "idempotency_key": "int-1", "class": "short", "message": "interrupt please"}, token)
		if code != 200 || fixture.calls.Load() != calls || count(`SELECT count(*) FROM tutor_messages WHERE learner_id=$1 AND role='assistant'`, owner) != 1 {
			t.Fatal("retry after interrupt duplicated", code, raw)
		}
		if n := len(events.named("tutor_reply_failed")); n == 0 {
			t.Fatal("tutor_reply_failed not emitted")
		}
		// Zero delivered content: no message, hold released, credits intact.
		var before int
		_ = db.Pool.QueryRow(ctx, `SELECT COALESCE(sum(amount),0) FROM credit_entries WHERE learner_id=$1`, owner).Scan(&before)
		code, raw, _, quote2 := turn(token, course, "short", "fail-first please", "int-2")
		var after int
		_ = db.Pool.QueryRow(ctx, `SELECT COALESCE(sum(amount),0) FROM credit_entries WHERE learner_id=$1`, owner).Scan(&after)
		state, spent := jobState(quote2)
		if code != 502 || state != "released" || spent != 0 || after != before || count(`SELECT count(*) FROM tutor_messages WHERE learner_id=$1`, owner) != 2 {
			t.Fatalf("zero-delivery: %d %s state=%s spent=%d balance %d->%d", code, raw, state, spent, before, after)
		}
	})
}
