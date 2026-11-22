package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const secretKey = "super-secret-key-material"

type logLine map[string]any

func newTestS3(t *testing.T, endpoint string) (*S3, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	store, err := NewS3(S3Options{Endpoint: endpoint, Region: "auto", Bucket: "backups", ApplicationBucket: "app", AccessKey: "AKIDEXAMPLE", SecretKey: secretKey, Logger: slog.New(slog.NewJSONHandler(&buf, nil))})
	if err != nil {
		t.Fatal(err)
	}
	return store, &buf
}

func lines(t *testing.T, buf *bytes.Buffer) []logLine {
	t.Helper()
	var out []logLine
	for _, raw := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if raw == "" {
			continue
		}
		var l logLine
		if err := json.Unmarshal([]byte(raw), &l); err != nil {
			t.Fatal(err, raw)
		}
		out = append(out, l)
	}
	return out
}

// An HTTPS endpoint whose certificate no trusted root signed (what the worker saw on staging before its
// image had a CA bundle) must be diagnosable from one error-level log line, with no secrets or learner
// keys in either the log line or the returned error, and the caller-visible sentinel still matching.
func TestReplayCharacterSuppressionsLogsTheTLSCauseOnceAndKeepsTheSentinel(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("request reached a server the client must not trust")
	}))
	defer server.Close()
	store, buf := newTestS3(t, server.URL)

	err := ReplayCharacterSuppressions(t.Context(), nil, store)
	if !errors.Is(err, errLedger) {
		t.Fatalf("sentinel lost: %v", err)
	}
	if !strings.Contains(err.Error(), "x509") {
		t.Fatalf("returned error does not name the error class: %v", err)
	}
	got := lines(t, buf)
	if len(got) != 1 {
		t.Fatalf("want exactly one log line, got %d: %s", len(got), buf.String())
	}
	l := got[0]
	if l["level"] != "ERROR" || l["op"] != "list" || l["bucket"] != "backups" || l["key_prefix"] != "character-suppressions/" || l["error_class"] != "x509" {
		t.Fatalf("unexpected log line: %v", l)
	}
	for _, text := range []string{buf.String(), err.Error()} {
		for _, leak := range []string{secretKey, "AKIDEXAMPLE", strings.TrimPrefix(server.URL, "https://")} {
			if strings.Contains(text, leak) {
				t.Fatalf("%q leaked into %q", leak, text)
			}
		}
	}
}

func TestStoreFailuresCarryHTTPStatusAndS3Code(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
		class  string
	}{{http.StatusForbidden, "AccessDenied", "http_403"}, {http.StatusNotImplemented, "NotImplemented", "http_501"}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>` + tc.code + `</Code><Message>no for learner@example.test</Message></Error>`))
		}))
		store, buf := newTestS3(t, server.URL)
		_, err := store.List(t.Context(), "moderation-blocks/v1/")
		server.Close()
		if err == nil || strings.Contains(err.Error(), "learner@example.test") {
			t.Fatalf("want a safe error, got %v", err)
		}
		got := lines(t, buf)
		if len(got) != 1 || got[0]["http_status"] != float64(tc.status) || got[0]["s3_code"] != tc.code || got[0]["error_class"] != tc.class || got[0]["op"] != "list" {
			t.Fatalf("status %d: log %s", tc.status, buf.String())
		}
	}
}

func TestStoreFailureLogsNeverIncludeFullKeys(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }))
	defer server.Close()
	store, buf := newTestS3(t, server.URL)
	ref := strings.Repeat("a", 64)
	if _, err := store.Exists(t.Context(), "account-deletions/v1/"+ref+".json"); err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(buf.String(), ref) {
		t.Fatalf("a learner reference reached the log: %s", buf.String())
	}
	if got := lines(t, buf); len(got) != 1 || got[0]["op"] != "head" || got[0]["key_prefix"] != "account-deletions/" {
		t.Fatalf("log %s", buf.String())
	}
}

func TestClassifyStoreError(t *testing.T) {
	ctx, cancel := context.WithDeadline(t.Context(), time.Unix(0, 0))
	defer cancel()
	if got := classify(ctx.Err()).Class; got != "timeout" {
		t.Fatalf("deadline: %s", got)
	}
	if got := classify(context.Canceled).Class; got != "canceled" {
		t.Fatalf("canceled: %s", got)
	}
	if got := classify(errors.New("boom")).Class; got != "unknown" {
		t.Fatalf("unknown: %s", got)
	}
}

func TestLoadParsesObjectVersioning(t *testing.T) {
	env := func(v string) func(string) string {
		return func(k string) string {
			return map[string]string{"BACKUP_S3_VERSIONING": v, "BACKUP_S3_ACCESS_KEY_ID": "w"}[k]
		}
	}
	for value, want := range map[string]bool{"": true, "enabled": true, "none": false} {
		cfg, err := Load(env(value))
		if err != nil || cfg.Versioned != want {
			t.Fatalf("%q: %v %v", value, cfg.Versioned, err)
		}
	}
	if _, err := Load(env("sometimes")); err == nil {
		t.Fatal("accepted an unknown BACKUP_S3_VERSIONING")
	}
}

func TestAssetVersionRequiresAVersionOnlyWhenTheBucketIsVersioned(t *testing.T) {
	if _, err := assetVersion("", true); err == nil {
		t.Fatal("a versioned bucket must return a version id")
	}
	if _, err := assetVersion("null", true); err == nil {
		t.Fatal("a null version in a versioned bucket is an error")
	}
	if v, err := assetVersion("abc", true); err != nil || v != "abc" {
		t.Fatal(v, err)
	}
	if v, err := assetVersion("", false); err != nil || v != UnversionedAsset {
		t.Fatal("an unversioned bucket records the explicit marker:", v, err)
	}
}
