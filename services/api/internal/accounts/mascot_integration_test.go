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

func TestMascotSettingsPersistFallbackExportAndErasure(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("mascot"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	email := "mascot@example.test"
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
	set := func(m any) *httptest.ResponseRecorder {
		return call("POST", "/v1/me/settings", map[string]any{"confirmed": true, "fields": map[string]any{"mascot": m}}, tokens.AccessToken)
	}
	mascotOf := func() map[string]any { return read()["mascot"].(map[string]any) }
	// Defaults for an existing Learner.
	if m := mascotOf(); m["mode"] != "default" || m["companion_visible"] != true {
		t.Fatalf("default mascot: %v", m)
	}
	blue := map[string]any{"body": "#2F8FE8", "bodyEdge": "#2271BD", "trim": "#1A5490", "visor": "#0E2606", "eye": "#DDF1FF", "accent": "#FFC233"}
	if w := set(map[string]any{"palette": map[string]any{"body": "#123456"}}); w.Code != 400 {
		t.Fatalf("invalid palette: %d", w.Code)
	}
	if w := set(map[string]any{"accessories": []string{"bowtie", "badge", "scarf"}}); w.Code != 400 {
		t.Fatalf("too many accessories: %d", w.Code)
	}
	if w := set(map[string]any{"palette": blue, "accessories": []string{"bowtie"}}); w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	// Partial update keeps omitted fields.
	if w := set(map[string]any{"companion_visible": false}); w.Code != 200 {
		t.Fatalf("partial: %d %s", w.Code, w.Body)
	}
	m := mascotOf()
	if m["companion_visible"] != false || m["palette"] == nil || len(m["accessories"].([]any)) != 1 {
		t.Fatalf("partial update lost fields: %v", m)
	}
	var learnerID string
	if err = db.Pool.QueryRow(ctx, "SELECT learner_id::text FROM auth_identities WHERE email=$1", email).Scan(&learnerID); err != nil {
		t.Fatal(err)
	}
	// A Character the Learner does not own is rejected; an owned one is accepted, then falls back when deleted.
	var other, mine string
	if err = db.Pool.QueryRow(ctx, "INSERT INTO learners(display_name) VALUES('Other') RETURNING id::text").Scan(&other); err != nil {
		t.Fatal(err)
	}
	var foreign string
	if err = db.Pool.QueryRow(ctx, "INSERT INTO characters(learner_id) VALUES($1::uuid) RETURNING id::text", other).Scan(&foreign); err != nil {
		t.Fatal(err)
	}
	if w := set(map[string]any{"mode": "character", "character_id": foreign}); w.Code != 400 {
		t.Fatalf("foreign character: %d", w.Code)
	}
	if err = db.Pool.QueryRow(ctx, "INSERT INTO characters(learner_id) VALUES($1::uuid) RETURNING id::text", learnerID).Scan(&mine); err != nil {
		t.Fatal(err)
	}
	if w := set(map[string]any{"mode": "character", "character_id": mine}); w.Code != 200 {
		t.Fatalf("own character: %d %s", w.Code, w.Body)
	}
	if m := mascotOf(); m["mode"] != "character" || m["character_id"] != mine {
		t.Fatalf("character mode: %v", m)
	}
	if _, err = db.Pool.Exec(ctx, "UPDATE characters SET deleted_at=now() WHERE id=$1::uuid", mine); err != nil {
		t.Fatal(err)
	}
	if m := mascotOf(); m["mode"] != "default" || m["character_id"] != nil || len(m["accessories"].([]any)) != 1 {
		t.Fatalf("deleted character must fall back to default Buo: %v", m)
	}
	if _, err = db.Pool.Exec(ctx, "UPDATE auth_sessions SET reauthenticated_at=now()"); err != nil {
		t.Fatal(err)
	}
	export := call("GET", "/v1/me/data-export", nil, tokens.AccessToken)
	var payload struct {
		Tables map[string][]map[string]any `json:"tables"`
	}
	if err = json.Unmarshal(export.Body.Bytes(), &payload); err != nil || len(payload.Tables["learner_profiles"]) != 1 || payload.Tables["learner_profiles"][0]["mascot"] == nil {
		t.Fatalf("export lacks mascot: %v %s", err, export.Body)
	}
	if w := call("POST", "/v1/me/deletion-request", map[string]string{"confirmation": "DELETE"}, tokens.AccessToken); w.Code != 200 {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	var left int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM learner_profiles WHERE learner_id=$1::uuid", learnerID).Scan(&left); err != nil || left != 0 {
		t.Fatalf("mascot survived erasure: %d %v", left, err)
	}
}
