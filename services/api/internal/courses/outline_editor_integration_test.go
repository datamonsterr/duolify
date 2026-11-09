//go:build integration

package courses_test

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
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestOutlineEditorIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("outline"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	// No Engine, Credits or Pricing dependency is supplied. Every edit must work.
	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{Accounts: auth, CourseBrief: courses.New(courses.Options{Pool: db.Pool})}, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()
	call := func(method, path string, body any, token string) (int, map[string]any) {
		raw, _ := json.Marshal(body)
		req, err := http.NewRequestWithContext(ctx, method, server.URL+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	login := func(email string) string {
		if code, _ := call("POST", "/v1/auth/code", map[string]any{"email": email, "age_confirmed": true}, ""); code != 202 {
			t.Fatal(code)
		}
		code, out := call("POST", "/v1/auth/verify", map[string]any{"email": email, "age_confirmed": true, "code": mail.Code(email)}, "")
		if code != 200 {
			t.Fatal(code, out)
		}
		return out["access_token"].(string)
	}
	token := login("outline@example.test")
	other := login("outline-other@example.test")
	code, brief := call("POST", "/v1/course-brief/confirm", map[string]any{"confirmed": true, "brief": map[string]string{"topic": "Sky"}}, token)
	if code != 201 {
		t.Fatal(code, brief)
	}
	outline := []any{map[string]any{"id": "u1", "title": "Moon", "lessons": []any{map[string]string{"id": "l1", "title": "Phases", "goal": "Explain phases"}}}}
	code, draft := call("POST", "/v1/outline-drafts", map[string]any{"brief_revision": brief["id"], "outline": outline}, token)
	if code != 201 {
		t.Fatalf("create draft: %d %v", code, draft)
	}
	path := "/v1/outline-drafts/" + draft["id"].(string)
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		if code, _ := call(method, path, map[string]any{"expected_revision": 1, "outline": outline, "history": []any{}}, other); code != 404 {
			t.Fatalf("ownership %s %d", method, code)
		}
	}
	if code, _ := call("POST", "/v1/outline-drafts", map[string]any{"brief_revision": brief["id"], "outline": outline}, other); code != 404 {
		t.Fatal("foreign brief", code)
	}
	invalid := []any{map[string]any{"id": "u1", "title": "Moon", "lessons": []any{map[string]string{"id": "u1", "title": "Bad", "goal": ""}}}}
	if code, _ := call("PUT", path, map[string]any{"expected_revision": 1, "outline": invalid, "history": []any{}}, token); code != 400 {
		t.Fatal("invalid schema", code)
	}
	history := []any{map[string]any{"value": outline, "focusId": "l1"}}
	changed := []any{map[string]any{"id": "u1", "title": "Edited Moon", "lessons": []any{map[string]string{"id": "l1", "title": "Phases", "goal": "Explain phases"}}}}
	var wg sync.WaitGroup
	results := make(chan map[string]any, 2)
	codes := make(chan int, 2)
	for i := range 2 {
		incoming := changed
		if i == 1 {
			incoming = []any{map[string]any{"id": "u1", "title": "Other device Moon", "lessons": []any{map[string]string{"id": "l1", "title": "Phases", "goal": "Explain phases"}}}}
		}
		wg.Go(func() {
			code, out := call("PUT", path, map[string]any{"expected_revision": 1, "outline": incoming, "history": history}, token)
			results <- out
			codes <- code
		})
	}
	wg.Wait()
	close(results)
	close(codes)
	counts := map[int]int{}
	for c := range codes {
		counts[c]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatal("CAS", counts)
	}
	code, latest := call("GET", "/v1/outline-drafts?brief_revision="+brief["id"].(string), nil, token)
	if code != 200 || latest["draft"].(map[string]any)["id"] != draft["id"] {
		t.Fatal("unselected recovery replaced head", code, latest)
	}
	for result := range results {
		if recovery, ok := result["recovery"].(map[string]any); ok {
			current := result["current"].(map[string]any)
			if recovery["outline"].([]any)[0].(map[string]any)["title"] == current["outline"].([]any)[0].(map[string]any)["title"] {
				t.Fatal("lost distinct concurrent content", result)
			}
			code, recovered := call("GET", "/v1/outline-drafts/"+recovery["id"].(string), nil, token)
			if code != 200 || recovered["revision"] != float64(1) {
				t.Fatal("lost conflict branch", code, recovered)
			}
		}
	}
	for _, body := range []map[string]any{
		{"expected_revision": 2, "outline": outline, "history": make([]any, 51)},
		{"expected_revision": 2, "outline": outline, "history": []any{}, "learner_id": "forged"},
	} {
		if code, _ := call("PUT", path, body, token); code != 400 {
			t.Fatal("invalid history/unknown fields", code)
		}
	}
	code, head := call("GET", path, nil, token)
	if code != 200 || head["revision"] != float64(2) || len(head["history"].([]any)) != 1 {
		t.Fatal("saved history", code, head)
	}
	if code, _ := call("POST", path+"/confirm", map[string]any{"expected_revision": 1}, token); code != 409 {
		t.Fatal("stale confirmation", code)
	}
	code, confirmed := call("POST", path+"/confirm", map[string]any{"expected_revision": 2}, token)
	if code != 200 || confirmed["revision"] != float64(2) || len(confirmed["content_hash"].(string)) != 64 {
		t.Fatal("confirmation", code, confirmed)
	}
	if code, _ := call("GET", path+"/confirmed?expected_revision=2", nil, token); code != 200 {
		t.Fatal(code)
	}
	if code, _ := call("PUT", path, map[string]any{"expected_revision": 2, "outline": outline, "history": []any{}}, token); code != 200 {
		t.Fatal("persist undo", code)
	}
	if code, _ := call("GET", path+"/confirmed?expected_revision=2", nil, token); code != 409 {
		t.Fatal("stale consumer", code)
	}
	code, export := call("GET", "/v1/me/data-export", nil, token)
	if code != 200 || len(export["tables"].(map[string]any)["outline_drafts"].([]any)) != 2 {
		t.Fatal("export", code, export)
	}
	var ledger, jobs int
	if err = db.Pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM credit_entries),(SELECT count(*) FROM credit_jobs)").Scan(&ledger, &jobs); err != nil || ledger != 0 || jobs != 0 {
		t.Fatal("manual editing charged", ledger, jobs, err)
	}
	if code, _ := call("DELETE", path, nil, token); code != 204 {
		t.Fatal("delete draft", code)
	}
	if code, _ := call("GET", path, nil, token); code != 404 {
		t.Fatal("deleted draft readable", code)
	}
	if code, _ := call("POST", "/v1/me/deletion-request", map[string]string{"confirmation": "DELETE"}, token); code != 200 {
		t.Fatal("account deletion", code)
	}
	var count int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM outline_drafts").Scan(&count); err != nil || count != 0 {
		t.Fatal("account cascade", count, err)
	}
}
