package e2e

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/chat"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
)

func TestChatEdgeCapabilityDefaultsOff(t *testing.T) {
	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{}, slog.Default()))
	t.Cleanup(server.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/v1/features", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("features status = %d", res.StatusCode)
	}
	var flags struct {
		ChatEdgeEnabled *bool `json:"chat_edge_enabled"`
	}
	if err := json.NewDecoder(res.Body).Decode(&flags); err != nil {
		t.Fatal(err)
	}
	if flags.ChatEdgeEnabled == nil || *flags.ChatEdgeEnabled {
		t.Fatalf("chat_edge_enabled must explicitly default to false, got %+v", flags)
	}
}

type edgeChatEngine struct{ calls atomic.Int32 }

func (e *edgeChatEngine) Chat(ctx context.Context, _ *enginev1.ChatRequest) (enginev1.EngineService_ChatClient, error) {
	e.calls.Add(1)
	return &edgeChatStream{ctx: ctx}, nil
}

type edgeChatStream struct {
	enginev1.EngineService_ChatClient
	ctx  context.Context
	sent bool
}

const edgeReply = "An echo reflects sound."

func (s *edgeChatStream) Recv() (*enginev1.ChatEvent, error) {
	if s.sent {
		<-s.ctx.Done()
		return nil, s.ctx.Err()
	}
	s.sent = true
	return &enginev1.ChatEvent{Event: &enginev1.ChatEvent_Delta{Delta: edgeReply}}, nil
}

func TestChatEdgeHTTPQuoteStopAndRecover(t *testing.T) {
	const origin = "https://client.example.test"
	const service = "/internal/edge/chat"
	const secret = "test-only-edge-signing-key-32-byte-minimum"
	t.Setenv("API_ALLOWED_ORIGINS", origin)
	auth, access, database := signedInDatabase(t)
	ctx := t.Context()
	account, err := auth.ParseAccess(access)
	if err != nil {
		t.Fatal(err)
	}
	catalogue, err := pricing.Load(ctx, database.Queries)
	if err != nil {
		t.Fatal(err)
	}
	version := catalogue.Config().Version
	if _, err := database.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version)
VALUES($1,100,'purchase','edge-chat',$2)`, account.Subject, version); err != nil {
		t.Fatal(err)
	}
	var course string
	err = database.Pool.QueryRow(ctx, `WITH q AS (
  INSERT INTO pricing_quotes(learner_id,owner_reference,pricing_version,snapshot,created_at,expires_at)
  VALUES($1::uuid,'edge-seed',$2,'{}',now(),now()+interval '10 minutes') RETURNING id
), n AS (
  INSERT INTO course_creations(learner_id,draft_id,revision,quote_id,request,state)
  SELECT $1::uuid,gen_random_uuid(),1,id,'{}','enrolled' FROM q RETURNING id
), c AS (
  INSERT INTO courses(learner_id,creation_id,title,description)
  SELECT $1::uuid,id,'Acoustics','How sound travels' FROM n RETURNING id
), u AS (
  INSERT INTO course_units(learner_id,course_id,authored_id,position,title)
  SELECT $1::uuid,id,'u1',0,'Echoes' FROM c
), e AS (
  INSERT INTO course_enrollments(learner_id,course_id) SELECT $1::uuid,id FROM c
) SELECT id::text FROM c`, account.Subject, version).Scan(&course)
	if err != nil {
		t.Fatal(err)
	}
	financial := credits.NewServiceWithCatalogue(database.Pool, nil, catalogue, nil)
	courseService := courses.New(courses.Options{Pool: database.Pool, Environment: "test", Financial: financial})
	engine := new(edgeChatEngine)
	tutor := chat.New(chat.Options{Pool: database.Pool, Engine: engine, Financial: financial, Profiles: courseService})
	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{
		Accounts: auth, Tutor: tutor, Quotes: pricing.NewQuotes(database.Pool),
		ChatEdge: chat.NewEdge(chat.EdgeOptions{Secret: secret, Tutor: tutor, SessionActive: auth.ActiveSession}),
	}, slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(server.Close)
	server.Client().Timeout = 20 * time.Second

	request := func(method, path, bearer, callerOrigin string, body []byte) *http.Request {
		t.Helper()
		r, err := http.NewRequestWithContext(ctx, method, server.URL+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+bearer)
		r.Header.Set("Content-Type", "application/json")
		if callerOrigin != "" {
			r.Header.Set("Origin", callerOrigin)
		}
		return r
	}
	read := func(r *http.Request, want int) []byte {
		t.Helper()
		res, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		raw, err := io.ReadAll(res.Body)
		if err != nil || res.StatusCode != want {
			t.Fatalf("API status = %d, want %d: %s (%v)", res.StatusCode, want, raw, err)
		}
		return raw
	}
	jsonBody := func(raw []byte) map[string]any {
		t.Helper()
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		return decoded
	}
	signed := func(method, path, transport string, body []byte, scope map[string]any) *http.Request {
		t.Helper()
		claims := map[string]any{
			"method": method, "path": path, "exp": time.Now().Add(30 * time.Second).Unix(),
			"body_sha256": fmt.Sprintf("%x", sha256.Sum256(body)), "transport": transport,
		}
		if scope != nil {
			claims["scope"] = scope
		}
		raw, err := json.Marshal(claims)
		if err != nil {
			t.Fatal(err)
		}
		payload := base64.RawURLEncoding.EncodeToString(raw)
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = io.WriteString(mac, "duolify-edge-chat-v1."+payload)
		return request(method, path, payload+"."+base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), "", body)
	}
	channel := "/v1/courses/" + course + "/tutor/edge-channel"
	grant := jsonBody(read(request("POST", channel, access, origin, []byte(`{"thread_id":""}`)), 200))
	grantBody, err := json.Marshal(map[string]any{"token": grant["token"], "origin": origin})
	if err != nil {
		t.Fatal(err)
	}
	read(request("POST", service+"/authorize", access, "", grantBody), 401)
	scope := jsonBody(read(signed("POST", service+"/authorize", "websocket", grantBody, nil), 200))
	if scope["learner_id"] != account.Subject || scope["session_id"] != account.ID || scope["course_id"] != course {
		t.Fatal("Go authorized unexpected scope")
	}
	quoteInput := []byte(`{"class":"short","message":"Why does sound echo?"}`)
	quote := jsonBody(read(signed("POST", service+"/quote", "websocket", quoteInput, scope), 200))["id"].(string)
	turn := "/turns/cf05-stable-turn"
	message := []byte(fmt.Sprintf(`{"quote_id":%q,"idempotency_key":"cf05-stable-turn","class":"short","message":"Why does sound echo?"}`, quote))
	read(signed("POST", service+"/messages", "websocket", message, scope), 409)
	read(request("POST", "/v1/quotes/"+quote+"/accept", access, "", []byte("{}")), 200)

	stream, err := server.Client().Do(signed("POST", service+"/messages", "websocket", message, scope))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.Body.Close() }()
	if stream.StatusCode != 200 || stream.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("Tutor stream status = %d", stream.StatusCode)
	}
	reader := bufio.NewReader(stream.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal("Tutor stream ended before a delta", err)
		}
		if strings.Contains(line, edgeReply) {
			break
		}
	}
	var reserved int
	if err := database.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_jobs WHERE quote_id=$1::uuid AND state='running'", quote).Scan(&reserved); err != nil || reserved != 1 {
		t.Fatalf("a single reservation must be running before stop: %d (%v)", reserved, err)
	}
	stop := jsonBody(read(signed("POST", service+turn+"/stop", "websocket", nil, scope), 200))
	if stop["state"] != "stopped" {
		t.Fatalf("stop returned %v", stop)
	}
	if tail, err := io.ReadAll(reader); err != nil || !bytes.Contains(tail, []byte("event: error")) {
		t.Fatalf("terminal event = %s (%v)", tail, err)
	}
	recovered := jsonBody(read(signed("GET", service+turn, "sse", nil, scope), 200))
	if msg, _ := recovered["message"].(map[string]any); recovered["state"] != "stopped" || msg["content"] != edgeReply {
		t.Fatalf("recover returned %v", recovered)
	}
	thread := recovered["thread_id"].(string)
	scope["thread_id"] = thread
	replay := read(signed("POST", service+"/messages", "sse", message, scope), 200)
	if !bytes.Contains(replay, []byte(edgeReply)) || engine.calls.Load() != 1 {
		t.Fatalf("replay restarted inference or dropped content: %s", replay)
	}
	var jobs, messages int
	if err := database.Pool.QueryRow(ctx, `SELECT
(SELECT count(*) FROM credit_jobs WHERE quote_id=$1::uuid AND state='settled'),
(SELECT count(*) FROM tutor_messages WHERE thread_id=$2::uuid)`, quote, thread).Scan(&jobs, &messages); err != nil || jobs != 1 || messages != 2 {
		t.Fatalf("canonical settlement/messages = %d/%d (%v)", jobs, messages, err)
	}
	scope["thread_id"] = "00000000-0000-4000-8000-000000000005"
	read(signed("GET", service+turn, "sse", nil, scope), 404)
	scope["thread_id"] = thread
	scope["learner_id"] = "00000000-0000-4000-8000-000000000005"
	read(signed("GET", service+turn, "sse", nil, scope), 401)
}
