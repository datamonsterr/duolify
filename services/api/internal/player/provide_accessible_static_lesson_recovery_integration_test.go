//go:build integration

package player_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/player"
)

func TestProvideAccessibleStaticLessonRecoveryIntegration(t *testing.T) {
	f := backuptest.New(t)
	f.Seed(t)
	mail := new(accounts.MailSink)
	auth, err := accounts.New(f.DB.Pool, strings.Repeat("static-recovery-test", 2), mail)
	if err != nil {
		t.Fatal(err)
	}

	service := player.New(player.Options{
		Pool:        f.DB.Pool,
		Objects:     f.ApplicationObjects(),
		AssetOrigin: "https://assets.example.test",
	})
	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{
		Accounts: auth,
		Player:   service,
	}, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()

	call := func(method, path, token string, body any) (int, []byte) {
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequestWithContext(t.Context(), method, server.URL+path, bytes.NewReader(raw))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, e := server.Client().Do(req)
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
		if c, b := call("POST", "/v1/auth/code", "", map[string]any{"email": email, "age_confirmed": true}); c != 202 {
			t.Fatal(c, string(b))
		}
		c, b := call("POST", "/v1/auth/verify", "", map[string]any{"email": email, "code": mail.Code(email), "age_confirmed": true})
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
	other := login("other-learner@example.test")
	const lesson = "f3333333-3333-4333-8333-333333333333"
	manifestPath := "/v1/lessons/" + lesson + "/manifest"

	// 1. Authorization checks
	if c, _ := call("GET", manifestPath, "", nil); c != 401 {
		t.Fatalf("expected 401 for unauthenticated request, got %d", c)
	}
	if c, _ := call("GET", manifestPath, other, nil); c != 404 {
		t.Fatalf("expected 404 for unenrolled learner, got %d", c)
	}

	// 2. Manifest explanation DTO contract
	c, b := call("GET", manifestPath, owner, nil)
	if c != 200 {
		t.Fatalf("manifest fetch failed: %d %s", c, string(b))
	}
	var m player.Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if len(m.Slides) == 0 {
		t.Fatal("expected slides in manifest")
	}

	// Verify each slide has a non-empty explanation DTO
	for _, slide := range m.Slides {
		if slide.Explanation == "" {
			t.Fatalf("slide %s missing explanation DTO", slide.ID)
		}
		if len(slide.Explanation) > 20000 {
			t.Fatalf("slide %s explanation exceeds bound: %d", slide.ID, len(slide.Explanation))
		}
	}

	// Verify no assessment answers or private object keys leaked
	bodyStr := string(b)
	for _, leak := range []string{"answer_key", "correct_answer", "html_key", "lessons/fixture-v1"} {
		if strings.Contains(bodyStr, leak) {
			t.Fatalf("manifest leaks private data (%s): %s", leak, bodyStr)
		}
	}

	// 3. Preferences storage and retrieval
	settingsBody := map[string]any{
		"confirmed": true,
		"fields": map[string]any{
			"motion":          "reduce",
			"appearance":      "dark",
			"captions":        true,
			"text_size":       1.2,
			"narration":       false,
			"narration_speed": 1.25,
		},
	}
	if c, resp := call("POST", "/v1/me/settings", owner, settingsBody); c != 200 {
		t.Fatalf("update settings: %d %s", c, string(resp))
	}
	if c, resp := call("GET", "/v1/me/settings", owner, nil); c != 200 {
		t.Fatalf("get settings: %d %s", c, string(resp))
	} else {
		var stored map[string]any
		_ = json.Unmarshal(resp, &stored)
		if stored["motion"] != "reduce" || stored["appearance"] != "dark" {
			t.Fatalf("preferences not preserved: %s", string(resp))
		}
	}

	// 4. Position preservation during recovery and focus restoration
	positionPath := "/v1/lessons/" + lesson + "/position"
	posInput := map[string]any{
		"version":  m.Version,
		"slide_id": m.Slides[0].ID,
		"time_ms":  1500,
	}
	if c, resp := call("PUT", positionPath, owner, posInput); c != 200 {
		t.Fatalf("save position: %d %s", c, string(resp))
	}
	if c, resp := call("GET", positionPath, owner, nil); c != 200 {
		t.Fatalf("get position: %d %s", c, string(resp))
	} else {
		var pos player.Position
		_ = json.Unmarshal(resp, &pos)
		if pos.SlideID != m.Slides[0].ID || pos.TimeMS != 1500 {
			t.Fatalf("unexpected restored position: %+v", pos)
		}
	}
}
