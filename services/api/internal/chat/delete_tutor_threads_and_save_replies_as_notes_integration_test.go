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
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/chat"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	backend "github.com/datamonsterr/duolify/services/api/internal/engine"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/notes"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"google.golang.org/grpc"
)

type deleteEngine struct {
	enginev1.UnimplementedEngineServiceServer
	mu       sync.Mutex
	requests []*enginev1.ChatRequest
}

func (e *deleteEngine) Chat(r *enginev1.ChatRequest, stream enginev1.EngineService_ChatServer) error {
	e.mu.Lock()
	e.requests = append(e.requests, r)
	e.mu.Unlock()
	return stream.Send(&enginev1.ChatEvent{Event: &enginev1.ChatEvent_Delta{Delta: "A fresh answer."}})
}
func (e *deleteEngine) last() *enginev1.ChatRequest {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.requests[len(e.requests)-1]
}

func TestDeleteTutorThreadsAndSaveRepliesAsNotesIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
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
	fixture := new(deleteEngine)
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
	noteService := notes.NewService(db.Pool, events)
	tutor := chat.New(chat.Options{Pool: db.Pool, Engine: engine, Financial: financial, Profiles: courseService, Events: events, Notes: noteService})
	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{Accounts: auth, Quotes: pricing.NewQuotes(db.Pool), Credits: financial, Tutor: tutor, Notes: noteService}, slog.New(slog.NewTextHandler(io.Discard, nil))))
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
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := db.Pool.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	seed := func(owner, prefix string) (string, string) {
		t.Helper()
		var quote, creation, course, unit string
		if err := db.Pool.QueryRow(ctx, `INSERT INTO pricing_quotes(learner_id,owner_reference,pricing_version,snapshot,created_at,expires_at) VALUES($1::uuid,'seed-ref',$2,'{}',now(),now()+interval '10 minutes') RETURNING id::text`, owner, cat.Config().Version).Scan(&quote); err != nil {
			t.Fatal(err)
		}
		if err := db.Pool.QueryRow(ctx, `INSERT INTO course_creations(learner_id,draft_id,revision,quote_id,request,state) VALUES($1::uuid,gen_random_uuid(),1,$2::uuid,'{}','enrolled') RETURNING id::text`, owner, quote).Scan(&creation); err != nil {
			t.Fatal(err)
		}
		if err := db.Pool.QueryRow(ctx, `WITH c AS (INSERT INTO courses(learner_id,creation_id,title,description) VALUES($1::uuid,$2::uuid,$3,'Course') RETURNING id),
		 u AS (INSERT INTO course_units(learner_id,course_id,authored_id,position,title) SELECT $1::uuid,c.id,'u1',0,'Unit' FROM c RETURNING id),
		 e AS (INSERT INTO course_enrollments(learner_id,course_id) SELECT $1::uuid,c.id FROM c)
		 SELECT (SELECT id::text FROM c),(SELECT id::text FROM u)`, owner, creation, prefix+" course").Scan(&course, &unit); err != nil {
			t.Fatal(err)
		}
		var lesson string
		if err := db.Pool.QueryRow(ctx, `INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status) VALUES($1::uuid,$2::uuid,'l0',0,$3,$4,'current') RETURNING id::text`, owner, unit, prefix+" lesson", prefix+" goal").Scan(&lesson); err != nil {
			t.Fatal(err)
		}
		return course, lesson
	}
	// thread seeds a thread with a question, a complete reply, an interrupted reply and a text-free turn row.
	thread := func(owner, course, question, reply string) (thread, user, assistant, cut string) {
		t.Helper()
		if err := db.Pool.QueryRow(ctx, `INSERT INTO tutor_threads(learner_id,course_id) VALUES($1::uuid,$2::uuid) RETURNING id::text`, owner, course).Scan(&thread); err != nil {
			t.Fatal(err)
		}
		ins := func(role, content, status string) string {
			var id string
			if err := db.Pool.QueryRow(ctx, `INSERT INTO tutor_messages(thread_id,learner_id,role,content,status) VALUES($1::uuid,$2::uuid,$3,$4,$5) RETURNING id::text`, thread, owner, role, content, status).Scan(&id); err != nil {
				t.Fatal(err)
			}
			return id
		}
		user, assistant, cut = ins("user", question, "complete"), ins("assistant", reply, "complete"), ins("assistant", "partial prefix", "interrupted")
		return
	}
	fund := func(owner string) {
		exec(`INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version) VALUES($1,500,'purchase',$2,$3)`, owner, "fund-"+owner+fmt.Sprint(time.Now().UnixNano()), cat.Config().Version)
	}

	tokenA, owner := login("e805-a@example.test")
	tokenB, other := login("e805-b@example.test")
	courseA, lessonA := seed(owner, "Alpha")
	courseB, lessonB := seed(other, "Bravo")
	const secretReply = "ZEBRA-CONSTELLATION is where Polaris hides"
	threadA, userMsg, replyMsg, cutMsg := thread(owner, courseA, "Where is ZEBRA-QUESTION?", secretReply)
	saveURL := func(id string) string { return "/v1/tutor/messages/" + id + "/note" }

	var noteID string
	t.Run("save is idempotent, raw, free and carries provenance", func(t *testing.T) {
		// The Learner has zero credits: the raw note must still save.
		code, raw := call("POST", saveURL(replyMsg), map[string]any{"lesson_id": lessonA}, tokenA)
		if code != 200 {
			t.Fatal(code, raw)
		}
		first := asMap(raw)
		noteID, _ = first["id"].(string)
		if noteID == "" || first["status"] != "enrichment_pending" || first["truncated"] != false {
			t.Fatalf("save response %v", first)
		}
		code, raw = call("POST", saveURL(replyMsg), map[string]any{"lesson_id": lessonA}, tokenA)
		if code != 200 || asMap(raw)["id"] != noteID {
			t.Fatalf("second save must return the same note: %d %s", code, raw)
		}
		code, raw = call("POST", saveURL(replyMsg), nil, tokenA)
		if code != 200 || asMap(raw)["id"] != noteID {
			t.Fatalf("a retry without a body must return the same note: %d %s", code, raw)
		}
		if n := count(`SELECT count(*) FROM quick_notes WHERE learner_id=$1::uuid`, owner); n != 1 {
			t.Fatalf("notes = %d", n)
		}
		if n := count(`SELECT count(*) FROM quick_note_chat_sources WHERE note_id=$1::uuid AND thread_id=$2::uuid AND message_id=$3::uuid AND lesson_id=$4::uuid AND NOT unavailable`, noteID, threadA, replyMsg, lessonA); n != 1 {
			t.Fatal("provenance missing")
		}
		var source, text string
		if err := db.Pool.QueryRow(ctx, `SELECT source,raw_text FROM quick_notes WHERE id=$1::uuid`, noteID).Scan(&source, &text); err != nil || source != "chat" || text != secretReply {
			t.Fatalf("note %q %q %v", source, text, err)
		}
		// Exactly one creation event, ids and counts only.
		created := events.named("quick_note_created")
		if len(created) != 1 || created[0]["source"] != "chat" || fmt.Sprint(created[0]["length"]) != fmt.Sprint(len(secretReply)) {
			t.Fatalf("events %v", created)
		}
		for _, e := range events.events {
			if strings.Contains(fmt.Sprint(e), "ZEBRA") {
				t.Fatal("private text reached analytics")
			}
		}
		code, raw = call("GET", "/v1/notes/"+noteID, nil, tokenA)
		src, _ := asMap(raw)["chat_source"].(map[string]any)
		if code != 200 || src["state"] != "available" || src["thread_id"] != threadA || src["message_id"] != replyMsg {
			t.Fatalf("note %d %s", code, raw)
		}
	})
	t.Run("only the learner's own complete assistant replies can be saved", func(t *testing.T) {
		_, _, fReply, _ := thread(other, courseB, "q", "foreign reply")
		for name, c := range map[string]struct {
			token, id string
			want      int
			body      any
		}{
			"foreign message":    {tokenA, fReply, 404, nil},
			"user message":       {tokenA, userMsg, 404, nil},
			"interrupted prefix": {tokenA, cutMsg, 409, nil},
			"unknown":            {tokenA, "00000000-0000-4000-8000-000000000001", 404, nil},
			"malformed":          {tokenA, "nope", 404, nil},
			"foreign lesson":     {tokenA, replyMsg, 404, map[string]any{"lesson_id": lessonB}},
			"unauthenticated":    {"", replyMsg, 401, nil},
		} {
			if code, raw := call("POST", saveURL(c.id), c.body, c.token); code != c.want {
				t.Fatalf("%s: %d %s", name, code, raw)
			}
		}
		if n := count(`SELECT count(*) FROM quick_notes WHERE learner_id IN ($1::uuid,$2::uuid)`, owner, other); n != 1 {
			t.Fatalf("a refused save created a note: %d", n)
		}
	})
	t.Run("a long reply is truncated at the note bound and says so", func(t *testing.T) {
		_, _, long, _ := thread(owner, courseA, "q", strings.Repeat("é", 2500))
		code, raw := call("POST", saveURL(long), nil, tokenA)
		out := asMap(raw)
		if code != 200 || out["truncated"] != true {
			t.Fatal(code, raw)
		}
		if n := count(`SELECT octet_length(raw_text) FROM quick_notes WHERE id=$1::uuid`, out["id"]); n != 2000 {
			t.Fatalf("stored length %d", n)
		}
	})
	t.Run("opt-out suppresses the creation event", func(t *testing.T) {
		exec(`INSERT INTO learner_profiles(learner_id,analytics_opt_out) VALUES($1::uuid,true) ON CONFLICT (learner_id) DO UPDATE SET analytics_opt_out=true`, other)
		_, _, reply, _ := thread(other, courseB, "q", "bravo reply")
		before := len(events.named("quick_note_created"))
		if code, raw := call("POST", saveURL(reply), nil, tokenB); code != 200 {
			t.Fatal(code, raw)
		}
		if len(events.named("quick_note_created")) != before {
			t.Fatal("event delivered despite opt-out")
		}
	})
	t.Run("delete is owner-only and removes messages and references", func(t *testing.T) {
		fund(owner)
		exec(`INSERT INTO tutor_turns(learner_id,course_id,idempotency_key,quote_id,state,thread_id,user_message_id,assistant_message_id)
		 SELECT $1::uuid,$2::uuid,'del-key',(SELECT id FROM pricing_quotes WHERE learner_id=$1::uuid LIMIT 1),'completed',$3::uuid,$4::uuid,$5::uuid`, owner, courseA, threadA, userMsg, replyMsg)
		if code, _ := call("DELETE", "/v1/tutor/threads/"+threadA, nil, tokenB); code != 404 {
			t.Fatalf("foreign delete = %d", code)
		}
		if n := count(`SELECT count(*) FROM tutor_messages WHERE thread_id=$1::uuid`, threadA); n != 3 {
			t.Fatal("foreign delete changed messages")
		}
		if code, raw := call("DELETE", "/v1/tutor/threads/"+threadA, nil, tokenA); code != 204 {
			t.Fatal(code, raw)
		}
		if n := count(`SELECT count(*) FROM tutor_messages WHERE thread_id=$1::uuid`, threadA); n != 0 {
			t.Fatal("messages remain")
		}
		if n := count(`SELECT count(*) FROM tutor_turns WHERE idempotency_key='del-key' AND user_message_id IS NULL AND assistant_message_id IS NULL AND thread_id IS NULL`); n != 1 {
			t.Fatal("context references remain on the turn row")
		}
		if n := count(`SELECT count(*) FROM tutor_thread_deletion_tombstones WHERE thread_id=$1::uuid`, threadA); n != 1 {
			t.Fatal("no tombstone")
		}
		if code, _ := call("DELETE", "/v1/tutor/threads/"+threadA, nil, tokenA); code != 404 {
			t.Fatalf("second delete = %d", code)
		}
		if code, _ := call("POST", saveURL(replyMsg), nil, tokenA); code != 404 {
			t.Fatal("saving a deleted reply must be not found")
		}
	})
	t.Run("notes saved from a deleted thread stay readable with source unavailable", func(t *testing.T) {
		code, raw := call("GET", "/v1/notes/"+noteID, nil, tokenA)
		out := asMap(raw)
		src, _ := out["chat_source"].(map[string]any)
		if code != 200 || out["raw_text"] != secretReply || src["state"] != "unavailable" || src["thread_id"] != nil || src["message_id"] != nil {
			t.Fatalf("note %d %s", code, raw)
		}
		code, raw = call("GET", "/v1/notes", nil, tokenA)
		if code != 200 || !strings.Contains(raw, `"unavailable"`) {
			t.Fatalf("list %d %s", code, raw)
		}
	})
	t.Run("later tutor turns never include the deleted history", func(t *testing.T) {
		// A new thread is started by the next turn; the old text must not reach the Engine.
		_, raw := call("POST", "/v1/courses/"+courseA+"/tutor/quote", map[string]any{"class": "short", "message": "Anything new?"}, tokenA)
		q := asMap(raw)
		quote, _ := q["id"].(string)
		if quote == "" {
			t.Fatal(raw)
		}
		if c, r := call("POST", "/v1/quotes/"+quote+"/accept", map[string]any{}, tokenA); c != 200 {
			t.Fatal(c, r)
		}
		code, raw := call("POST", "/v1/courses/"+courseA+"/tutor/messages", map[string]any{"quote_id": quote, "idempotency_key": "after-delete", "class": "short", "message": "Anything new?"}, tokenA)
		if code != 200 || !strings.Contains(raw, "A fresh answer.") {
			t.Fatal(code, raw)
		}
		sent := fixture.last().String()
		for _, gone := range []string{"ZEBRA-CONSTELLATION", "ZEBRA-QUESTION", "partial prefix"} {
			if strings.Contains(sent, gone) {
				t.Fatalf("deleted history %q reached the Engine request", gone)
			}
		}
	})
	t.Run("a restored backup re-applies the thread tombstone", func(t *testing.T) {
		exec(`INSERT INTO tutor_threads(id,learner_id,course_id) VALUES($1::uuid,$2::uuid,$3::uuid)`, threadA, owner, courseA)
		exec(`INSERT INTO tutor_messages(id,thread_id,learner_id,role,content) VALUES($1::uuid,$2::uuid,$3::uuid,'assistant','resurrected text')`, replyMsg, threadA, owner)
		exec(`UPDATE quick_note_chat_sources SET unavailable=false,thread_id=$2::uuid,message_id=$3::uuid WHERE note_id=$1::uuid`, noteID, threadA, replyMsg)
		if err := accounts.ApplyDeletionTombstones(ctx, db.Pool); err != nil {
			t.Fatal(err)
		}
		if n := count(`SELECT count(*) FROM tutor_threads WHERE id=$1::uuid`, threadA) + count(`SELECT count(*) FROM tutor_messages WHERE id=$1::uuid`, replyMsg); n != 0 {
			t.Fatal("deleted thread survived restore")
		}
		if n := count(`SELECT count(*) FROM quick_note_chat_sources WHERE note_id=$1::uuid AND unavailable AND thread_id IS NULL AND message_id IS NULL`, noteID); n != 1 {
			t.Fatal("provenance not re-marked unavailable")
		}
	})
	t.Run("save racing delete never leaves provenance pointing at nothing", func(t *testing.T) {
		for i := range 8 {
			th, _, reply, _ := thread(owner, courseA, "q", fmt.Sprintf("race reply %d", i))
			var wg sync.WaitGroup
			var saveCode int
			wg.Add(2)
			go func() { defer wg.Done(); saveCode, _ = call("POST", saveURL(reply), nil, tokenA) }()
			go func() { defer wg.Done(); call("DELETE", "/v1/tutor/threads/"+th, nil, tokenA) }()
			wg.Wait()
			if n := count(`SELECT count(*) FROM quick_note_chat_sources WHERE NOT unavailable AND thread_id=$1::uuid`, th); n != 0 {
				t.Fatalf("iteration %d: live provenance for a deleted thread (save=%d)", i, saveCode)
			}
			if saveCode == 200 {
				if n := count(`SELECT count(*) FROM quick_notes WHERE learner_id=$1::uuid AND action_key=$2`, owner, "tutor-message:"+reply); n != 1 {
					t.Fatalf("iteration %d: save succeeded without a note", i)
				}
			}
		}
	})
}
