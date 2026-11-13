package eval

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/jackc/pgx/v5"
)

type reportErrorStore struct {
	db.DBTX
	writes int
}

func (s *reportErrorStore) QueryRow(context.Context, string, ...any) pgx.Row {
	s.writes++
	return reportErrorRow{}
}

type reportErrorRow struct{}

func (reportErrorRow) Scan(...any) error {
	return errors.New("private database error: eval_reports secret connection details")
}

func TestServeHTTPAnonymousReads(t *testing.T) {
	for _, path := range []string{"dashboards", "dashboards/reliability", "dashboards/cost", "alerts", "reports"} {
		t.Run(path, func(t *testing.T) {
			service := NewService(nil, nil, nil, nil)
			w := httptest.NewRecorder()
			service.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), "GET", "/v1/eval/"+path, nil))
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d body=%s; want 401", w.Code, w.Body)
			}
		})
	}
}

func TestServeHTTPReportSecurity(t *testing.T) {
	for _, tc := range []struct {
		name, configuredToken, authorization, body string
		status                                     int
	}{
		{"disabled when unset", "", "Bearer service-test-token", `{}`, http.StatusUnauthorized},
		{"empty token rejected when unset", "", "Bearer ", `{}`, http.StatusUnauthorized},
		{"missing token", "service-test-token", "", `{}`, http.StatusUnauthorized},
		{"wrong token", "service-test-token", "Bearer wrong-test-token", `{}`, http.StatusUnauthorized},
		{"bare token", "service-test-token", "service-test-token", `{}`, http.StatusUnauthorized},
		{"valid token reaches JSON validation", "service-test-token", "Bearer service-test-token", `{`, http.StatusBadRequest},
		{"oversized JSON", "service-test-token", "Bearer service-test-token", `{"details":{"padding":"` + strings.Repeat("x", 1<<20) + `"}}`, http.StatusRequestEntityTooLarge},
		{"oversized trailing whitespace", "service-test-token", "Bearer service-test-token", `{}` + strings.Repeat(" ", 1<<20), http.StatusRequestEntityTooLarge},
		{"multiple JSON values", "service-test-token", "Bearer service-test-token", `{} {}`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("EVAL_REPORT_SERVICE_TOKEN", tc.configuredToken)
			store := &reportErrorStore{}
			service := NewService(nil, nil, nil, nil)
			service.queries = db.New(store)
			req := httptest.NewRequestWithContext(t.Context(), "POST", "/v1/eval/reports", strings.NewReader(tc.body))
			// Unknown length exercises MaxBytesReader rather than Content-Length checks.
			req.ContentLength = -1
			req.Header.Set("Authorization", tc.authorization)
			w := httptest.NewRecorder()
			service.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s; want %d", w.Code, w.Body, tc.status)
			}
			if store.writes != 0 {
				t.Fatalf("rejected report reached persistence: %d writes", store.writes)
			}
		})
	}
}

func TestServeHTTPReportInternalError(t *testing.T) {
	t.Setenv("EVAL_REPORT_SERVICE_TOKEN", "service-test-token")
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	service := NewService(nil, nil, nil, nil)
	service.queries = db.New(&reportErrorStore{})
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/v1/eval/reports", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer service-test-token")
	w := httptest.NewRecorder()
	service.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError || strings.TrimSpace(w.Body.String()) != `{"error":"internal server error"}` {
		t.Fatalf("status=%d body=%s; want generic 500", w.Code, w.Body)
	}
	if !strings.Contains(logs.String(), "private database error: eval_reports secret connection details") {
		t.Fatalf("underlying error missing from server logs: %s", &logs)
	}
}
