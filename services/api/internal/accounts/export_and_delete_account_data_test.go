package accounts_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
)

func TestLearnerReferenceIsStablePseudonym(t *testing.T) {
	got := accounts.LearnerReference("11111111-1111-4111-8111-111111111111")
	want := sha256.Sum256([]byte("11111111-1111-4111-8111-111111111111"))
	if got != hex.EncodeToString(want[:]) || len(got) != 64 {
		t.Fatalf("learner reference=%q", got)
	}
}

func TestClassifiedLearnerTablesCoversKnownTables(t *testing.T) {
	required := []string{
		"learner_profiles",
		"auth_identities",
		"auth_provider_identities",
		"auth_sessions",
		"credit_entries",
		"pricing_quotes",
		"pricing_admin_grants",
		"pricing_drafts",
		"pricing_audit",
		"pricing_version_cancellations",
	}
	for _, name := range required {
		policy, ok := accounts.ClassifiedLearnerTables[name]
		if !ok || policy == "" {
			t.Fatalf("expected classified table %q to have retention policy", name)
		}
	}
}

func TestPostHogPersonDeleterHandlesNilCredentials(t *testing.T) {
	if deleter := accounts.NewPostHogPersonDeleter("https://us.i.posthog.com", "", "key"); deleter != nil {
		t.Fatal("expected nil deleter with empty project ID")
	}
	if deleter := accounts.NewPostHogPersonDeleter("https://us.i.posthog.com", "proj", ""); deleter != nil {
		t.Fatal("expected nil deleter with empty api key")
	}
}

func TestPostHogPersonDeleterCallsEndpoint(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			t.Errorf("unexpected authorization header: %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected content type: %q", r.Header.Get("Content-Type"))
		}
		var payload struct {
			DistinctIDs      []string `json:"distinct_ids"`
			DeleteEvents     bool     `json:"delete_events"`
			DeleteRecordings bool     `json:"delete_recordings"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.DistinctIDs) != 1 || payload.DistinctIDs[0] != "test-learner-uuid" {
			t.Errorf("unexpected payload: %+v", payload)
		}
		if !payload.DeleteEvents || !payload.DeleteRecordings {
			t.Errorf("expected delete flags to be true: %+v", payload)
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"deletion_errors": []any{}})
	}))
	defer server.Close()

	deleter := accounts.NewPostHogPersonDeleter(server.URL, "test-proj", "test-api-key")
	if deleter == nil {
		t.Fatal("expected non-nil deleter")
	}
	if err := deleter.DeletePerson(context.Background(), "test-learner-uuid"); err != nil {
		t.Fatalf("unexpected DeletePerson error: %v", err)
	}
	if !called {
		t.Fatal("expected mock endpoint to be called")
	}
}

func TestPostHogPersonDeleterRejectsErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"deletion_errors": []string{"failed to delete"},
		})
	}))
	defer server.Close()

	deleter := accounts.NewPostHogPersonDeleter(server.URL, "test-proj", "test-api-key")
	if err := deleter.DeletePerson(context.Background(), "test-learner-uuid"); err == nil {
		t.Fatal("expected error on deletion errors response")
	}
}
