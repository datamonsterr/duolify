package player

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRewriteMetadataReplacesContentType(t *testing.T) {
	var copied bool
	var directive, source, gotType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			w.Header().Set("Content-Type", "application/octet-stream")
		case http.MethodPut:
			copied = true
			directive, source, gotType = r.Header.Get("X-Amz-Metadata-Directive"), r.Header.Get("X-Amz-Copy-Source"), r.Header.Get("Content-Type")
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<CopyObjectResult><ETag>"x"</ETag></CopyObjectResult>`))
		}
	}))
	defer server.Close()
	env := map[string]string{"S3_ENDPOINT": server.URL, "S3_BUCKET": "app", "S3_REGION": "us-east-1", "S3_ACCESS_KEY_ID": "test", "S3_SECRET_ACCESS_KEY": "test"}
	store := NewS3(func(k string) string { return env[k] })
	changed, err := store.RewriteMetadata(t.Context(), "lessons/x/slide-0.html", false)
	if err != nil || !changed || !copied {
		t.Fatalf("changed=%v copied=%v err=%v", changed, copied, err)
	}
	if directive != "REPLACE" || source != "app/lessons/x/slide-0.html" || gotType != "text/html; charset=utf-8" {
		t.Fatalf("directive %q source %q type %q", directive, source, gotType)
	}
}
