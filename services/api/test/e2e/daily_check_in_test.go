package e2e

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
)

func TestDailyCheckInHTTPAndPrivacy(t *testing.T) {
	auth, token, db := signedInDatabase(t)
	svc := credits.NewService(db.Pool, nil)
	api := httpapi.New(httpapi.Dependencies{Accounts: auth, Credits: svc}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	call := func(method, path, body string, authenticated bool) *httptest.ResponseRecorder {
		r := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
		if authenticated {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		return w
	}
	if w := call("POST", "/v1/me/check-in", "", false); w.Code != 401 {
		t.Fatalf("unauthenticated: %d %s", w.Code, w.Body)
	}
	for i := 0; i < 2; i++ {
		w := call("POST", "/v1/me/check-in", "", true)
		if w.Code != 200 {
			t.Fatalf("check-in: %d %s", w.Code, w.Body)
		}
		var result struct {
			Granted   int
			FirstTime bool `json:"first_time"`
			Bank      int  `json:"banked_checkin"`
			Cap       int
			Next      string `json:"next_check_in_at"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Bank != 400 || result.Cap != 1050 || result.Next == "" || (i == 0 && (result.Granted != 400 || !result.FirstTime)) || (i == 1 && (result.Granted != 0 || result.FirstTime)) {
			t.Fatalf("response %d: %+v", i, result)
		}
	}
	w := call("GET", "/v1/me/data-export", "", true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "credit_check_ins") || !strings.Contains(w.Body.String(), "check_in_bonus") {
		t.Fatalf("export: %d %s", w.Code, w.Body)
	}
	w = call("POST", "/v1/me/deletion-request", "{\"confirmation\":\"DELETE\"}", true)
	if w.Code != 200 {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	var count int
	if err := db.Pool.QueryRow(t.Context(), "SELECT count(*) FROM credit_check_ins").Scan(&count); err != nil || count != 0 {
		t.Fatalf("attendance retained: %d %v", count, err)
	}
}
