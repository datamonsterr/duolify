package s3conformance

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	clientCall = regexp.MustCompile(`\.Client\.([A-Z]\w+)\(`)
	paginator  = regexp.MustCompile(`s3\.New(\w+)Paginator\(`)
)

// observedS3Calls scans non-test product code that talks to S3.
func observedS3Calls(t *testing.T) map[string][]string {
	t.Helper()
	found := map[string][]string{}
	for _, dir := range []string{"backup", "backupadmin", "player", "characters", "telemetry"} {
		files, err := filepath.Glob(filepath.Join("..", dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range clientCall.FindAllStringSubmatch(string(raw), -1) {
				found[m[1]] = append(found[m[1]], file)
			}
			for _, m := range paginator.FindAllStringSubmatch(string(raw), -1) {
				found[m[1]] = append(found[m[1]], file)
			}
		}
	}
	return found
}

// A newly used S3 call without conformance coverage (or a written exemption) fails here.
func TestEveryProductS3CallHasConformanceCoverage(t *testing.T) {
	covered := map[string]bool{}
	for _, op := range Operations() {
		for _, call := range op.Calls {
			covered[call] = true
		}
	}
	observed := observedS3Calls(t)
	if len(observed) < 5 {
		t.Fatalf("scan found too few S3 calls (%v); the scanner is broken", observed)
	}
	var missing []string
	for call, files := range observed {
		if _, exempt := ExemptCalls[call]; !covered[call] && !exempt {
			missing = append(missing, call+" in "+strings.Join(files, ","))
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("S3 calls without conformance coverage: %v", missing)
	}
	for call, reason := range ExemptCalls {
		if reason == "" || covered[call] {
			t.Errorf("exemption %s needs a reason and must not also be covered", call)
		}
	}
}

func TestOperationTableNamesTheRequiredOperations(t *testing.T) {
	have := map[string]bool{}
	for _, op := range Operations() {
		have[op.Name] = true
		if op.Run == nil || len(op.Calls) == 0 {
			t.Errorf("operation %q has no run function or calls", op.Name)
		}
	}
	for _, name := range []string{"bucket versioning", "versioned put", "list versions", "get by version", "delete-marker restore", "conditional create (If-None-Match: *)", "retention lock"} {
		if !have[name] {
			t.Errorf("missing operation %q", name)
		}
	}
}

func TestRedactorHidesCredentialsEverywhere(t *testing.T) {
	cfg := Config{AccessKey: "AKIAEXAMPLEKEY", SecretKey: "s3cr3t/value+x"}
	out := cfg.Redact("denied for AKIAEXAMPLEKEY with s3cr3t/value+x; again s3cr3t/value+x")
	if strings.Contains(out, cfg.AccessKey) || strings.Contains(out, cfg.SecretKey) || !strings.Contains(out, "[redacted]") {
		t.Fatalf("credentials leaked: %q", out)
	}
	if (Config{}).Redact("plain") != "plain" {
		t.Fatal("empty credentials must not rewrite output")
	}
}

func TestConfigFromEnvRequiresEndpointAndCredentials(t *testing.T) {
	env := map[string]string{"S3_CONFORMANCE_ENDPOINT": "https://hel1.example.com", "S3_CONFORMANCE_ACCESS_KEY_ID": "a", "S3_CONFORMANCE_SECRET_ACCESS_KEY": "b"}
	cfg, err := ConfigFromEnv(func(k string) string { return env[k] })
	if err != nil || cfg.Region != "us-east-1" || !cfg.PathStyle || cfg.Bucket != "" {
		t.Fatalf("defaults: %+v %v", cfg, err)
	}
	if _, err := ConfigFromEnv(func(string) string { return "" }); err == nil {
		t.Fatal("expected an error without endpoint and credentials")
	}
}
