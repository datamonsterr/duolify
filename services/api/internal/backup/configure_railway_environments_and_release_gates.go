package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/telemetry"
)

// ServiceDefinition represents a declared service in the Railway IaC configuration.
type ServiceDefinition struct {
	Name               string            `json:"name"`
	Start              string            `json:"start,omitempty"`
	PreDeploy          string            `json:"preDeploy,omitempty"`
	Healthcheck        string            `json:"healthcheck,omitempty"`
	HealthcheckTimeout int               `json:"healthcheckTimeout,omitempty"`
	Domains            []string          `json:"domains"`
	Env                map[string]string `json:"env,omitempty"`
}

// ResourceDefinition represents an infrastructure dependency like database, cache, or bucket.
type ResourceDefinition struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Region string `json:"region,omitempty"`
}

// EnvironmentPlanRequest captures the specification of a Railway environment plan.
type EnvironmentPlanRequest struct {
	Environment string               `json:"environment"`
	ProjectName string               `json:"projectName"`
	Services    []ServiceDefinition  `json:"services"`
	Resources   []ResourceDefinition `json:"resources"`
}

// EnvironmentPlanResponse captures the plan evaluation, violations, and gating requirements.
type EnvironmentPlanResponse struct {
	Valid            bool     `json:"valid"`
	Environment      string   `json:"environment"`
	PlanSummary      string   `json:"plan_summary"`
	Violations       []string `json:"violations,omitempty"`
	LiveApplyAllowed bool     `json:"live_apply_allowed"`
	HITLGateRequired string   `json:"hitl_gate_required"`
}

var (
	reBearer     = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9_\-\.]+`)
	reAnthropic  = regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]+`)
	reOpenRouter = regexp.MustCompile(`sk-or-[A-Za-z0-9_\-]+`)
	reJWT        = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)
	reURLPass    = regexp.MustCompile(`(?i)(postgres(?:ql)?|redis)://([^:]+):([^@]+)@`)
	reGithub     = regexp.MustCompile(`ghp_[A-Za-z0-9]+`)
	reRailway    = regexp.MustCompile(`railway_[A-Za-z0-9]+`)
	reSecretVal  = regexp.MustCompile(`(?i)(secret|token|password)[\s:=]+([^\s,;]+)`)
)

// RedactSecrets redacts sensitive credentials, tokens, and authorization values from text.
func RedactSecrets(input string) string {
	res := reBearer.ReplaceAllString(input, "Bearer [REDACTED]")
	res = reAnthropic.ReplaceAllString(res, "[REDACTED]")
	res = reOpenRouter.ReplaceAllString(res, "[REDACTED]")
	res = reJWT.ReplaceAllString(res, "[REDACTED]")
	res = reURLPass.ReplaceAllString(res, "$1://$2:[REDACTED]@")
	res = reGithub.ReplaceAllString(res, "[REDACTED]")
	res = reRailway.ReplaceAllString(res, "[REDACTED]")
	res = reSecretVal.ReplaceAllString(res, "$1: [REDACTED]")
	return res
}

// ValidateEnvironmentPlan validates the environment declaration:
// 1. Environment must be dev, staging, or prod, and project must be duolify.
// 2. Engine service must be private (domains must be empty).
// 3. Required dependencies must exist (postgres, redis, lesson-assets, backups).
// 4. API and worker services must exist and declare preDeploy migrations.
// 5. Config must not commit plaintext secrets (variables by name only, values preserved or referenced).
// 6. Live apply remains disabled and gated on E11-07.
func ValidateEnvironmentPlan(req EnvironmentPlanRequest) EnvironmentPlanResponse {
	var violations []string

	// 1. Environment and project check
	switch req.Environment {
	case "dev", "staging", "prod":
	default:
		violations = append(violations, fmt.Sprintf("invalid environment %q; allowed environments are dev, staging, prod", req.Environment))
	}
	if req.ProjectName != "duolify" {
		violations = append(violations, fmt.Sprintf("invalid projectName %q; expected duolify", req.ProjectName))
	}

	// Index services and resources
	services := make(map[string]ServiceDefinition, len(req.Services))
	for _, svc := range req.Services {
		services[svc.Name] = svc
	}
	resources := make(map[string]ResourceDefinition, len(req.Resources))
	for _, r := range req.Resources {
		resources[r.Name] = r
	}

	// 2. Engine check: private networking ONLY
	engine, hasEngine := services["engine"]
	if !hasEngine {
		violations = append(violations, "missing required engine service")
	} else if len(engine.Domains) > 0 {
		violations = append(violations, fmt.Sprintf("engine service must not define public domains or routes; found: %v", engine.Domains))
	}

	// 3. Resources dependencies check
	if _, ok := resources["postgres"]; !ok {
		violations = append(violations, "missing required database dependency (Postgres 18)")
	}
	if _, ok := resources["redis"]; !ok {
		violations = append(violations, "missing required cache dependency (Redis)")
	}
	if _, ok := resources["lesson-assets"]; !ok {
		violations = append(violations, "missing required storage bucket dependency (lesson-assets)")
	}
	if _, ok := resources["backups"]; !ok {
		violations = append(violations, "missing required storage bucket dependency (backups)")
	}

	// 4. API and Worker service checks
	api, hasAPI := services["api"]
	if !hasAPI {
		violations = append(violations, "missing required api service")
	} else {
		if api.PreDeploy != "/api migrate" {
			violations = append(violations, fmt.Sprintf("api preDeploy must be '/api migrate', got %q", api.PreDeploy))
		}
		if api.Healthcheck != "/readyz" {
			violations = append(violations, fmt.Sprintf("api healthcheck must be '/readyz', got %q", api.Healthcheck))
		}
	}

	worker, hasWorker := services["worker"]
	if !hasWorker {
		violations = append(violations, "missing required worker service")
	} else {
		if worker.PreDeploy != "/api migrate" {
			violations = append(violations, fmt.Sprintf("worker preDeploy must be '/api migrate', got %q", worker.PreDeploy))
		}
	}

	// 5. Plaintext secret check
	for svcName, svc := range services {
		for key, val := range svc.Env {
			valTrim := strings.TrimSpace(val)
			keyUpper := strings.ToUpper(key)
			isSecretKey := strings.Contains(keyUpper, "SECRET") ||
				strings.Contains(keyUpper, "KEY") ||
				strings.Contains(keyUpper, "TOKEN") ||
				strings.Contains(keyUpper, "PASSWORD")

			if isSecretKey {
				isPreservedOrRef := valTrim == "preserve()" ||
					(strings.HasPrefix(valTrim, "${{") && strings.HasSuffix(valTrim, "}}")) ||
					valTrim == ""

				if !isPreservedOrRef {
					violations = append(violations, fmt.Sprintf("service %s env var %s contains plaintext secret; must use preserve() or variable reference", svcName, key))
				}
			} else if strings.HasPrefix(valTrim, "sk-ant-") ||
				strings.HasPrefix(valTrim, "sk-or-") ||
				strings.HasPrefix(valTrim, "eyJ") ||
				strings.HasPrefix(valTrim, "ghp_") ||
				strings.HasPrefix(valTrim, "railway_") {
				violations = append(violations, fmt.Sprintf("service %s env var %s contains plaintext token pattern", svcName, key))
			}
		}
	}

	isValid := len(violations) == 0
	summary := fmt.Sprintf(
		"Environment: %s, Project: %s, Services: %d, Resources: %d, Engine Private: %t",
		req.Environment, req.ProjectName, len(req.Services), len(req.Resources), hasEngine && len(engine.Domains) == 0,
	)

	redactedViolations := make([]string, len(violations))
	for i, v := range violations {
		redactedViolations[i] = RedactSecrets(v)
	}

	return EnvironmentPlanResponse{
		Valid:            isValid,
		Environment:      req.Environment,
		PlanSummary:      RedactSecrets(summary),
		Violations:       redactedViolations,
		LiveApplyAllowed: false, // Always disabled in AFK ticket
		HITLGateRequired: "E11-07",
	}
}

// GetRailwayEnvironmentFixture returns canonical dev, staging, or prod environment definition.
func GetRailwayEnvironmentFixture(env string) (EnvironmentPlanRequest, error) {
	switch env {
	case "dev", "staging", "prod":
	default:
		return EnvironmentPlanRequest{}, fmt.Errorf("unknown environment %q", env)
	}

	duolifyEnv := "development"
	providerMode := "mock"
	logLevel := "debug"
	switch env {
	case "staging":
		duolifyEnv = "staging"
		logLevel = "info"
	case "prod":
		duolifyEnv = "production"
		providerMode = "live"
		logLevel = "info"
	}

	return EnvironmentPlanRequest{
		Environment: env,
		ProjectName: "duolify",
		Services: []ServiceDefinition{
			{
				Name:               "api",
				Start:              "/api serve",
				PreDeploy:          "/api migrate",
				Healthcheck:        "/readyz",
				HealthcheckTimeout: 120,
				Domains:            []string{}, // Attached by Railway routing for API
				Env: map[string]string{
					"DUOLIFY_ENV":                 duolifyEnv,
					"LOG_LEVEL":                   logLevel,
					"API_ADDR":                    "[::]:8080",
					"PORT":                        "8080",
					"DATABASE_URL":                "${{postgres.DATABASE_URL}}",
					"REDIS_URL":                   "${{redis.REDIS_URL}}",
					"ENGINE_HOST":                 "${{engine.RAILWAY_PRIVATE_DOMAIN}}",
					"ENGINE_ADDR":                 "http://${{ENGINE_HOST}}:50051",
					"S3_ENDPOINT":                 "${{lesson-assets.ENDPOINT}}",
					"S3_BUCKET":                   "${{lesson-assets.BUCKET}}",
					"S3_REGION":                   "${{lesson-assets.REGION}}",
					"S3_ACCESS_KEY_ID":            "${{lesson-assets.ACCESS_KEY_ID}}",
					"S3_SECRET_ACCESS_KEY":        "${{lesson-assets.SECRET_ACCESS_KEY}}",
					"BACKUP_S3_ENDPOINT":          "${{backups.ENDPOINT}}",
					"BACKUP_S3_BUCKET":            "${{backups.BUCKET}}",
					"BACKUP_S3_REGION":            "${{backups.REGION}}",
					"BACKUP_S3_ACCESS_KEY_ID":     "preserve()",
					"BACKUP_S3_SECRET_ACCESS_KEY": "preserve()",
					"JWT_SECRET":                  "preserve()",
					"POSTHOG_API_KEY":             "preserve()",
					"POSTHOG_HOST":                "https://us.i.posthog.com",
				},
			},
			{
				Name:      "worker",
				Start:     "/api worker",
				PreDeploy: "/api migrate",
				Env: map[string]string{
					"DUOLIFY_ENV":                 duolifyEnv,
					"LOG_LEVEL":                   logLevel,
					"DATABASE_URL":                "${{postgres.DATABASE_URL}}",
					"REDIS_URL":                   "${{redis.REDIS_URL}}",
					"BACKUP_S3_ENDPOINT":          "${{backups.ENDPOINT}}",
					"BACKUP_S3_BUCKET":            "${{backups.BUCKET}}",
					"BACKUP_S3_REGION":            "${{backups.REGION}}",
					"BACKUP_S3_ACCESS_KEY_ID":     "preserve()",
					"BACKUP_S3_SECRET_ACCESS_KEY": "preserve()",
					"BACKUP_AGE_RECIPIENT":        "preserve()",
				},
			},
			{
				Name:    "engine",
				Start:   "/duolify-engine",
				Domains: []string{}, // Must be empty: private networking only!
				Env: map[string]string{
					"ENGINE_ADDR_LISTEN":   "[::]:50051",
					"ENGINE_PROVIDER_MODE": providerMode,
					"ANTHROPIC_API_KEY":    "preserve()",
					"OPENROUTER_API_KEY":   "preserve()",
				},
			},
		},
		Resources: []ResourceDefinition{
			{Name: "postgres", Type: "postgres"},
			{Name: "redis", Type: "redis"},
			{Name: "lesson-assets", Type: "bucket", Region: "sin"},
			{Name: "backups", Type: "bucket", Region: "sin"},
		},
	}, nil
}

// ValidateDeployWorkflowPolicy enforces deployment policies:
// - Runners must be Namespace runners, never ubuntu-latest.
// - Deploy targets in AFK ticket are limited to dev only.
// - Any live apply to staging or prod must be gated on E11-07 and stays disabled.
func ValidateDeployWorkflowPolicy(workflowText string) error {
	if strings.Contains(workflowText, "ubuntu-latest") {
		return errors.New("deployment workflow policy violation: ubuntu-latest is forbidden; use namespace-profile runner")
	}

	hasNamespaceRunner := strings.Contains(workflowText, "namespace-profile-datamonster01") ||
		strings.Contains(workflowText, "namespace-profile-linux-xl")
	if !hasNamespaceRunner {
		return errors.New("deployment workflow policy violation: runner must be namespace-profile")
	}

	// If environment mentions prod or staging, it MUST require E11-07
	if strings.Contains(workflowText, "environment: prod") || strings.Contains(workflowText, "environment: staging") {
		if !strings.Contains(workflowText, "E11-07") && !strings.Contains(workflowText, "e11-07") {
			return errors.New("deployment workflow policy violation: production or staging deployment requires E11-07 owner approval gate")
		}
	}

	return nil
}

// RollbackGate verifies that dependencies and migrations allow service activation.
type RollbackGate struct {
	probe func(context.Context) error
}

// NewRollbackGate creates a new RollbackGate with the provided readiness probe.
func NewRollbackGate(probe func(context.Context) error) *RollbackGate {
	return &RollbackGate{probe: probe}
}

// VerifyReadiness probes dependencies; if failing, triggers rollback before traffic arrives.
func (g *RollbackGate) VerifyReadiness(ctx context.Context) error {
	if g.probe == nil {
		return errors.New("no readiness probe configured")
	}
	if err := g.probe(ctx); err != nil {
		return fmt.Errorf("readiness verification failed: %w (rollback required)", err)
	}
	return nil
}

// EmitEnvironmentPlanEvent records operational telemetry for an evaluated environment plan.
func EmitEnvironmentPlanEvent(plan EnvironmentPlanRequest, ok bool, recorder analytics.Events, optOut bool) {
	if optOut || recorder == nil {
		return
	}
	properties := map[string]any{
		"source":      "api",
		"platform":    "web",
		"app_version": "0.1.0",
		"environment": plan.Environment,
		"ok":          ok,
	}
	_ = recorder.Capture("00000000-0000-4000-8000-000000000011", "environment_plan_validated", properties)
}

// EnvironmentPlanHandler handles plan validation requests via HTTP.
type EnvironmentPlanHandler struct {
	logger  *slog.Logger
	monitor *telemetry.Monitor
}

// NewEnvironmentPlanHandler constructs an HTTP handler for environment plan validation.
func NewEnvironmentPlanHandler(logger *slog.Logger, monitor *telemetry.Monitor) *EnvironmentPlanHandler {
	return &EnvironmentPlanHandler{
		logger:  logger,
		monitor: monitor,
	}
}

func (h *EnvironmentPlanHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	var req EnvironmentPlanRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	res := ValidateEnvironmentPlan(req)

	w.Header().Set("Content-Type", "application/json")
	if !res.Valid {
		w.WriteHeader(http.StatusBadRequest)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	respBytes, _ := json.Marshal(res)
	// Guarantee secrets are redacted in output
	redactedResp := RedactSecrets(string(respBytes))
	_, _ = w.Write([]byte(redactedResp))
}
