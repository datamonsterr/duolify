package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiddleware(t *testing.T) {
	for _, tt := range []struct {
		name, origin, method string
		panicHandler         bool
		want                 int
		cors                 bool
	}{
		{"normal", "", http.MethodGet, false, 200, false},
		{"tauri", "tauri://localhost", http.MethodGet, false, 200, true},
		{"windows tauri", "http://tauri.localhost", http.MethodGet, false, 200, true},
		{"https tauri", "https://tauri.localhost", http.MethodGet, false, 200, true},
		{"vite", "http://localhost:1420", http.MethodOptions, false, 204, true},
		{"unknown origin", "https://evil.example", http.MethodGet, false, 403, false},
		{"panic", "tauri://localhost", http.MethodGet, true, 500, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			handler := Middleware(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if RequestID(r.Context()) == "" {
					t.Error("missing context request id")
				}
				if tt.panicHandler {
					panic("private panic message")
				}
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequestWithContext(t.Context(), tt.method, "/healthz?secret=hidden", nil)
			req.Header.Set("Origin", tt.origin)
			req.Header.Set("X-Request-ID", "untrusted-id")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != tt.want {
				t.Fatalf("code=%d body=%s", recorder.Code, recorder.Body.String())
			}
			id := recorder.Header().Get("X-Request-ID")
			if id == "" || id == "untrusted-id" {
				t.Fatalf("bad id %q", id)
			}
			if (recorder.Header().Get("Access-Control-Allow-Origin") == tt.origin && tt.origin != "") != tt.cors {
				t.Error("bad CORS")
			}
			if tt.cors && !strings.Contains(recorder.Header().Get("Access-Control-Allow-Headers"), "X-Quote-ID") {
				t.Error("preview quote header blocked by CORS")
			}
			if tt.cors && !strings.Contains(recorder.Header().Get("Access-Control-Allow-Headers"), "X-App-Version") {
				t.Error("lesson test app version header blocked by CORS")
			}

			if strings.Contains(recorder.Body.String(), "private panic message") {
				t.Error("panic exposed")
			}
			lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
			var last map[string]any
			for _, line := range lines {
				if err := json.Unmarshal([]byte(line), &last); err != nil {
					t.Fatal(err)
				}
			}
			if last["status"] != float64(tt.want) || last["request_id"] != id {
				t.Fatalf("log=%v", last)
			}
			if strings.Contains(logs.String(), "hidden") {
				t.Error("query leaked to logs")
			}
		})
	}
}
