package backup

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/s3fallback"
)

// An error from the new store that is not "not found" must surface, never read the old store.
func TestBackupReadFallbackOnlyOnNotFound(t *testing.T) {
	var oldReads int
	newServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }))
	oldServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { oldReads++; _, _ = w.Write([]byte("x")) }))
	defer newServer.Close()
	defer oldServer.Close()
	store, err := NewS3(S3Options{Endpoint: newServer.URL, Region: "r", Bucket: "new", AccessKey: "k", SecretKey: "s",
		Fallback: &s3fallback.Store{Endpoint: oldServer.URL, Region: "r", Bucket: "old", AccessKey: "k2", SecretKey: "s2"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.GetCurrent(t.Context(), "k"); err == nil {
		t.Error("GetCurrent read the old store after a 500 from the new store")
	}
	if _, err = store.GetVersion(t.Context(), "k", "v1"); err == nil {
		t.Error("GetVersion read the old store after a 500 from the new store")
	}
	if ok, err := store.Exists(t.Context(), "k"); err == nil || ok {
		t.Errorf("Exists hid the new-store error: %v %v", ok, err)
	}
	if oldReads != 0 {
		t.Errorf("old store got %d reads", oldReads)
	}
}
