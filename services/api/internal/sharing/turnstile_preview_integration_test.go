//go:build integration

package sharing_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/accountstest"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
)

func TestTurnstileSharePreviewIntegration(t *testing.T) {
	f := newSeededSharingFixture(t)
	s := newService(t, f, startEngine(t), &capture{})
	course, _ := seedPublishableCourse(t, f, s)
	link, err := s.PublishLink(t.Context(), owner, course)
	if err != nil {
		t.Fatal(err)
	}
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			time.Sleep(150 * time.Millisecond)
			return
		}
		accountstest.FakeSiteverify(w, r)
	}))
	t.Cleanup(fixture.Close)
	call := func(secret, path, hostname, proof string, timeout time.Duration) *httptest.ResponseRecorder {
		gate := accounts.NewTurnstile(accountstest.TurnstileSiteKey, accounts.NewSiteverify(secret, fixture.URL+path, []string{hostname}, &http.Client{Timeout: timeout}), nil)
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/public/shares/"+link, nil)
		req.SetPathValue("token", link)
		if proof != "" {
			req.Header.Set("X-Turnstile-Token", proof)
		}
		w := httptest.NewRecorder()
		sharing.NewPreviewHandler(s.WithTurnstile(gate)).ServeHTTP(w, req)
		return w
	}
	const fresh = "share_preview:valid"
	if w := call(accountstest.TurnstilePassSecret, "", accountstest.TurnstileHostname, fresh, time.Second); w.Code != 200 || !strings.Contains(w.Body.String(), "Publish Test Course") {
		t.Fatalf("verified preview: %d %s", w.Code, w.Body)
	}
	for _, tc := range []struct {
		name, secret, path, hostname, proof string
		timeout                             time.Duration
	}{
		{"missing", accountstest.TurnstilePassSecret, "", accountstest.TurnstileHostname, "", time.Second},
		{"invalid", accountstest.TurnstileFailSecret, "", accountstest.TurnstileHostname, fresh, time.Second},
		{"spent", accountstest.TurnstileSpentSecret, "", accountstest.TurnstileHostname, fresh, time.Second},
		{"timeout", accountstest.TurnstilePassSecret, "/slow", accountstest.TurnstileHostname, fresh, 15 * time.Millisecond},
		{"wrong action", accountstest.TurnstilePassSecret, "", accountstest.TurnstileHostname, "email_code:wrong", time.Second},
		{"wrong hostname", accountstest.TurnstilePassSecret, "", "another.example.com", fresh, time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := call(tc.secret, tc.path, tc.hostname, tc.proof, tc.timeout)
			if w.Code != 400 || !strings.Contains(w.Body.String(), `"code":"turnstile_failed"`) || strings.Contains(w.Body.String(), "Publish Test Course") {
				t.Fatalf("warmed preview exposed after proof denial: %d %s", w.Code, w.Body)
			}
			if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Robots-Tag") != "noindex" {
				t.Fatal("proof denial lost public response policy")
			}
		})
	}
}
