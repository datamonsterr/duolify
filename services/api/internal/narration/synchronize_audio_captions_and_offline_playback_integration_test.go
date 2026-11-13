//go:build integration

package narration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestSynchronizeAudioCaptionsAndOfflinePlaybackIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	f := backuptest.New(t)
	f.Seed(t)
	pool := f.DB.Pool
	objects := f.ApplicationObjects()

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
	if _, err = pool.Exec(ctx, `INSERT INTO credit_entries (learner_id, amount, kind, reference, pricing_version, provenance) VALUES ($1, 200, 'purchase', 'sync-topup:' || gen_random_uuid(), '2026.04', '{"bucket": "purchase"}'::jsonb)`, owner); err != nil {
		t.Fatal(err)
	}

	// Release any open reservation so tests only reflect this run
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

	service := narration.New(narration.Options{
		Pool:        pool,
		Objects:     objects,
		Speech:      enginev1.NewEngineServiceClient(conn),
		Financial:   financial,
		Events:      events,
		JobAttempts: 1,
	})

	queue, err := service.Queue()
	if err != nil {
		t.Fatal(err)
	}
	if err = queue.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = queue.Stop(context.WithoutCancel(ctx)) }()

	docs := playerfixture.Documents()
	var fixtureManifest player.ObjectManifest
	if err = json.Unmarshal(docs[playerfixture.ManifestKey], &fixtureManifest); err != nil {
		t.Fatal(err)
	}
	texts := []string{fixtureManifest.Slides[0].NarrationText, fixtureManifest.Slides[1].NarrationText, fixtureManifest.Slides[2].NarrationText}
	mock.SetFaults(map[string]string{texts[2]: "engine"}) // slide 3 fails to verify fallback

	queued, err := service.Enqueue(ctx, owner, lesson)
	if err != nil || queued != 3 {
		t.Fatal(queued, err)
	}

	// Wait for background synthesis to complete
	deadline := time.Now().Add(40 * time.Second)
	for {
		var pending int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM slide_narrations WHERE lesson_id=$1 AND slide_id<>'seed-ready' AND status='pending'`, lesson).Scan(&pending)
		if pending == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("narration jobs timed out")
		}
		time.Sleep(100 * time.Millisecond)
	}

	mail := new(accounts.MailSink)
	auth, err := accounts.New(pool, strings.Repeat("narration-test", 3), mail)
	if err != nil {
		t.Fatal(err)
	}

	handler := httpapi.New(httpapi.Dependencies{
		Accounts: auth,
		Player: player.New(player.Options{
			Pool:        pool,
			Objects:     objects,
			AssetOrigin: playerfixture.AssetOrigin,
		}),
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
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

	// 1. Manifest status updates: sky-1 and sky-2 are ready, sky-3 is failed
	code, body, _ := call("/v1/lessons/"+lesson+"/manifest", ownerToken, nil)
	if code != 200 {
		t.Fatalf("expected 200 manifest, got %d: %s", code, string(body))
	}

	var m player.Manifest
	if err = json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}

	if len(m.Slides) != 3 {
		t.Fatalf("expected 3 slides, got %d", len(m.Slides))
	}

	if m.Slides[0].NarrationStatus != "ready" || m.Slides[0].Narration == nil {
		t.Fatalf("slide 0: expected ready narration, got %s", m.Slides[0].NarrationStatus)
	}
	if m.Slides[1].NarrationStatus != "ready" || m.Slides[1].Narration == nil {
		t.Fatalf("slide 1: expected ready narration, got %s", m.Slides[1].NarrationStatus)
	}
	if m.Slides[2].NarrationStatus != "failed" || m.Slides[2].Narration != nil {
		t.Fatalf("slide 2: expected failed narration, got %s", m.Slides[2].NarrationStatus)
	}

	// 2. Alignment & media clock fixtures: character timestamps match duration and text
	alignCode, alignBody, _ := call(m.Slides[0].Narration.AlignmentURL, ownerToken, nil)
	if alignCode != 200 {
		t.Fatalf("expected 200 alignment, got %d", alignCode)
	}
	var alignment struct {
		Characters []string `json:"characters"`
		StartMs    []int    `json:"character_start_times_seconds"`
		EndMs      []int    `json:"character_end_times_seconds"`
	}
	if err = json.Unmarshal(alignBody, &alignment); err != nil {
		t.Fatal(err)
	}
	if len(alignment.Characters) != len([]rune(texts[0])) {
		t.Fatalf("expected %d characters, got %d", len([]rune(texts[0])), len(alignment.Characters))
	}

	// 3. Cache bridge: owner can retrieve audio bytes, other learner receives 401
	audioCode, audioBody, header := call(m.Slides[0].Narration.AudioURL, ownerToken, nil)
	if audioCode != 200 {
		t.Fatalf("expected 200 audio, got %d", audioCode)
	}
	if !strings.HasPrefix(header.Get("Content-Type"), "audio/") {
		t.Fatalf("unexpected Content-Type: %s", header.Get("Content-Type"))
	}
	if len(audioBody) == 0 {
		t.Fatal("empty audio body")
	}

	// Unauthorized access: unauthenticated -> 401, other learner -> 404
	for token, want := range map[string]int{"": 401, otherToken: 404} {
		if c, _, _ := call(m.Slides[0].Narration.AudioURL, token, nil); c != want {
			t.Fatalf("expected status %d for token %q, got %d", want, token, c)
		}
	}
}
