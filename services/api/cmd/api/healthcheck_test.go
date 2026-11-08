package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestHealthcheck(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusServiceUnavailable, http.StatusFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/healthz" {
					t.Errorf("path = %s", r.URL.Path)
				}
				w.WriteHeader(status)
			}))
			defer server.Close()
			u, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("API_ADDR", u.Host)
			err = run(context.Background(), []string{"healthcheck"})
			if (err == nil) != (status == http.StatusOK) {
				t.Fatalf("status = %d, err = %v", status, err)
			}
		})
	}
}

func TestHealthcheckRefused(t *testing.T) {
	t.Setenv("API_ADDR", "127.0.0.1:0")
	if err := run(context.Background(), []string{"healthcheck"}); err == nil {
		t.Fatal("unreachable listener reported healthy")
	}
}

func TestHealthcheckRejectsArguments(t *testing.T) {
	if err := run(context.Background(), []string{"healthcheck", "unexpected"}); err == nil {
		t.Fatal("unexpected arguments accepted")
	}
}

func TestHealthcheckDatabaseRequiresURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if err := run(context.Background(), []string{"healthcheck", "--database"}); err == nil {
		t.Fatal("missing database URL accepted")
	}
}
