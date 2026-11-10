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
	"github.com/datamonsterr/duolify/services/api/internal/player/playerfixture"
)

func TestControlAndResumeSlidePlaybackIntegration(t *testing.T) {
	f := backuptest.New(t)
	f.Seed(t)
	mail := new(accounts.MailSink)
	auth, err := accounts.New(f.DB.Pool, strings.Repeat("cursor-test", 4), mail)
	if err != nil {
		t.Fatal(err)
	}
	handler := func() http.Handler {
		return httpapi.New(httpapi.Dependencies{Accounts: auth, Player: player.New(player.Options{Pool: f.DB.Pool, Objects: f.ApplicationObjects(), AssetOrigin: "https://assets.example.test"})}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}
	server := httptest.NewServer(handler())
	defer server.Close()
	call := func(method, path, token, body string) (int, []byte) {
		req, _ := http.NewRequestWithContext(t.Context(), method, server.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = response.Body.Close() }()
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, raw
	}
	login := func(email string) string {
		input := `{"email":"` + email + `","age_confirmed":true}`
		if code, raw := call("POST", "/v1/auth/code", "", input); code != 202 {
			t.Fatal(code, string(raw))
		}
		code, raw := call("POST", "/v1/auth/verify", "", `{"email":"`+email+`","age_confirmed":true,"code":"`+mail.Code(email)+`"}`)
		var token struct {
			Access string `json:"access_token"`
		}
		if err := json.Unmarshal(raw, &token); err != nil || code != 200 {
			t.Fatal(code, string(raw), err)
		}
		return token.Access
	}
	owner, other := login("synthetic@example.test"), login("cursor-other@example.test")
	const path = "/v1/lessons/f3333333-3333-4333-8333-333333333333/position"
	const input = `{"version":1,"slide_id":"sky-2","time_ms":1800}`
	for _, method := range []string{"GET", "PUT"} {
		for _, test := range []struct {
			token  string
			status int
		}{{"", 401}, {other, 404}} {
			if code, raw := call(method, path, test.token, input); code != test.status {
				t.Fatal(method, code, string(raw))
			}
		}
	}
	if code, raw := call("PUT", path, owner, input); code != 200 {
		t.Fatal("save", code, string(raw))
	}
	// A newly constructed service reads the durable cursor, not process memory.
	server.Close()
	server = httptest.NewServer(handler())
	defer server.Close()
	if code, raw := call("GET", path, owner, ""); code != 200 || !bytes.Contains(raw, []byte(`"time_ms":1800`)) || !bytes.Contains(raw, []byte(`"slide_id":"sky-2"`)) {
		t.Fatal("resume", code, string(raw))
	}
	for _, body := range []string{
		`{"version":0,"slide_id":"sky-2","time_ms":1800}`,
		`{"version":2,"slide_id":"sky-2","time_ms":1800}`,
		`{"version":1,"slide_id":"foreign","time_ms":0}`,
		`{"version":1,"slide_id":"sky-2","time_ms":-1}`,
		`{"version":1,"slide_id":"sky-2","time_ms":3001}`,
		`{"version":1,"slide_id":"sky-2","time_ms":1.5}`,
		`{"version":1,"slide_id":"sky-2","time_ms":1,"completed":true}`,
		`{"version":1,"slide_id":"sky-2"}`,
		input + ` {}`,
	} {
		if code, raw := call("PUT", path, owner, body); code != 400 {
			t.Fatal("invalid cursor accepted", body, code, string(raw))
		}
	}
	if code, raw := call("GET", "/v1/me/data-export", owner, ""); code != 200 || !bytes.Contains(raw, []byte("lesson_playback_positions")) {
		t.Fatal("cursor export", code)
	}
	// A valid authored slide ID may equal an endpoint name. It must still return
	// HTML through the slide route, rather than accidentally returning a cursor.
	var content player.ObjectManifest
	if err := json.Unmarshal(playerfixture.Documents()[playerfixture.ManifestKey], &content); err != nil {
		t.Fatal(err)
	}
	content.Slides[0].ID = "position"
	raw, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	const routeManifest = "lessons/route-position/manifest.json"
	if err := f.ApplicationObjects().Put(t.Context(), routeManifest, raw); err != nil {
		t.Fatal(err)
	}
	if _, err := f.DB.Pool.Exec(t.Context(), "UPDATE lesson_manifests SET manifest_key=$1 WHERE lesson_id='f3333333-3333-4333-8333-333333333333'", routeManifest); err != nil {
		t.Fatal(err)
	}
	if code, body := call("GET", "/v1/lessons/f3333333-3333-4333-8333-333333333333/slides/position", owner, ""); code != 200 || !bytes.Contains(body, []byte(`"html"`)) {
		t.Fatal("reserved-looking slide ID", code, string(body))
	}
	var state string
	if err := f.DB.Pool.QueryRow(t.Context(), "SELECT status FROM course_lesson_stubs WHERE id='f3333333-3333-4333-8333-333333333333'").Scan(&state); err != nil || state != "ready_to_generate" {
		t.Fatal("playback awarded completion", state, err)
	}
	if _, err := f.DB.Pool.Exec(t.Context(), "DELETE FROM course_enrollments WHERE course_id='f1111111-1111-4111-8111-111111111111'"); err != nil {
		t.Fatal(err)
	}
	if code, _ := call("PUT", path, owner, input); code != 404 {
		t.Fatal("revoked enrollment", code)
	}
	if _, err := f.DB.Pool.Exec(t.Context(), "INSERT INTO account_deletion_tombstones(learner_reference) VALUES($1)", accounts.LearnerReference("a1111111-1111-4111-8111-111111111111")); err != nil {
		t.Fatal(err)
	}
	if err := accounts.ApplyDeletionTombstones(t.Context(), f.DB.Pool); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := f.DB.Pool.QueryRow(t.Context(), "SELECT count(*) FROM lesson_playback_positions").Scan(&count); err != nil || count != 0 {
		t.Fatal("cursor survived deletion", count, err)
	}
}
