//go:build integration

package backup_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/backup"
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/telemetry"
)

func repoRoot(t *testing.T) string {
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

func TestRailwayIaCEnvironmentFixtures(t *testing.T) {
	root := repoRoot(t)
	railwayTSPath := filepath.Join(root, ".railway", "railway.ts")
	content, err := os.ReadFile(filepath.Clean(railwayTSPath)) // #nosec G304
	if err != nil {
		t.Fatalf("could not read .railway/railway.ts: %v", err)
	}
	code := string(content)

	// Validate dev, staging, prod environment support in authoring file
	for _, env := range []string{"dev", "staging", "prod"} {
		if !strings.Contains(code, env) {
			t.Fatalf(".railway/railway.ts does not declare or support environment %q", env)
		}
	}

	// Validate that validator in backup package parses dev, staging, prod fixtures
	for _, env := range []string{"dev", "staging", "prod"} {
		fixture, err := backup.GetRailwayEnvironmentFixture(env)
		if err != nil {
			t.Fatalf("failed to get fixture for %s: %v", env, err)
		}
		result := backup.ValidateEnvironmentPlan(fixture)
		if !result.Valid {
			t.Fatalf("expected environment %s fixture to be valid, got violations: %v", env, result.Violations)
		}
		if result.LiveApplyAllowed {
			t.Fatalf("environment %s must have live apply disabled in AFK ticket", env)
		}
		if result.HITLGateRequired != "E11-07" {
			t.Fatalf("environment %s must require HITL gate E11-07", env)
		}
		// Verify engine has no public domains
		for _, svc := range fixture.Services {
			if svc.Name == "engine" && len(svc.Domains) != 0 {
				t.Fatalf("engine in %s has public domains: %v", env, svc.Domains)
			}
		}
	}
}

func TestDeployWorkflowPolicy(t *testing.T) {
	root := repoRoot(t)
	deployDevPath := filepath.Join(root, ".github", "workflows", "deploy-dev.yml")
	content, err := os.ReadFile(filepath.Clean(deployDevPath)) // #nosec G304
	if err != nil {
		t.Fatalf("could not read deploy-dev.yml: %v", err)
	}

	deployWorkflowText := string(content)

	// Verify runner is Namespace runner, NEVER ubuntu-latest
	if !strings.Contains(deployWorkflowText, "runs-on: namespace-profile-datamonster01") &&
		!strings.Contains(deployWorkflowText, "runs-on: namespace-profile-linux-xl") {
		t.Fatal("deploy job must run on a namespace-profile runner")
	}
	if strings.Contains(deployWorkflowText, "ubuntu-latest") {
		t.Fatal("deploy job must never use ubuntu-latest")
	}

	// Verify environment is dev only
	if !strings.Contains(deployWorkflowText, "environment: dev") {
		t.Fatal("deploy-dev.yml environment must be 'dev'")
	}

	// Policy check via backup validator
	policyErr := backup.ValidateDeployWorkflowPolicy(deployWorkflowText)
	if policyErr != nil {
		t.Fatalf("deploy workflow violates release policy: %v", policyErr)
	}

	// Verify that production or staging deploy workflow cannot apply live without E11-07
	prodPolicyErr := backup.ValidateDeployWorkflowPolicy(`
name: deploy-prod
jobs:
  deploy:
    runs-on: namespace-profile-datamonster01
    environment: prod
    steps:
      - run: railway config apply
`)
	if prodPolicyErr == nil {
		t.Fatal("un-gated prod apply must be rejected by deploy workflow policy")
	}
}

type mockEngineService struct{}

func (mockEngineService) Health(context.Context) error { return nil }
func (mockEngineService) PreviewCourse(context.Context, *enginev1.PreviewCourseRequest) (*enginev1.PreviewCourseResponse, error) {
	return nil, nil
}
func (mockEngineService) SetupChat(context.Context, *enginev1.SetupChatRequest) (*enginev1.SetupChatResponse, error) {
	return nil, nil
}

func TestMigrationReadinessRollbackHealthChecks(t *testing.T) {
	fixture := backuptest.New(t)

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	monitor := telemetry.New(logger)

	// Test healthy readiness probe
	healthyDeps := httpapi.Dependencies{
		Postgres:      fixture.DB.Pool.Ping,
		Redis:         func(context.Context) error { return nil },
		ObjectStorage: func(context.Context) error { return nil },
		Engine:        mockEngineService{},
		Monitor:       monitor,
	}

	server := httptest.NewServer(httpapi.New(healthyDeps, logger))
	defer server.Close()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/readyz", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected healthy /readyz to return 200, got %d", resp.StatusCode)
	}

	// Test failing database probe triggers 503 (readiness rollback gate)
	failingDBDeps := httpapi.Dependencies{
		Postgres: func(context.Context) error {
			return errors.New("database connection refused: migration or rollback required")
		},
		Redis:         func(context.Context) error { return nil },
		ObjectStorage: func(context.Context) error { return nil },
		Monitor:       monitor,
	}

	serverFail := httptest.NewServer(httpapi.New(failingDBDeps, logger))
	defer serverFail.Close()

	reqFail, err := http.NewRequestWithContext(t.Context(), http.MethodGet, serverFail.URL+"/readyz", nil)
	if err != nil {
		t.Fatal(err)
	}
	respFail, err := serverFail.Client().Do(reqFail)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = respFail.Body.Close() }()

	if respFail.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected failing /readyz to return 503 Service Unavailable, got %d", respFail.StatusCode)
	}

	// Validate migration preDeploy rollback behavior through backup package check
	rollbackGate := backup.NewRollbackGate(failingDBDeps.Postgres)
	if err := rollbackGate.VerifyReadiness(t.Context()); err == nil {
		t.Fatal("expected rollback gate to fail when dependency is unhealthy")
	}
}
