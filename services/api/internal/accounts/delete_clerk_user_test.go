package accounts_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
)

// fakeClerkUsers stands in for the Clerk Backend API; the tests never call Clerk.
type fakeClerkUsers struct {
	calls []string
	err   error
}

func (f *fakeClerkUsers) Delete(_ context.Context, id string) (*clerk.DeletedResource, error) {
	f.calls = append(f.calls, id)
	return &clerk.DeletedResource{ID: id, Deleted: f.err == nil}, f.err
}

func apiError(status int) error {
	return &clerk.APIErrorResponse{HTTPStatusCode: status, Errors: []clerk.Error{{Code: "resource_not_found"}}}
}

func TestClerkUserDeletionOutcomes(t *testing.T) {
	for name, tt := range map[string]struct {
		err  error
		fail bool
	}{
		"deleted":                  {},
		"already deleted at Clerk": {err: apiError(http.StatusNotFound)},
		"wrapped not found":        {err: errors.Join(errors.New("transport"), apiError(http.StatusNotFound))},
		"rate limited":             {err: apiError(http.StatusTooManyRequests), fail: true},
		"server error":             {err: apiError(http.StatusInternalServerError), fail: true},
		"unauthorized key":         {err: apiError(http.StatusUnauthorized), fail: true},
		"network":                  {err: errors.New("dial tcp: i/o timeout"), fail: true},
	} {
		t.Run(name, func(t *testing.T) {
			users := &fakeClerkUsers{err: tt.err}
			err := accounts.NewClerkUserDeleter(users).DeleteUser(t.Context(), "user_123")
			if (err != nil) != tt.fail {
				t.Fatalf("error = %v, want failure %v", err, tt.fail)
			}
			if len(users.calls) != 1 || users.calls[0] != "user_123" {
				t.Fatalf("calls = %v", users.calls)
			}
		})
	}
}

func TestClerkUserDeleterFromEnvIsOffWithoutKeys(t *testing.T) {
	env := func(values map[string]string) func(string) string {
		return func(k string) string { return values[k] }
	}
	if d, err := accounts.ClerkUserDeleterFromEnv(env(nil)); d != nil || err != nil {
		t.Fatal("deleter built without a Clerk key", d, err)
	}
	// The publishable key alone (SSO misconfiguration) never builds a deleter.
	if d, err := accounts.ClerkUserDeleterFromEnv(env(map[string]string{"CLERK_PUBLISHABLE_KEY": "pk_test_x"})); d != nil || err != nil {
		t.Fatal(d, err)
	}
	if _, err := accounts.ClerkUserDeleterFromEnv(env(map[string]string{"CLERK_SECRET_KEY": "not-a-secret-key"})); err == nil {
		t.Fatal("non-secret key accepted")
	}
	if d, err := accounts.ClerkUserDeleterFromEnv(env(map[string]string{"CLERK_SECRET_KEY": "sk_test_abc"})); d == nil || err != nil {
		t.Fatal("configured deleter missing", err)
	}
}

type spyDeleter struct{ calls int }

func (s *spyDeleter) DeleteUser(context.Context, string) error { s.calls++; return nil }

// With no Clerk configured the eraser is disabled and touches neither Clerk nor the database
// (a nil pool would panic).
func TestClerkUserEraserDisabledWithoutClerk(t *testing.T) {
	eraser := accounts.NewClerkUserEraser(nil, nil)
	if eraser.Enabled() {
		t.Fatal("enabled without a deleter")
	}
	if n, err := eraser.ProcessClerkUserDeletions(t.Context()); n != 0 || err != nil {
		t.Fatal(n, err)
	}
	var nilEraser *accounts.ClerkUserEraser
	if nilEraser.Enabled() {
		t.Fatal("nil eraser enabled")
	}
	if !accounts.NewClerkUserEraser(nil, &spyDeleter{}).Enabled() {
		t.Fatal("configured eraser disabled")
	}
}
