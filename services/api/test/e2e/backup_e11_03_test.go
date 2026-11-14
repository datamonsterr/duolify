package e2e

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
)

func TestExpireOldFixtureDumpsAndRestoreOverwrittenObject(t *testing.T) {
	fixture := backuptest.New(t)
	now := time.Date(2026, 11, 13, 4, 0, 0, 0, time.UTC)
	const ledger = "account-deletions/v1/0000000000000000000000000000000000000000000000000000000000000001.json"
	seeded := []string{
		"2026-10-28/schema-1.dump.age",           // 16 days: expires
		"2026-10-29/schema-1.dump.age",           // 15 days: expires
		"2026-10-30/schema-1.dump.age",           // 14 days: kept
		"financial/2026-10-07/schema-1.dump.age", // 37 days: expires
		"financial/2026-10-08/schema-1.dump.age", // 36 days: expires
		"financial/2026-10-09/schema-1.dump.age", // 35 days: kept
		"financial/2026-10-29/schema-1.dump.age", // outlives its general sibling
		ledger,
		"operator-notes.txt",
	}
	for _, key := range seeded {
		fixture.SeedBackupObject(t, key, []byte("age-encryption.org/v1\nsynthetic"))
	}
	result, err := fixture.Expirer(now).Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if result.Deleted != 4 {
		t.Fatalf("deleted %d dumps, want 4", result.Deleted)
	}
	want := []string{
		"2026-10-30/schema-1.dump.age", "financial/2026-10-09/schema-1.dump.age", "financial/2026-10-29/schema-1.dump.age", ledger, "operator-notes.txt",
	}
	got := fixture.BackupKeys(t)
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("remaining backup keys %v, want %v", got, want)
	}
	fixture.AssertWriterCannotDelete(t)

	// Asset references are plain URLs; recovery must make the same URL serve the old bytes.
	assets := fixture.ApplicationStorage()
	const key = "slides/moon-phases.html"
	first, err := assets.PutVersioned(t.Context(), key, []byte("<p>original</p>"), nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := assets.GetCurrent(r.Context(), key)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write(data)
	}))
	defer server.Close()
	fetch := func() string {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/assets/"+key, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = response.Body.Close() }()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	if fetch() != "<p>original</p>" {
		t.Fatal("original asset not served")
	}
	if _, err := assets.PutVersioned(t.Context(), key, []byte("<p>mistake</p>"), nil); err != nil {
		t.Fatal(err)
	}
	if fetch() != "<p>mistake</p>" {
		t.Fatal("overwrite not visible")
	}
	recovery := fixture.Recovery()
	if _, err := recovery.RestoreVersion(context.Background(), key, first, ""); err == nil {
		t.Fatal("untracked recovery must be refused")
	}
	if fetch() != "<p>mistake</p>" {
		t.Fatal("refused recovery changed the asset")
	}
	restored, err := recovery.RestoreVersion(t.Context(), key, first, "recover-2026-11-13")
	if err != nil {
		t.Fatal(err)
	}
	if fetch() != "<p>original</p>" {
		t.Fatal("same asset URL did not return the recovered object")
	}
	versions, err := fixture.AssetOperator().ListVersions(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 3 || versions[0].ID != restored || versions[0].Metadata["duolify-recovery-job"] != "recover-2026-11-13" {
		t.Fatalf("recovery was not appended and tracked: %+v", versions)
	}
}
