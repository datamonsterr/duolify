package e2e

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/eval"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
)

func TestEvalHTTPAuthorization(t *testing.T) {
	t.Setenv("EVAL_REPORT_SERVICE_TOKEN", "eval-service-test-token")
	auth, access, database := signedInDatabase(t)
	claims, err := auth.ParseAccess(access)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{
		Accounts: auth,
		Eval:     eval.NewService(database.Pool, nil, nil, nil),
	}, logger))
	t.Cleanup(server.Close)
	call := func(method, path, credential, body string) (int, string) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if credential != "" {
			req.Header.Set("Authorization", "Bearer "+credential)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, string(raw)
	}
	paths := []string{"dashboards", "dashboards/reliability", "dashboards/cost", "alerts", "reports"}
	for _, path := range paths {
		t.Run("anonymous cannot read "+path, func(t *testing.T) {
			if code, raw := call("GET", "/v1/eval/"+path, "", ""); code != http.StatusUnauthorized {
				t.Fatalf("status=%d body=%s; want 401", code, raw)
			}
		})
		t.Run("learner cannot read "+path, func(t *testing.T) {
			if code, raw := call("GET", "/v1/eval/"+path, access, ""); code != http.StatusForbidden {
				t.Fatalf("status=%d body=%s; want 403", code, raw)
			}
		})
	}
	if _, err = database.Pool.Exec(t.Context(), `INSERT INTO pricing_admin_grants(account_id) VALUES($1::uuid)`, claims.Subject); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		t.Run("admin can read "+path, func(t *testing.T) {
			if code, raw := call("GET", "/v1/eval/"+path, access, ""); code != http.StatusOK {
				t.Fatalf("status=%d body=%s; want 200", code, raw)
			}
		})
		t.Run("service token cannot read "+path, func(t *testing.T) {
			if code, raw := call("GET", "/v1/eval/"+path, "eval-service-test-token", ""); code != http.StatusUnauthorized {
				t.Fatalf("status=%d body=%s; want 401", code, raw)
			}
		})
	}
	report := `{"task":"course_preview","dataset_revision":"test","prompt_revision":"test","model":"mock","score":1,"threshold":0.85,"passed":true,"details":{}}`
	for _, tc := range []struct{ name, credential string }{
		{"anonymous", ""}, {"wrong service token", "forged-service-token"}, {"admin session", access},
	} {
		t.Run("forged report rejected for "+tc.name, func(t *testing.T) {
			if code, raw := call("POST", "/v1/eval/reports", tc.credential, report); code != http.StatusUnauthorized {
				t.Fatalf("status=%d body=%s; want 401", code, raw)
			}
		})
	}
	var count int
	if err = database.Pool.QueryRow(t.Context(), `SELECT count(*) FROM eval_reports`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unauthorized reports persisted: count=%d err=%v", count, err)
	}
	t.Run("service can ingest", func(t *testing.T) {
		if code, raw := call("POST", "/v1/eval/reports", "eval-service-test-token", report); code != http.StatusCreated {
			t.Fatalf("status=%d body=%s; want 201", code, raw)
		}
	})
	t.Run("oversized report rejected", func(t *testing.T) {
		body := `{"details":{"padding":"` + strings.Repeat("x", 1<<20) + `"}}`
		if code, raw := call("POST", "/v1/eval/reports", "eval-service-test-token", body); code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status=%d body=%s; want 413", code, raw)
		}
	})
	t.Run("internal errors are redacted", func(t *testing.T) {
		if _, err := database.Pool.Exec(t.Context(), `ALTER TABLE eval_reports RENAME TO eval_reports_unavailable`); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct{ method, credential string }{
			{http.MethodGet, access}, {http.MethodPost, "eval-service-test-token"},
		} {
			code, raw := call(tc.method, "/v1/eval/reports", tc.credential, report)
			if code != http.StatusInternalServerError || strings.TrimSpace(raw) != `{"error":"internal server error"}` {
				t.Fatalf("%s status=%d body=%s; want generic 500", tc.method, code, raw)
			}
		}
	})
	if _, err = database.Pool.Exec(t.Context(), `UPDATE auth_sessions SET reauthenticated_at=now()-interval '16 minutes' WHERE id=$1::uuid`, claims.ID); err != nil {
		t.Fatal(err)
	}
	t.Run("stale admin session rejected", func(t *testing.T) {
		if code, raw := call("GET", "/v1/eval/dashboards", access, ""); code != http.StatusForbidden {
			t.Fatalf("status=%d body=%s; want 403", code, raw)
		}
	})
}
