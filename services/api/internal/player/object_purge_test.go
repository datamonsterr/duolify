package player

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestPurgePrefixDeletesVersionsAndMarkers(t *testing.T) {
	const prefix = "lessons/generated/11111111-1111-4111-8111-111111111111/"
	var deleted []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/xml")
			_, _ = fmt.Fprintf(w, `<ListVersionsResult><IsTruncated>false</IsTruncated><Version><Key>%sslide.html</Key><VersionId>v1</VersionId></Version><Version><Key>%sslide.html</Key><VersionId>v2</VersionId></Version><DeleteMarker><Key>%shidden.html</Key><VersionId>marker</VersionId></DeleteMarker></ListVersionsResult>`, prefix, prefix, prefix)
			return
		}
		if r.Method == http.MethodDelete {
			deleted = append(deleted, r.URL.Query().Get("versionId"))
			w.WriteHeader(http.StatusNoContent)
			return
		}
		t.Errorf("unexpected method %s", r.Method)
	}))
	defer server.Close()
	env := map[string]string{"S3_ENDPOINT": server.URL, "S3_BUCKET": "app", "S3_REGION": "us-east-1", "S3_ACCESS_KEY_ID": "test", "S3_SECRET_ACCESS_KEY": "test", "S3_VERSIONING": "enabled"}
	store := NewS3(func(k string) string { return env[k] })
	if err := store.PurgePrefix(t.Context(), prefix); err != nil {
		t.Fatal(err)
	}
	slices.Sort(deleted)
	if !slices.Equal(deleted, []string{"marker", "v1", "v2"}) {
		t.Fatalf("deleted versions %v", deleted)
	}
}
