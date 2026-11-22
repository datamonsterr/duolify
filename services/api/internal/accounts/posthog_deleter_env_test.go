package accounts_test

import (
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
)

func TestPostHogPersonDeleterFromEnvNeedsThePrivateKey(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	// The public project key alone never enables deletion.
	if accounts.PostHogPersonDeleterFromEnv(env(map[string]string{"POSTHOG_API_KEY": "phc_public", "POSTHOG_PROJECT_ID": "1"})) != nil {
		t.Fatal("enabled with the public project key only")
	}
	if accounts.PostHogPersonDeleterFromEnv(env(map[string]string{"POSTHOG_PERSONAL_API_KEY": "phx_private"})) != nil {
		t.Fatal("enabled without a project id")
	}
	if accounts.PostHogPersonDeleterFromEnv(env(map[string]string{"POSTHOG_PERSONAL_API_KEY": "phx_private", "POSTHOG_PROJECT_ID": "1"})) == nil {
		t.Fatal("not enabled with project id and personal key")
	}
}
