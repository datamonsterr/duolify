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
	"github.com/datamonsterr/duolify/services/api/internal/retention"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestGuidedDraftsAreOwnerOnlyConditionalBoundedAndExpire(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("drafts"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	call := func(method, path, body, token string, headers ...string) *httptest.ResponseRecorder {
		r := httptest.NewRequestWithContext(ctx, method, path, strings.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		for i := 0; i+1 < len(headers); i += 2 {
			r.Header.Set(headers[i], headers[i+1])
		}
		w := httptest.NewRecorder()
		auth.ServeHTTP(w, r)
		return w
	}
	login := func(email string) string {
		if w := call("POST", "/v1/auth/code", `{"email":"`+email+`","age_confirmed":true}`, ""); w.Code != 202 {
			t.Fatalf("code: %d %s", w.Code, w.Body)
		}
		w := call("POST", "/v1/auth/verify", `{"email":"`+email+`","code":"`+mail.Code(email)+`","age_confirmed":true}`, "")
		var tokens accounts.Tokens
		if w.Code != 200 {
			t.Fatalf("login: %d %s", w.Code, w.Body)
		}
		if err := json.Unmarshal(w.Body.Bytes(), &tokens); err != nil {
			t.Fatal(err)
		}
		return tokens.AccessToken
	}
	const path = "/v1/me/guided-drafts/onboarding.setup"
	body := `{"script_version":3,"step_id":"level","answers":{"goal":{"value":"Learn Go"}}}`
	if w := call("GET", path, "", ""); w.Code != 401 {
		t.Fatalf("anonymous read: %d", w.Code)
	}
	owner, other := login("draft-owner@example.test"), login("draft-other@example.test")
	if w := call("GET", path, "", owner); w.Code != 404 {
		t.Fatalf("missing draft: %d", w.Code)
	}
	// Create needs an empty If-Match; a stale or invented tag conflicts.
	if w := call("PUT", path, body, owner, "If-Match", `"2020-01-01T00:00:00Z"`); w.Code != 409 {
		t.Fatalf("create with a tag: %d", w.Code)
	}
	w := call("PUT", path, body, owner)
	if w.Code != 200 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	tag := w.Header().Get("ETag")
	if tag == "" {
		t.Fatal("PUT must return an ETag")
	}
	var created struct {
		UpdatedAt string `json:"updated_at"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || `"`+created.UpdatedAt+`"` != tag {
		t.Fatalf("body updated_at %q must be the ETag %s", created.UpdatedAt, tag)
	}
	// Another Learner sees nothing and cannot overwrite or delete.
	if w := call("GET", path, "", other); w.Code != 404 {
		t.Fatalf("other learner read: %d", w.Code)
	}
	if w := call("DELETE", path, "", other); w.Code != 204 {
		t.Fatalf("other learner delete: %d", w.Code)
	}
	if w := call("GET", path, "", owner); w.Code != 200 || w.Header().Get("ETag") != tag || !strings.Contains(w.Body.String(), "Learn Go") {
		t.Fatalf("owner read after foreign delete: %d %s", w.Code, w.Body)
	}
	// Stale If-Match returns 409 with the current draft so the client can merge per step.
	next := `{"script_version":3,"step_id":"time","answers":{"goal":{"value":"Learn Go"},"level":{"value":"some"}}}`
	if w := call("PUT", path, next, owner); w.Code != 409 || !strings.Contains(w.Body.String(), "Learn Go") {
		t.Fatalf("missing If-Match: %d %s", w.Code, w.Body)
	}
	w = call("PUT", path, next, owner, "If-Match", tag)
	if w.Code != 200 || w.Header().Get("ETag") == tag {
		t.Fatalf("conditional save: %d %s", w.Code, w.Body)
	}
	if w := call("PUT", path, next, owner, "If-Match", tag); w.Code != 409 {
		t.Fatalf("reused tag: %d", w.Code)
	}
	// Bounds: oversize, unknown field, bad script id.
	tag = call("GET", path, "", owner).Header().Get("ETag")
	huge := `{"script_version":3,"step_id":"x","answers":{"notes":{"value":"` + strings.Repeat("y", accounts.MaxGuidedDraftBytes) + `"}}}`
	if w := call("PUT", path, huge, owner, "If-Match", tag); w.Code != 413 {
		t.Fatalf("oversize: %d", w.Code)
	}
	if w := call("PUT", path, `{"script_version":3,"step_id":"x","answers":{},"extra":1}`, owner, "If-Match", tag); w.Code != 400 {
		t.Fatalf("unknown field: %d", w.Code)
	}
	if w := call("PUT", "/v1/me/guided-drafts/Bad_Script", body, owner); w.Code != 404 {
		t.Fatalf("bad script id: %d", w.Code)
	}
	// Export includes the draft; an expired draft is invisible and purged by the worker.
	if w := call("GET", "/v1/me/data-export", "", owner); w.Code == 200 && !strings.Contains(w.Body.String(), "guided_chat_drafts") {
		t.Fatal("account export must include guided_chat_drafts")
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE guided_chat_drafts SET expires_at = now() - interval '1 minute'`); err != nil {
		t.Fatal(err)
	}
	if w := call("GET", path, "", owner); w.Code != 404 {
		t.Fatalf("expired draft readable: %d", w.Code)
	}
	res, err := retention.New(db.Pool, retention.Config{}, nil).PurgeExpiredRows(ctx)
	if err != nil || res.GuidedChatDrafts != 1 {
		t.Fatalf("purge: %+v %v", res, err)
	}
	// A fresh save after expiry starts over, then delete is idempotent.
	if w := call("PUT", path, body, owner); w.Code != 200 {
		t.Fatalf("recreate: %d %s", w.Code, w.Body)
	}
	for i := 0; i < 2; i++ {
		if w := call("DELETE", path, "", owner); w.Code != 204 {
			t.Fatalf("delete %d: %d", i, w.Code)
		}
	}
	// Erasure cascade.
	if w := call("PUT", path, body, owner); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if _, err := db.Pool.Exec(ctx, `DELETE FROM learners WHERE id = (SELECT learner_id FROM guided_chat_drafts LIMIT 1)`); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM guided_chat_drafts`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("drafts survived erasure: %d %v", left, err)
	}
}
