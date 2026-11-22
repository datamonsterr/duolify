//go:build integration

package accounts_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/accountstest"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestPlaybackSettingsDefaultsPersistExportAndErasure(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("playback"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	auth.ConfigureProfile(accounts.ProfileOptions{TextLimit: 20})
	auth.ConfigureDeletionLedger(accountstest.NewDeletionLedger())
	call := func(method, path string, body any, token string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req := httptest.NewRequestWithContext(ctx, method, path, strings.NewReader(string(b)))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		auth.ServeHTTP(w, req)
		return w
	}
	email := "playback@example.test"
	if w := call("POST", "/v1/auth/code", map[string]any{"email": email, "age_confirmed": true}, ""); w.Code != 202 {
		t.Fatalf("code: %d %s", w.Code, w.Body)
	}
	w := call("POST", "/v1/auth/verify", map[string]any{"email": email, "code": mail.Code(email), "age_confirmed": true}, "")
	if w.Code != 200 {
		t.Fatalf("verify: %d %s", w.Code, w.Body)
	}
	var tokens accounts.Tokens
	if err = json.Unmarshal(w.Body.Bytes(), &tokens); err != nil {
		t.Fatal(err)
	}
	read := func() map[string]any {
		w := call("GET", "/v1/me/settings", nil, tokens.AccessToken)
		if w.Code != 200 {
			t.Fatalf("read: %d %s", w.Code, w.Body)
		}
		var v map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	names := []string{"fullscreen_on_rotate", "autoplay", "auto_send_after_dictation"}
	// An existing Learner (row created before any save) gets true defaults.
	got := read()
	for _, n := range names {
		if got[n] != true {
			t.Fatalf("default %s = %v", n, got[n])
		}
	}
	// The profile (context) API must not carry settings fields.
	pw := call("GET", "/v1/me/profile", nil, tokens.AccessToken)
	for _, n := range names {
		if strings.Contains(pw.Body.String(), n) {
			t.Fatalf("profile leaks %s: %s", n, pw.Body)
		}
	}
	if w := call("POST", "/v1/me/settings", map[string]any{"confirmed": true, "fields": map[string]any{"autoplay": "no"}}, tokens.AccessToken); w.Code != 400 {
		t.Fatalf("invalid: %d", w.Code)
	}
	if w := call("POST", "/v1/me/settings", map[string]any{"confirmed": true, "fields": map[string]any{"fullscreen_on_rotate": false, "autoplay": false}}, tokens.AccessToken); w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	got = read()
	if got["fullscreen_on_rotate"] != false || got["autoplay"] != false || got["auto_send_after_dictation"] != true {
		t.Fatalf("partial update: %v", got)
	}
	if _, err = db.Pool.Exec(ctx, "UPDATE auth_sessions SET reauthenticated_at=now()"); err != nil {
		t.Fatal(err)
	}
	export := call("GET", "/v1/me/data-export", nil, tokens.AccessToken)
	if export.Code != 200 {
		t.Fatalf("export: %d %s", export.Code, export.Body)
	}
	var payload struct {
		Tables map[string][]map[string]any `json:"tables"`
	}
	if err = json.Unmarshal(export.Body.Bytes(), &payload); err != nil || len(payload.Tables["learner_profiles"]) != 1 {
		t.Fatalf("export shape: %v %s", err, export.Body)
	}
	row := payload.Tables["learner_profiles"][0]
	if row["fullscreen_on_rotate"] != false || row["autoplay"] != false || row["auto_send_after_dictation"] != true {
		t.Fatalf("export lacks settings: %v", row)
	}
	var learnerID string
	if err = db.Pool.QueryRow(ctx, "SELECT learner_id::text FROM auth_identities WHERE email=$1", email).Scan(&learnerID); err != nil {
		t.Fatal(err)
	}
	if w := call("POST", "/v1/me/deletion-request", map[string]string{"confirmation": "DELETE"}, tokens.AccessToken); w.Code != 200 {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	var left int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM learner_profiles WHERE learner_id=$1::uuid", learnerID).Scan(&left); err != nil || left != 0 {
		t.Fatalf("profile survived erasure: %d %v", left, err)
	}
}
