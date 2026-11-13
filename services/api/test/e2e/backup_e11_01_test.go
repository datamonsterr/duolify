package e2e

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
)

func TestBackupWorkerRecoversSyntheticDatabase(t *testing.T) {
	fixture := backuptest.New(t)
	fixture.Seed(t)
	fixture.UsePackagedWorker(t)
	date := time.Now().UTC().Format(time.DateOnly)
	key := fixture.ArchiveKey(date)
	fixture.RunJob(t, date)
	before := fixture.Ciphertext(t, key)
	fixture.RunJob(t, date)
	if got := fixture.Ciphertext(t, key); string(got) != string(before) {
		t.Fatal("retry replaced an already completed backup")
	}
	restored := fixture.Restore(t, before)
	fixture.AssertRestored(t, restored)
	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{Postgres: restored.Ping}, fixture.Logger))
	defer server.Close()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("restored HTTP status: %d", response.StatusCode)
	}
	fixture.AssertEvents(t, "backup_completed", "backup_completed")
	fixture.AssertWriterCannotDelete(t)
}
