package player

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestContentTypeForKey(t *testing.T) {
	for key, want := range map[string]string{
		"lessons/examples/js/v1/1-0/slide-0.html": "text/html; charset=utf-8",
		"lessons/narration/v1/h/alignment.json":   "application/json",
		"lessons/narration/v1/h/audio.wav":        "audio/wav",
		"lessons/a/b.mp3":                         "audio/mpeg",
		"lessons/a/b.ogg":                         "audio/ogg",
		"lessons/a/b.webm":                        "audio/webm",
		"lessons/a/b.png":                         "image/png",
		"lessons/a/b.webp":                        "image/webp",
		"lessons/a/b.svg":                         "image/svg+xml",
		"lessons/a/b.jpg":                         "image/jpeg",
		"lessons/a/b.unknown":                     "application/octet-stream",
	} {
		if got := ContentTypeForKey(key); got != want {
			t.Errorf("%s: got %q want %q", key, got, want)
		}
	}
}

func TestPutSetsContentTypeAndCacheControl(t *testing.T) {
	var gotType, gotCache string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotType, gotCache = r.Header.Get("Content-Type"), r.Header.Get("Cache-Control")
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	env := map[string]string{"S3_ENDPOINT": server.URL, "S3_BUCKET": "app", "S3_REGION": "us-east-1", "S3_ACCESS_KEY_ID": "test", "S3_SECRET_ACCESS_KEY": "test"}
	store := NewS3(func(k string) string { return env[k] })
	if err := store.Put(t.Context(), "lessons/examples/js/v1/1-0/slide-0.html", []byte("<p>x</p>")); err != nil {
		t.Fatal(err)
	}
	if gotType != "text/html; charset=utf-8" || gotCache != "public, max-age=31536000, immutable" {
		t.Fatalf("type %q cache %q", gotType, gotCache)
	}
}
