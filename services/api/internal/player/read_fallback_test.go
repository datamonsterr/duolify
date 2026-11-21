package player_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/player"
)

type recorder struct {
	mu      sync.Mutex
	methods []string
	objects map[string]string
}

func (r *recorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	r.methods = append(r.methods, req.Method)
	body, ok := r.objects[req.URL.Path]
	r.mu.Unlock()
	if req.Method == http.MethodGet && ok {
		_, _ = w.Write([]byte(body))
		return
	}
	if req.Method == http.MethodGet || req.Method == http.MethodHead {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (r *recorder) count(method string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, m := range r.methods {
		if m == method {
			n++
		}
	}
	return n
}

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestReadFallbackConfigValidation(t *testing.T) {
	base := map[string]string{"S3_ENDPOINT": "https://new.example.test", "S3_BUCKET": "new", "S3_REGION": "r", "S3_ACCESS_KEY_ID": "k", "S3_SECRET_ACCESS_KEY": "s"}
	store, err := player.LoadS3(env(base))
	if err != nil || store.Old != nil {
		t.Fatalf("fallback must be off by default: %v %v", store, err)
	}
	on := func(extra map[string]string) map[string]string {
		m := map[string]string{"S3_FALLBACK_ENABLED": "true"}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	if _, err = player.LoadS3(env(on(nil))); err == nil {
		t.Fatal("fallback on without an old store accepted")
	}
	old := map[string]string{"S3_OLD_ENDPOINT": "https://old.example.test", "S3_OLD_REGION": "r", "S3_OLD_BUCKET": "old", "S3_OLD_ACCESS_KEY_ID": "k2", "S3_OLD_SECRET_ACCESS_KEY": "s2"}
	if store, err = player.LoadS3(env(on(old))); err != nil || store.Old == nil || store.Old.Bucket != "old" || store.Bucket != "new" {
		t.Fatalf("complete config rejected: %v %v", store, err)
	}
	same := on(old)
	same["S3_OLD_ENDPOINT"], same["S3_OLD_BUCKET"] = "https://new.example.test", "new"
	if _, err = player.LoadS3(env(same)); err == nil {
		t.Fatal("old store equal to the new store accepted")
	}
}

func TestWritesNeverReachTheOldStore(t *testing.T) {
	fresh, legacy := &recorder{objects: map[string]string{}}, &recorder{objects: map[string]string{"/old/lessons/a.json": "{}"}}
	newServer, oldServer := httptest.NewServer(fresh), httptest.NewServer(legacy)
	defer newServer.Close()
	defer oldServer.Close()
	store, err := player.LoadS3(env(map[string]string{"S3_ENDPOINT": newServer.URL, "S3_BUCKET": "new", "S3_REGION": "r", "S3_ACCESS_KEY_ID": "k", "S3_SECRET_ACCESS_KEY": "s", "S3_FALLBACK_ENABLED": "true",
		"S3_OLD_ENDPOINT": oldServer.URL, "S3_OLD_BUCKET": "old", "S3_OLD_REGION": "r", "S3_OLD_ACCESS_KEY_ID": "k2", "S3_OLD_SECRET_ACCESS_KEY": "s2"}))
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(t.Context(), "lessons/a.json")
	if err != nil || string(got) != "{}" {
		t.Fatalf("old-store read through the fallback: %q %v", got, err)
	}
	_ = store.Put(t.Context(), "lessons/b.json", []byte("{}"))
	_ = store.Delete(t.Context(), "lessons/b.json")
	_, _ = store.Restore(t.Context(), "lessons/b.json")
	for _, method := range []string{http.MethodPut, http.MethodDelete, http.MethodPost} {
		if n := legacy.count(method); n != 0 {
			t.Fatalf("old store received %d %s requests", n, method)
		}
	}
	if fresh.count(http.MethodPut) == 0 {
		t.Fatal("write did not reach the new store")
	}
}

// An error from the new store that is not "not found" must surface, never read the old store.
func TestReadFallbackOnlyOnNotFound(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusInternalServerError, http.StatusServiceUnavailable} {
		legacy := &recorder{objects: map[string]string{"/old/lessons/a.json": "{}"}}
		newServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
		oldServer := httptest.NewServer(legacy)
		store, err := player.LoadS3(env(map[string]string{"S3_ENDPOINT": newServer.URL, "S3_BUCKET": "new", "S3_REGION": "r", "S3_ACCESS_KEY_ID": "k", "S3_SECRET_ACCESS_KEY": "s", "S3_FALLBACK_ENABLED": "true",
			"S3_OLD_ENDPOINT": oldServer.URL, "S3_OLD_BUCKET": "old", "S3_OLD_REGION": "r", "S3_OLD_ACCESS_KEY_ID": "k2", "S3_OLD_SECRET_ACCESS_KEY": "s2"}))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.Get(t.Context(), "lessons/a.json"); err == nil {
			t.Errorf("status %d: Get read the old store instead of failing", status)
		}
		if ok, err := store.Exists(t.Context(), "lessons/a.json"); err == nil || ok {
			t.Errorf("status %d: Exists hid the new-store error: %v %v", status, ok, err)
		}
		if n := legacy.count(http.MethodGet) + legacy.count(http.MethodHead); n != 0 {
			t.Errorf("status %d: old store got %d reads", status, n)
		}
		newServer.Close()
		oldServer.Close()
	}
}
