package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/backup"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/telemetry"
)

func e11Port(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func repoRootDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root")
		}
		dir = parent
	}
}

type mockEngine struct{}

func (mockEngine) Health(context.Context) error { return nil }
func (mockEngine) PreviewCourse(context.Context, *enginev1.PreviewCourseRequest) (*enginev1.PreviewCourseResponse, error) {
	return nil, nil
}
func (mockEngine) SetupChat(context.Context, *enginev1.SetupChatRequest) (*enginev1.SetupChatResponse, error) {
	return nil, nil
}

func TestE2EPlanEnvironmentChangesAndRejectViolations(t *testing.T) {
	root := repoRootDir(t)
	apiDir := filepath.Join(root, "services", "api")
	planScript := filepath.Join(root, ".railway", "plan.mjs")

	// Build the real api binary to test CLI end to end
	apiBin := filepath.Join(t.TempDir(), "api-cli")
	buildCmd := exec.CommandContext(t.Context(), "go", "build", "-o", apiBin, "./cmd/api") // #nosec G204
	buildCmd.Dir = apiDir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build api CLI binary: %v\noutput: %s", err, string(out))
	}

	// 1. Render real railway.ts end to end and validate with real built binary
	t.Run("render real railway.ts and validate via built CLI", func(t *testing.T) {
		renderCmd := exec.CommandContext(t.Context(), "node", planScript, "--all") // #nosec G204
		renderOut, err := renderCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("failed to render railway.ts via %s: %v\noutput: %s", planScript, err, string(renderOut))
		}

		validateCmd := exec.CommandContext(t.Context(), apiBin, "admin", "infra", "validate") // #nosec G204
		validateCmd.Stdin = bytes.NewReader(renderOut)
		valOut, err := validateCmd.CombinedOutput()
		outStr := string(valOut)
		if err != nil {
			t.Fatalf("expected real railway.ts plan to pass validation, failed: %v\noutput: %s", err, outStr)
		}

		for _, env := range []string{"dev", "staging", "prod"} {
			expectedSummary := fmt.Sprintf("Environment %s plan valid:", env)
			if !strings.Contains(outStr, expectedSummary) {
				t.Fatalf("missing success message for %s in: %s", env, outStr)
			}
		}
		if !strings.Contains(outStr, "live_apply_allowed=false") {
			t.Fatalf("expected live_apply_allowed=false in: %s", outStr)
		}
		if !strings.Contains(outStr, "hitl_gate_required=E11-07") {
			t.Fatalf("expected hitl_gate_required=E11-07 in: %s", outStr)
		}
	})

	// 2. Mutated plan: public engine domain is rejected
	t.Run("rejects mutated plan with public engine domain via built CLI", func(t *testing.T) {
		renderCmd := exec.CommandContext(t.Context(), "node", planScript, "--environment", "dev") // #nosec G204
		renderOut, err := renderCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("failed to render plan: %v", err)
		}

		plans, err := backup.ParseEnvironmentPlans(renderOut)
		if err != nil {
			t.Fatal(err)
		}
		// Mutate: add public domain to engine
		for i, svc := range plans[0].Services {
			if svc.Name == "engine" {
				plans[0].Services[i].Domains = []string{"engine.public.railway.app"}
			}
		}

		mutatedJSON, err := json.Marshal(plans[0])
		if err != nil {
			t.Fatal(err)
		}

		validateCmd := exec.CommandContext(t.Context(), apiBin, "admin", "infra", "validate") // #nosec G204
		validateCmd.Stdin = bytes.NewReader(mutatedJSON)
		valOut, err := validateCmd.CombinedOutput()
		if err == nil {
			t.Fatal("expected validation to fail for public engine domain")
		}
		if !strings.Contains(string(valOut), "engine service must not define public domains") {
			t.Fatalf("expected public domain violation in output: %s", string(valOut))
		}
	})

	// 3. Mutated plan: dropped dependency is rejected
	t.Run("rejects mutated plan with dropped dependency via built CLI", func(t *testing.T) {
		renderCmd := exec.CommandContext(t.Context(), "node", planScript, "--environment", "prod") // #nosec G204
		renderOut, err := renderCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("failed to render plan: %v", err)
		}

		plans, err := backup.ParseEnvironmentPlans(renderOut)
		if err != nil {
			t.Fatal(err)
		}
		// Mutate: drop backups bucket
		var kept []backup.ResourceDefinition
		for _, r := range plans[0].Resources {
			if r.Name != "backups" {
				kept = append(kept, r)
			}
		}
		plans[0].Resources = kept

		mutatedJSON, err := json.Marshal(plans[0])
		if err != nil {
			t.Fatal(err)
		}

		validateCmd := exec.CommandContext(t.Context(), apiBin, "admin", "infra", "validate") // #nosec G204
		validateCmd.Stdin = bytes.NewReader(mutatedJSON)
		valOut, err := validateCmd.CombinedOutput()
		if err == nil {
			t.Fatal("expected validation to fail when backups dependency is dropped")
		}
		if !strings.Contains(string(valOut), "backups") {
			t.Fatalf("expected backups violation in output: %s", string(valOut))
		}
	})

	// 4. Mutated plan: inlined plaintext secret is rejected and redacted
	t.Run("rejects mutated plan with secret literal and redacts in output", func(t *testing.T) {
		renderCmd := exec.CommandContext(t.Context(), "node", planScript, "--environment", "staging") // #nosec G204
		renderOut, err := renderCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("failed to render plan: %v", err)
		}

		plans, err := backup.ParseEnvironmentPlans(renderOut)
		if err != nil {
			t.Fatal(err)
		}
		plaintextSecret := "sk-ant-leak-token-must-be-redacted" // #nosec G101
		plans[0].Services[0].Env["JWT_SECRET"] = plaintextSecret

		mutatedJSON, err := json.Marshal(plans[0])
		if err != nil {
			t.Fatal(err)
		}

		validateCmd := exec.CommandContext(t.Context(), apiBin, "admin", "infra", "validate") // #nosec G204
		validateCmd.Stdin = bytes.NewReader(mutatedJSON)
		valOut, err := validateCmd.CombinedOutput()
		if err == nil {
			t.Fatal("expected validation to fail when plaintext secret is inlined")
		}
		outStr := string(valOut)
		if strings.Contains(outStr, plaintextSecret) {
			t.Fatalf("plaintext secret was not redacted in output: %s", outStr)
		}
		if !strings.Contains(outStr, "[REDACTED]") && !strings.Contains(outStr, "plaintext secret") {
			t.Fatalf("expected violation explanation in output: %s", outStr)
		}
	})
}

func TestE2EReadinessAndRollbackGateHTTP(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	monitor := telemetry.New(logger)

	isHealthy := true
	deps := httpapi.Dependencies{
		Postgres: func(context.Context) error {
			if !isHealthy {
				return errors.New("database unreachable: trigger rollback")
			}
			return nil
		},
		Redis:         func(context.Context) error { return nil },
		ObjectStorage: func(context.Context) error { return nil },
		Engine:        mockEngine{},
		Monitor:       monitor,
	}

	handler := httpapi.New(deps, logger)
	server := httptest.NewUnstartedServer(handler)
	bindAddr := e11Port("E11_API_E2E_ADDR", "127.0.0.1:4116")
	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", bindAddr)
	if err != nil {
		t.Fatalf("failed to bind on host port %s: %v", bindAddr, err)
	}
	_ = server.Listener.Close()
	server.Listener = listener
	server.Start()
	defer server.Close()

	client := server.Client()

	// 1. Healthy dependencies -> /readyz returns 200 OK -> release gate permits traffic
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/readyz", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK when dependencies are healthy, got %d", resp.StatusCode)
	}

	// 2. Unhealthy dependencies -> /readyz returns 503 -> rollback gate triggers
	isHealthy = false
	reqFail, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/readyz", nil)
	if err != nil {
		t.Fatal(err)
	}
	respFail, err := client.Do(reqFail)
	if err != nil {
		t.Fatal(err)
	}
	_ = respFail.Body.Close()
	if respFail.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable when dependency fails, got %d", respFail.StatusCode)
	}
}
