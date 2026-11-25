package accountstest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
)

// These keys are public Cloudflare fixtures. Use only with disposable tests.
const (
	TurnstileSiteKey     = "1x00000000000000000000AA"
	TurnstilePassSecret  = "1x0000000000000000000000000000000AA"
	TurnstileFailSecret  = "2x0000000000000000000000000000000AA"
	TurnstileSpentSecret = "3x0000000000000000000000000000000AA"
	TurnstileHostname    = "example.com"
)

// FakeSiteverify gives the documented dummy secrets deterministic responses and
// adds the action to a passing result so tests exercise production validation.
func FakeSiteverify(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Secret         string `json:"secret"`
		Response       string `json:"response"`
		IdempotencyKey string `json:"idempotency_key"`
	}
	if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&input) != nil || input.IdempotencyKey == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	action, token, found := strings.Cut(input.Response, ":")
	out := map[string]any{"success": false, "error-codes": []string{"invalid-input-response"}}
	switch {
	case input.Secret == TurnstileSpentSecret:
		out["error-codes"] = []string{"timeout-or-duplicate"}
	case input.Secret == TurnstilePassSecret && found && token != "":
		out = map[string]any{"success": true, "hostname": TurnstileHostname, "action": action}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// TurnstileClient keeps browser and API e2e runs offline, without another port.
func TurnstileClient() *http.Client {
	return &http.Client{Transport: turnstileTransport{}}
}

type turnstileTransport struct{}

func (turnstileTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Body != nil {
		defer func() { _ = r.Body.Close() }()
	}
	w := httptest.NewRecorder()
	if r.URL.String() != "https://challenges.cloudflare.com/turnstile/v0/siteverify" {
		w.WriteHeader(http.StatusNotFound)
	} else {
		FakeSiteverify(w, r)
	}
	return w.Result(), nil
}
