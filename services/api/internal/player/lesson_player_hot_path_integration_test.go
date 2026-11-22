//go:build integration

package player_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/datamonsterr/duolify/services/api/internal/player/playerfixture"
)

// countingObjects counts every object read the Player makes.
type countingObjects struct {
	inner player.Objects
	mu    sync.Mutex
	gets  int
}

func (c *countingObjects) Get(ctx context.Context, key string) ([]byte, error) {
	c.mu.Lock()
	c.gets++
	c.mu.Unlock()
	return c.inner.Get(ctx, key)
}

func (c *countingObjects) take() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := c.gets
	c.gets = 0
	return n
}

// TestLessonPlayerHotPathObjectReadsIntegration pins the object reads per request on
// the lesson player hot path (optimize-lesson-player-hot-path): position saves and
// warm manifest reads never touch object storage, a cold manifest read is one GET,
// authorization still runs per learner on every request, tampering is still caught
// and malformed ids are client errors.
func TestLessonPlayerHotPathObjectReadsIntegration(t *testing.T) {
	f := backuptest.New(t)
	f.Seed(t)
	mail := new(accounts.MailSink)
	auth, err := accounts.New(f.DB.Pool, strings.Repeat("hot-path-test", 4), mail)
	if err != nil {
		t.Fatal(err)
	}
	store := f.ApplicationObjects()
	newServer := func() (*httptest.Server, *countingObjects) {
		c := &countingObjects{inner: store}
		service := player.New(player.Options{Pool: f.DB.Pool, Objects: c, AssetOrigin: playerfixture.AssetOrigin})
		return httptest.NewServer(httpapi.New(httpapi.Dependencies{Accounts: auth, Player: service}, slog.New(slog.NewTextHandler(io.Discard, nil)))), c
	}
	server, counter := newServer()
	defer server.Close()
	call := func(srv *httptest.Server, method, path, token string, body any) (int, []byte) {
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequestWithContext(t.Context(), method, srv.URL+path, bytes.NewReader(raw))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, e := srv.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer func() { _ = resp.Body.Close() }()
		out, e := io.ReadAll(resp.Body)
		if e != nil {
			t.Fatal(e)
		}
		return resp.StatusCode, out
	}
	login := func(email string) string {
		if c, b := call(server, "POST", "/v1/auth/code", "", map[string]any{"email": email, "age_confirmed": true}); c != 202 {
			t.Fatal(c, string(b))
		}
		c, b := call(server, "POST", "/v1/auth/verify", "", map[string]any{"email": email, "code": mail.Code(email), "age_confirmed": true})
		if c != 200 {
			t.Fatal(c, string(b))
		}
		var token struct {
			Access string `json:"access_token"`
		}
		if err := json.Unmarshal(b, &token); err != nil {
			t.Fatal(err)
		}
		return token.Access
	}
	owner := login("synthetic@example.test")
	other := login("other-player@example.test")
	const lesson = "f3333333-3333-4333-8333-333333333333"
	manifestPath := "/v1/lessons/" + lesson + "/manifest"
	position := map[string]any{"version": 1, "slide_id": "sky-1", "time_ms": 1200}
	reads := func(label string) int {
		n := counter.take()
		t.Logf("object GETs %-34s %d", label, n)
		return n
	}

	// Cold manifest read: one GET, because the manifest already stores every digest.
	counter.take()
	c, body := call(server, "GET", manifestPath, owner, nil)
	if c != 200 {
		t.Fatal(c, string(body))
	}
	var m player.Manifest
	if err := json.Unmarshal(body, &m); err != nil || len(m.Slides) != 3 {
		t.Fatal(err, string(body))
	}
	for _, s := range m.Slides {
		if len(s.HTMLSHA256) != 64 {
			t.Fatal("manifest lost the slide digest", s)
		}
	}
	if n := reads("manifest cold"); n > 1 {
		t.Fatalf("cold manifest read made %d object reads, want at most 1", n)
	}

	// Warm manifest read, position save and sync: no object reads at all.
	if c, again := call(server, "GET", manifestPath, owner, nil); c != 200 || !bytes.Equal(body, again) {
		t.Fatal("warm manifest differs", c)
	}
	if n := reads("manifest warm"); n != 0 {
		t.Fatalf("warm manifest read made %d object reads", n)
	}
	if c, b := call(server, "PUT", "/v1/lessons/"+lesson+"/position", owner, position); c != 200 {
		t.Fatal(c, string(b))
	}
	if c, b := call(server, "GET", "/v1/lessons/"+lesson+"/position", owner, nil); c != 200 || !bytes.Contains(b, []byte("sky-1")) {
		t.Fatal(c, string(b))
	}
	syncBody := map[string]any{"events": []map[string]any{{"action_key": "hot-path-1", "version": 1, "slide_id": "sky-2", "time_ms": 100, "occurred_at": "2026-11-20T10:00:00Z"}}}
	if c, b := call(server, "POST", "/v1/lessons/"+lesson+"/sync", owner, syncBody); c != 200 {
		t.Fatal(c, string(b))
	}
	if n := reads("position PUT+GET and sync (warm)"); n != 0 {
		t.Fatalf("position/sync made %d object reads, want 0", n)
	}

	// A slide read fetches only that slide's HTML.
	if c, b := call(server, "GET", m.Slides[0].HTMLURL, owner, nil); c != 200 || !bytes.Contains(b, []byte("html")) {
		t.Fatal(c, string(b))
	}
	if n := reads("slide read (warm manifest)"); n != 1 {
		t.Fatalf("slide read made %d object reads, want 1", n)
	}

	// Another learner never reads or writes through the warm cache.
	for _, probe := range []struct{ method, path string }{
		{"GET", manifestPath},
		{"GET", m.Slides[0].HTMLURL},
		{"GET", "/v1/lessons/" + lesson + "/position"},
		{"PUT", "/v1/lessons/" + lesson + "/position"},
		{"POST", "/v1/lessons/" + lesson + "/sync"},
		{"GET", "/v1/lessons/" + lesson + "/slides/sky-1/narration/audio"},
	} {
		if c, b := call(server, probe.method, probe.path, other, position); c != 404 {
			t.Fatalf("learner B reached learner A's lesson: %s %s -> %d %s", probe.method, probe.path, c, b)
		}
		if c, _ := call(server, probe.method, probe.path, "", position); c != 401 {
			t.Fatalf("anonymous %s %s -> %d", probe.method, probe.path, c)
		}
	}
	counter.take()

	// Malformed ids are client errors, never a Postgres 22P02 turned into a 5xx.
	for _, path := range []string{
		"/v1/lessons/not-a-uuid/manifest",
		"/v1/lessons/not-a-uuid/position",
		"/v1/lessons/not-a-uuid/slides/sky-1",
		"/v1/lessons/not-a-uuid/slides/sky-1/narration/audio",
		"/v1/lessons/" + lesson + "x/manifest",
		"/v1/courses/not-a-uuid/download",
	} {
		if c, b := call(server, "GET", path, owner, nil); c != 400 && c != 404 {
			t.Fatalf("malformed id %s -> %d %s", path, c, b)
		}
	}
	if c, _ := call(server, "POST", "/v1/lessons/not-a-uuid/sync", owner, syncBody); c != 400 && c != 404 {
		t.Fatal("malformed sync id", c)
	}
	if c, _ := call(server, "GET", "/v1/lessons/"+strings.ToUpper(lesson)+"/manifest", owner, nil); c != 200 {
		t.Fatal("upper-case uuid should still resolve", c)
	}
	counter.take()

	// Integrity is unchanged where the specs require it. A slide whose bytes differ
	// from the digest stored in the manifest is refused even though it is valid HTML.
	htmlKey := "lessons/fixture-v1/one.html"
	original, err := store.Get(t.Context(), htmlKey)
	if err != nil {
		t.Fatal(err)
	}
	put := func(key string, raw []byte) {
		if _, e := store.Client.PutObject(t.Context(), &s3.PutObjectInput{Bucket: aws.String(store.Bucket), Key: aws.String(key), Body: bytes.NewReader(raw)}); e != nil {
			t.Fatal(e)
		}
	}
	put(htmlKey, []byte("<html><body>tampered but harmless</body></html>"))
	if c, _ := call(server, "GET", m.Slides[0].HTMLURL, owner, nil); c != 503 {
		t.Fatal("slide with a digest mismatch was served", c)
	}
	put(htmlKey, original)
	if c, _ := call(server, "GET", m.Slides[0].HTMLURL, owner, nil); c != 200 {
		t.Fatal("restored slide unreadable", c)
	}

	// A tampered manifest object is caught on the next cold read (new process, or the
	// bounded cache expiring), exactly as before.
	manifestRaw, err := store.Get(t.Context(), playerfixture.ManifestKey)
	if err != nil {
		t.Fatal(err)
	}
	put(playerfixture.ManifestKey, []byte(`{"version":1,"answer_key":"private","slides":[]}`))
	cold, coldCounter := newServer()
	defer cold.Close()
	if c, b := call(cold, "GET", manifestPath, owner, nil); c != 503 || bytes.Contains(b, []byte("private")) {
		t.Fatal("tampered manifest served on a cold read", c)
	}
	if got := coldCounter.take(); got != 1 {
		t.Fatalf("cold tampered read made %d object reads, want 1", got)
	}
	if c, _ := call(cold, "PUT", "/v1/lessons/"+lesson+"/position", owner, position); c != 503 {
		t.Fatal("position saved against a tampered manifest", c)
	}
	put(playerfixture.ManifestKey, manifestRaw)
	if c, _ := call(cold, "GET", manifestPath, owner, nil); c != 200 {
		t.Fatal("restored manifest unreadable", c)
	}
}
