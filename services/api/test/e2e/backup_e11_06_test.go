package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

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

func TestE2EPlanEnvironmentChangesAndRejectViolations(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
	monitor := telemetry.New(logger)

	handler := backup.NewEnvironmentPlanHandler(logger, monitor)
	server := httptest.NewUnstartedServer(httpapi.Middleware(logger, handler, monitor))
	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", e11Port("E11_API_E2E_ADDR", "127.0.0.1:4116"))
	if err != nil {
		t.Fatalf("failed to listen on host port: %v", err)
	}
	_ = server.Listener.Close()
	server.Listener = listener
	server.Start()
	defer server.Close()

	client := server.Client()

	t.Run("valid dev environment plan succeeds and remains plan-only", func(t *testing.T) {
		validPlanReq := backup.EnvironmentPlanRequest{
			Environment: "dev",
			ProjectName: "duolify",
			Services: []backup.ServiceDefinition{
				{
					Name:        "api",
					Start:       "/api serve",
					PreDeploy:   "/api migrate",
					Healthcheck: "/readyz",
					Env: map[string]string{
						"DATABASE_URL": "${{postgres.DATABASE_URL}}",
						"REDIS_URL":    "${{redis.REDIS_URL}}",
						"ENGINE_HOST":  "${{engine.RAILWAY_PRIVATE_DOMAIN}}",
						"JWT_SECRET":   "preserve()",
					},
				},
				{
					Name:      "worker",
					Start:     "/api worker",
					PreDeploy: "/api migrate",
					Env: map[string]string{
						"DATABASE_URL": "${{postgres.DATABASE_URL}}",
						"REDIS_URL":    "${{redis.REDIS_URL}}",
					},
				},
				{
					Name:    "engine",
					Start:   "/duolify-engine",
					Domains: []string{}, // Must be private!
					Env: map[string]string{
						"ENGINE_ADDR_LISTEN": "[::]:50051",
						"ANTHROPIC_API_KEY":  "preserve()",
					},
				},
			},
			Resources: []backup.ResourceDefinition{
				{Name: "postgres", Type: "postgres"},
				{Name: "redis", Type: "redis"},
				{Name: "lesson-assets", Type: "bucket"},
				{Name: "backups", Type: "bucket"},
			},
		}

		payload, err := json.Marshal(validPlanReq)
		if err != nil {
			t.Fatal(err)
		}

		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/admin/v1/infra/plan", bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200 OK, got %d: %s", resp.StatusCode, string(body))
		}

		var planResp backup.EnvironmentPlanResponse
		if err := json.NewDecoder(resp.Body).Decode(&planResp); err != nil {
			t.Fatal(err)
		}

		if !planResp.Valid {
			t.Fatalf("expected plan to be valid: %+v", planResp)
		}
		if planResp.LiveApplyAllowed {
			t.Fatal("live apply must be disabled in AFK ticket")
		}
		if planResp.HITLGateRequired != "E11-07" {
			t.Fatalf("expected HITL gate E11-07, got %q", planResp.HITLGateRequired)
		}
	})

	t.Run("rejects public engine with public domain", func(t *testing.T) {
		publicEngineReq := backup.EnvironmentPlanRequest{
			Environment: "staging",
			ProjectName: "duolify",
			Services: []backup.ServiceDefinition{
				{
					Name:        "api",
					Start:       "/api serve",
					PreDeploy:   "/api migrate",
					Healthcheck: "/readyz",
					Env: map[string]string{
						"DATABASE_URL": "${{postgres.DATABASE_URL}}",
						"REDIS_URL":    "${{redis.REDIS_URL}}",
					},
				},
				{
					Name:    "engine",
					Start:   "/duolify-engine",
					Domains: []string{"engine-public.up.railway.app"}, // VIOLATION!
				},
			},
			Resources: []backup.ResourceDefinition{
				{Name: "postgres", Type: "postgres"},
				{Name: "redis", Type: "redis"},
				{Name: "lesson-assets", Type: "bucket"},
				{Name: "backups", Type: "bucket"},
			},
		}

		payload, _ := json.Marshal(publicEngineReq)
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/admin/v1/infra/plan", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for public engine, got %d", resp.StatusCode)
		}

		var planResp backup.EnvironmentPlanResponse
		_ = json.NewDecoder(resp.Body).Decode(&planResp)
		if planResp.Valid {
			t.Fatal("expected plan to be rejected")
		}
		foundViolation := false
		for _, v := range planResp.Violations {
			if strings.Contains(strings.ToLower(v), "engine") && strings.Contains(strings.ToLower(v), "public") {
				foundViolation = true
				break
			}
		}
		if !foundViolation {
			t.Fatalf("missing public engine violation in: %v", planResp.Violations)
		}
	})

	t.Run("rejects missing dependencies", func(t *testing.T) {
		missingDepsReq := backup.EnvironmentPlanRequest{
			Environment: "prod",
			ProjectName: "duolify",
			Services: []backup.ServiceDefinition{
				{
					Name:  "api",
					Start: "/api serve",
				},
			},
			Resources: []backup.ResourceDefinition{
				// Missing postgres, redis, S3 buckets!
			},
		}

		payload, _ := json.Marshal(missingDepsReq)
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/admin/v1/infra/plan", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for missing dependencies, got %d", resp.StatusCode)
		}

		var planResp backup.EnvironmentPlanResponse
		_ = json.NewDecoder(resp.Body).Decode(&planResp)
		if planResp.Valid {
			t.Fatal("expected plan to be rejected")
		}
		if len(planResp.Violations) == 0 {
			t.Fatal("expected dependency violations")
		}
	})

	t.Run("rejects plaintext secrets and redacts them from output", func(t *testing.T) {
		rawSecret := "super-secret-token-xyz-12345"
		secretInConfigReq := backup.EnvironmentPlanRequest{
			Environment: "prod",
			ProjectName: "duolify",
			Services: []backup.ServiceDefinition{
				{
					Name:        "api",
					Start:       "/api serve",
					PreDeploy:   "/api migrate",
					Healthcheck: "/readyz",
					Env: map[string]string{
						"DATABASE_URL": "${{postgres.DATABASE_URL}}",
						"REDIS_URL":    "${{redis.REDIS_URL}}",
						"JWT_SECRET":   rawSecret, // VIOLATION: plaintext secret!
					},
				},
				{
					Name:    "engine",
					Start:   "/duolify-engine",
					Domains: []string{},
				},
			},
			Resources: []backup.ResourceDefinition{
				{Name: "postgres", Type: "postgres"},
				{Name: "redis", Type: "redis"},
				{Name: "lesson-assets", Type: "bucket"},
				{Name: "backups", Type: "bucket"},
			},
		}

		payload, _ := json.Marshal(secretInConfigReq)
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/admin/v1/infra/plan", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for secret in config, got %d", resp.StatusCode)
		}

		bodyBytes, _ := io.ReadAll(resp.Body)
		bodyStr := string(bodyBytes)

		// Assert secret is NEVER leaked in output
		if strings.Contains(bodyStr, rawSecret) {
			t.Fatalf("raw secret was leaked in response body: %s", bodyStr)
		}

		// Assert logs also do not contain raw secret
		logs := logBuf.String()
		if strings.Contains(logs, rawSecret) {
			t.Fatalf("raw secret was leaked in server logs: %s", logs)
		}
	})
}
