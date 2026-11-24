package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

const testTurnstileSecret = "1x0000000000000000000000000000000AA"

type siteverifyTransport func(*http.Request) (*http.Response, error)

func (f siteverifyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func siteverifyReply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}

func TestTurnstileSiteverifyResults(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		want       TurnstileReason
	}{
		{"success", `{"success":true,"hostname":"example.com","action":"sign_up"}`, 200, TurnstileOK},
		{"wrong hostname", `{"success":true,"hostname":"other.example.com","action":"sign_up"}`, 200, TurnstileMismatch},
		{"wrong action", `{"success":true,"hostname":"example.com","action":"email_code"}`, 200, TurnstileMismatch},
		{"missing action", `{"success":true,"hostname":"example.com"}`, 200, TurnstileMismatch},
		{"invalid", `{"success":false,"error-codes":["invalid-input-response"]}`, 200, TurnstileInvalid},
		{"spent", `{"success":false,"error-codes":["timeout-or-duplicate"]}`, 200, TurnstileExpired},
		{"provider error", `{"success":false,"error-codes":["internal-error"]}`, 200, TurnstileUnavailable},
		{"malformed", `{"success":`, 200, TurnstileUnavailable},
		{"oversized", strings.Repeat(" ", 20_000) + `{"success":true,"hostname":"example.com","action":"sign_up"}`, 200, TurnstileUnavailable},
		{"HTTP failure", `{"success":true,"hostname":"example.com","action":"sign_up"}`, 503, TurnstileUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: siteverifyTransport(func(r *http.Request) (*http.Response, error) {
				defer func() { _ = r.Body.Close() }()
				return siteverifyReply(tc.status, tc.body), nil
			})}
			v := NewSiteverify(testTurnstileSecret, "", []string{"example.com"}, client)
			if got := v.Verify(t.Context(), "proof", "192.0.2.2", TurnstileSignUp); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTurnstileRetriesTransportOnceWithSameIdempotencyKey(t *testing.T) {
	var keys []string
	client := &http.Client{Transport: siteverifyTransport(func(req *http.Request) (*http.Response, error) {
		defer func() { _ = req.Body.Close() }()
		var body map[string]string
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if req.Method != http.MethodPost || req.URL.String() != "https://challenges.cloudflare.com/turnstile/v0/siteverify" || req.Header.Get("Content-Type") != "application/json" || body["secret"] != testTurnstileSecret || body["response"] != "proof" || body["remoteip"] != "192.0.2.2" {
			t.Fatal("unexpected Siteverify contract")
		}
		if _, err := uuid.Parse(body["idempotency_key"]); err != nil {
			t.Fatalf("expected a UUID idempotency key: %v", err)
		}
		keys = append(keys, body["idempotency_key"])
		return nil, errors.New("synthetic transport failure")
	})}
	v := NewSiteverify(testTurnstileSecret, "", []string{"example.com"}, client)
	if got := v.Verify(t.Context(), "proof", "192.0.2.2", TurnstileSignUp); got != TurnstileUnavailable || len(keys) != 2 || keys[0] != keys[1] {
		t.Fatalf("expected two attempts with one idempotency key, got %q with %d attempts", got, len(keys))
	}
	for _, tc := range []struct {
		proof string
		want  TurnstileReason
	}{{"  ", TurnstileMissing}, {strings.Repeat("a", 2049), TurnstileInvalid}} {
		if got := v.Verify(t.Context(), tc.proof, "", TurnstileSignUp); got != tc.want || len(keys) != 2 {
			t.Fatalf("malformed proof reached Siteverify: %q, %d calls", got, len(keys))
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got := v.Verify(ctx, "proof", "", TurnstileSignUp); got != TurnstileUnavailable || len(keys) != 2 {
		t.Fatalf("cancelled verification reached Siteverify: %q, %d calls", got, len(keys))
	}
}

func TestTurnstileConfigDefaultsOffAndRequiresMatchingHostnameConfig(t *testing.T) {
	values := map[string]string{}
	getenv := func(name string) string { return values[name] }
	if gate, err := TurnstileFromEnv(getenv, nil); gate != nil || err != nil {
		t.Fatal("empty environment must disable Turnstile", err)
	}
	values["TURNSTILE_ENABLED"] = "tru"
	if _, err := TurnstileFromEnv(getenv, nil); err == nil {
		t.Fatal("misspelled flag should fail configuration")
	}
	values["TURNSTILE_ENABLED"] = "true"
	values["TURNSTILE_SECRET_KEY"] = testTurnstileSecret
	values["TURNSTILE_SITE_KEY"] = "1x00000000000000000000AA"
	for _, hosts := range []string{"", "https://example.com", "*.example.com", "example.com,"} {
		values["TURNSTILE_ALLOWED_HOSTNAMES"] = hosts
		if _, err := TurnstileFromEnv(getenv, nil); err == nil {
			t.Fatalf("accepted invalid hostname configuration %q", hosts)
		}
	}
	values["TURNSTILE_ALLOWED_HOSTNAMES"] = "Example.COM, tauri.localhost"
	if gate, err := TurnstileFromEnv(getenv, nil); gate == nil || err != nil {
		t.Fatal("valid configuration did not enable Turnstile", err)
	}
}
