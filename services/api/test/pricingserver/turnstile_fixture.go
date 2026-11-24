package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/accountstest"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Browser scenarios share the normal fixture database and MailSink. Each prefix
// uses the real router with a fixed, offline Siteverify secret for that scenario.
func registerTurnstileFixtures(mux *http.ServeMux, pool *pgxpool.Pool, sink *accounts.MailSink, limits accounts.RateLimits, previews *sharing.Service, logger *slog.Logger) error {
	for mode, secret := range map[string]string{"pass": accountstest.TurnstilePassSecret, "fail": accountstest.TurnstileFailSecret} {
		auth, err := accounts.New(pool, "test-only-signing-key-at-least-32-bytes", sink)
		if err != nil {
			return err
		}
		auth.ConfigureRateLimits(limits)
		auth.ConfigureWebOrigins(accounts.WebOriginsFromEnv(os.Getenv))
		verifier := accounts.NewSiteverify(secret, "", []string{accountstest.TurnstileHostname}, accountstest.TurnstileClient())
		gate := accounts.NewTurnstile(accountstest.TurnstileSiteKey, verifier, nil)
		auth.ConfigureTurnstile(gate)
		api := httpapi.New(httpapi.Dependencies{Accounts: auth, Sharing: previews.WithTurnstile(gate)}, logger)
		prefix := "/test/turnstile/" + mode
		mounted := http.StripPrefix(prefix, api)
		mux.HandleFunc(prefix+"/", func(w http.ResponseWriter, r *http.Request) {
			// Keep these scenarios out of the normal browser preview rate-limit bucket.
			r = r.Clone(r.Context())
			r.RemoteAddr = "192.0.2.40:12345"
			mounted.ServeHTTP(w, r)
		})
	}
	return nil
}
