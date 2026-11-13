//go:build integration

package narration_test

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
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/narration"
	"github.com/datamonsterr/duolify/services/api/internal/narration/narrationtest"
	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/datamonsterr/duolify/services/api/internal/player/playerfixture"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	owner  = "a1111111-1111-4111-8111-111111111111"
	lesson = "f3333333-3333-4333-8333-333333333333"
)

type recorder struct {
	mu     sync.Mutex
	events []map[string]any
}

func (r *recorder) Capture(_ string, event string, p map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	recorded := map[string]any{"event": event}
	for k, v := range p {
		recorded[k] = v
	}
	r.events = append(r.events, recorded)
	return nil
}
func (r *recorder) Close() error { return nil }
func (r *recorder) named(event string) []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []map[string]any
	for _, e := range r.events {
		if e["event"] == event {
			out = append(out, e)
		}
	}
	return out
}

func TestSynthesizeAndPlayOneNarratedSlideIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	f := backuptest.New(t)
	f.Seed(t)
	// The backup fixture stores narration=false; these tests exercise a Learner who has narration on.
	if _, err := f.DB.Pool.Exec(t.Context(), `UPDATE learner_profiles SET narration=true WHERE learner_id=$1`, "a1111111-1111-4111-8111-111111111111"); err != nil {
		t.Fatal(err)
	}
	pool := f.DB.Pool
	objects := f.ApplicationObjects()

	// Real gRPC between API and the engine twin: mock speech with alignment.
	mock := &narrationtest.Engine{}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	enginev1.RegisterEngineServiceServer(server, mock)
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	catalogue, err := pricing.Load(ctx, f.DB.Queries)
	if err != nil {
		t.Fatal(err)
	}
	events := &recorder{}
	financial := credits.NewServiceWithCatalogue(pool, nil, catalogue, events)
	if _, err = pool.Exec(ctx, `INSERT INTO credit_entries (learner_id, amount, kind, reference, pricing_version, provenance) VALUES ($1, 50, 'purchase', 'narration-topup:' || gen_random_uuid(), '2026.04', '{"bucket": "purchase"}'::jsonb)`, owner); err != nil {
		t.Fatal(err)
	}
	// The shared fixture leaves one open reservation; release it so the free-tier
	// concurrency bound only reflects this test's narration jobs.
	var open int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM credit_jobs WHERE learner_id=$1 AND state IN ('reserved','running')`, owner).Scan(&open)
	for open > 0 {
		var id string
		if err = pool.QueryRow(ctx, `SELECT id::text FROM credit_jobs WHERE learner_id=$1 AND state='reserved' LIMIT 1`, owner).Scan(&id); err != nil {
			break
		}
		if _, err = financial.Release(ctx, owner, id); err != nil {
			t.Fatal(err)
		}
		open--
	}
	service := narration.New(narration.Options{Pool: pool, Objects: objects, Speech: enginev1.NewEngineServiceClient(conn), Financial: financial, Events: events, JobAttempts: 1})
	queue, err := service.Queue()
	if err != nil {
		t.Fatal(err)
	}
	if err = queue.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = queue.Stop(context.WithoutCancel(ctx)) }()

	docs := playerfixture.Documents()
	var manifest player.ObjectManifest
	if err = json.Unmarshal(docs[playerfixture.ManifestKey], &manifest); err != nil {
		t.Fatal(err)
	}
	texts := []string{manifest.Slides[0].NarrationText, manifest.Slides[1].NarrationText, manifest.Slides[2].NarrationText}
	mock.SetFaults(map[string]string{texts[1]: "alignment", texts[2]: "engine"})

	// Text is stored before synthesis is queued; a missing text never reaches the provider.
	queued, err := service.Enqueue(ctx, owner, lesson)
	if err != nil || queued != 3 {
		t.Fatal(queued, err)
	}
	statuses := func() map[string]string {
		out := map[string]string{}
		rows, e := pool.Query(ctx, `SELECT slide_id,status FROM slide_narrations WHERE lesson_id=$1 AND slide_id<>'seed-ready'`, lesson)
		if e != nil {
			t.Fatal(e)
		}
		defer rows.Close()
		for rows.Next() {
			var id, st string
			if e = rows.Scan(&id, &st); e != nil {
				t.Fatal(e)
			}
			out[id] = st
		}
		return out
	}
	deadline := time.Now().Add(40 * time.Second)
	for {
		s := statuses()
		pending := 0
		for _, st := range s {
			if st == narration.StatusPending {
				pending++
			}
		}
		if len(s) == 3 && pending == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("narration jobs did not finish", s)
		}
		time.Sleep(100 * time.Millisecond)
	}
	got := statuses()
	if got["sky-1"] != "ready" || got["sky-2"] != "failed" || got["sky-3"] != "failed" {
		var codes []string
		cr, _ := pool.Query(ctx, `SELECT slide_id||'='||failure_code FROM slide_narrations WHERE lesson_id=$1 AND slide_id<>'seed-ready'`, lesson)
		for cr.Next() {
			var c string
			_ = cr.Scan(&c)
			codes = append(codes, c)
		}
		cr.Close()
		t.Fatal("unexpected statuses", got, codes)
	}
	var code2, code3 string
	_ = pool.QueryRow(ctx, `SELECT failure_code FROM slide_narrations WHERE lesson_id=$1 AND slide_id='sky-2'`, lesson).Scan(&code2)
	_ = pool.QueryRow(ctx, `SELECT failure_code FROM slide_narrations WHERE lesson_id=$1 AND slide_id='sky-3'`, lesson).Scan(&code3)
	if code2 != "invalid_alignment" || code3 != "engine_unavailable" {
		t.Fatal(code2, code3)
	}

	// Audio is reserved separately; failures release only their own audio component.
	rows, err := pool.Query(ctx, `SELECT j.state, COALESCE((j.result->>'audio_spent')::int,0), COALESCE((j.result->>'content_spent')::int,0)
		FROM credit_jobs j JOIN pricing_quotes q ON q.id=j.quote_id WHERE j.learner_id=$1 AND q.snapshot->>'action'='narration'`, owner)
	if err != nil {
		t.Fatal(err)
	}
	settled, released := 0, 0
	for rows.Next() {
		var state string
		var audio, content int
		if err = rows.Scan(&state, &audio, &content); err != nil {
			t.Fatal(err)
		}
		if content != 0 {
			t.Fatal("narration must never spend content credits", content)
		}
		switch state {
		case "settled":
			settled++
			if audio <= 0 {
				t.Fatal("successful synthesis must charge its audio component", audio)
			}
		case "released":
			released++
			if audio != 0 {
				t.Fatal("failed audio must not charge", audio)
			}
		default:
			t.Fatal("reservation left open", state)
		}
	}
	rows.Close()
	// E14-02: the lesson's slides share one audio reservation; failed slides add no charge.
	if settled != 1 || released != 0 {
		t.Fatal("reservation outcomes", settled, released)
	}

	// Stored versioned objects: WAV audio and alignment covering every character.
	var audioKey, alignmentKey string
	if err = pool.QueryRow(ctx, `SELECT audio_key,alignment_key FROM slide_narrations WHERE lesson_id=$1 AND slide_id='sky-1'`, lesson).Scan(&audioKey, &alignmentKey); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(audioKey, "lessons/narration/v1/") {
		t.Fatal(audioKey)
	}
	audio, err := objects.Get(ctx, audioKey)
	if err != nil || !bytes.HasPrefix(audio, []byte("RIFF")) {
		t.Fatal("audio object", err)
	}
	rawAlignment, err := objects.Get(ctx, alignmentKey)
	var alignment narration.Alignment
	if err != nil || json.Unmarshal(rawAlignment, &alignment) != nil || len(alignment.Characters) != len([]rune(texts[0])) {
		t.Fatal("alignment object", err)
	}

	// Manifest status, owner-only audio, and text stays usable beside failed audio.
	mail := new(accounts.MailSink)
	auth, err := accounts.New(pool, strings.Repeat("narration-test", 3), mail)
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.New(httpapi.Dependencies{Accounts: auth, Player: player.New(player.Options{Pool: pool, Objects: objects, AssetOrigin: playerfixture.AssetOrigin})}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	web := httptest.NewServer(handler)
	defer web.Close()
	call := func(path, token string, body any) (int, []byte, http.Header) {
		raw, _ := json.Marshal(body)
		method := http.MethodGet
		if body != nil {
			method = http.MethodPost
		}
		req, _ := http.NewRequestWithContext(ctx, method, web.URL+path, bytes.NewReader(raw))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, e := web.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer func() { _ = resp.Body.Close() }()
		out, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, out, resp.Header
	}
	login := func(email string) string {
		if c, b, _ := call("/v1/auth/code", "", map[string]any{"email": email, "age_confirmed": true}); c != 202 {
			t.Fatal(c, string(b))
		}
		c, b, _ := call("/v1/auth/verify", "", map[string]any{"email": email, "code": mail.Code(email), "age_confirmed": true})
		var token struct {
			Access string `json:"access_token"`
		}
		if c != 200 || json.Unmarshal(b, &token) != nil {
			t.Fatal(c, string(b))
		}
		return token.Access
	}
	ownerToken, otherToken := login("synthetic@example.test"), login("other-narration@example.test")
	code, body, _ := call("/v1/lessons/"+lesson+"/manifest", ownerToken, nil)
	var m player.Manifest
	if code != 200 || json.Unmarshal(body, &m) != nil {
		t.Fatal(code, string(body))
	}
	for i, want := range []string{"ready", "failed", "failed"} {
		s := m.Slides[i]
		if s.NarrationStatus != want || s.NarrationText != texts[i] {
			t.Fatal("manifest narration", i, s.NarrationStatus)
		}
		if (s.Narration != nil) != (want == "ready") {
			t.Fatal("only ready narration exposes audio", i)
		}
	}
	if strings.Contains(string(body), "lessons/narration") {
		t.Fatal("object keys leaked")
	}
	audioURL := m.Slides[0].Narration.AudioURL
	if c, b, h := call(audioURL, ownerToken, nil); c != 200 || !bytes.HasPrefix(b, []byte("RIFF")) || h.Get("Content-Type") != "audio/wav" {
		t.Fatal("owner audio", c)
	}
	if c, _, _ := call(m.Slides[0].Narration.AlignmentURL, ownerToken, nil); c != 200 {
		t.Fatal("owner alignment", c)
	}
	for token, want := range map[string]int{"": 401, otherToken: 404} {
		if c, _, _ := call(audioURL, token, nil); c != want {
			t.Fatal("audio authorization", token == "", c)
		}
	}
	if c, _, _ := call("/v1/lessons/"+lesson+"/slides/sky-2/narration/audio", ownerToken, nil); c != 404 {
		t.Fatal("failed audio must not be served", c)
	}

	// Identical text, voice, model and settings: zero charge, no provider call.
	before := mock.Calls()
	if _, err = pool.Exec(ctx, `INSERT INTO slide_narrations(lesson_id,learner_id,slide_id,status,narration_text) VALUES($1,$2,'dup','pending',$3)`, lesson, owner, texts[0]); err != nil {
		t.Fatal(err)
	}
	if err = service.Synthesize(ctx, owner, lesson, "dup", true); err != nil {
		t.Fatal(err)
	}
	if mock.Calls() != before || statuses()["dup"] != "ready" || len(events.named("narration_cache_hit")) != 1 {
		t.Fatal("cache hit must reuse the object without a provider call")
	}
	var jobs int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM credit_jobs j JOIN pricing_quotes q ON q.id=j.quote_id WHERE j.learner_id=$1 AND q.snapshot->>'action'='narration'`, owner).Scan(&jobs)
	if jobs != 1 {
		t.Fatal("cache hit reserved credits", jobs)
	}

	// Concurrent identical requests produce one synthesis call.
	const shared = "Both slides say exactly the same thing."
	for _, id := range []string{"same-a", "same-b"} {
		if _, err = pool.Exec(ctx, `INSERT INTO slide_narrations(lesson_id,learner_id,slide_id,status,narration_text) VALUES($1,$2,$3,'pending',$4)`, lesson, owner, id, shared); err != nil {
			t.Fatal(err)
		}
	}
	before = mock.Calls()
	var wg sync.WaitGroup
	for _, id := range []string{"same-a", "same-b"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := service.Synthesize(ctx, owner, lesson, id, true); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if mock.Calls() != before+1 {
		t.Fatal("concurrent requests must share one synthesis", mock.Calls()-before)
	}

	// Missing text is rejected before any provider or reservation.
	before = mock.Calls()
	if _, err = pool.Exec(ctx, `INSERT INTO slide_narrations(lesson_id,learner_id,slide_id,status,narration_text) VALUES($1,$2,'blank','pending','   ')`, lesson, owner); err != nil {
		t.Fatal(err)
	}
	if err = service.Synthesize(ctx, owner, lesson, "blank", true); err != nil || statuses()["blank"] != "failed" || mock.Calls() != before {
		t.Fatal("missing text must fail closed", err)
	}

	// Analytics: closed properties, no learner text.
	for _, name := range []string{"narration_generated", "narration_failed", "$ai_generation"} {
		if len(events.named(name)) == 0 {
			t.Fatal("missing event", name)
		}
	}
	for _, e := range events.named("$ai_generation") {
		if e["feature"] != "narration" {
			t.Fatal("ai generation feature", e)
		}
	}
	for _, text := range append(texts, shared) {
		for _, e := range events.events {
			if strings.Contains(fmt.Sprint(e), text) {
				t.Fatal("learner text reached analytics", e["event"])
			}
		}
	}

	// Data: export lists the table; unshared objects are identified; deletion cascades.
	if c, b, _ := call("/v1/me/data-export", ownerToken, nil); c != 200 || !bytes.Contains(b, []byte("slide_narrations")) {
		t.Fatal("export must include slide_narrations", c)
	}
	keys, err := narration.UnsharedObjectKeys(ctx, pool, []string{lesson})
	if err != nil || len(keys) < 2 {
		t.Fatal(keys, err)
	}
	if keys, err = narration.UnsharedObjectKeys(ctx, pool, []string{"00000000-0000-4000-8000-000000000000"}); err != nil || len(keys) != 0 {
		t.Fatal("objects of other lessons must stay", keys, err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM lesson_manifests WHERE lesson_id=$1`, lesson); err != nil {
		t.Fatal(err)
	}
	var remaining int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM slide_narrations WHERE lesson_id=$1`, lesson).Scan(&remaining)
	if remaining != 0 {
		t.Fatal("lesson deletion must remove narration rows", remaining)
	}
}
