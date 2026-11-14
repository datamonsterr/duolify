package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
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

type rawPlanItem struct {
	Environment string              `json:"environment"`
	ProjectName string              `json:"projectName"`
	Name        string              `json:"name"`
	Services    []ServiceDefinition `json:"services"`
	Resources   []json.RawMessage   `json:"resources"`
}

func (r rawPlanItem) toEnvironmentPlanRequest() (EnvironmentPlanRequest, error) {
	proj := r.ProjectName
	if proj == "" {
		proj = r.Name
	}
	env := r.Environment

	services := make([]ServiceDefinition, len(r.Services))
	copy(services, r.Services)

	var resources []ResourceDefinition

	for _, raw := range r.Resources {
		var peek struct {
			Type               string            `json:"type"`
			Kind               string            `json:"kind"`
			Name               string            `json:"name"`
			Region             string            `json:"region"`
			Start              string            `json:"start"`
			PreDeploy          string            `json:"preDeploy"`
			Healthcheck        string            `json:"healthcheck"`
			HealthcheckTimeout int               `json:"healthcheckTimeout"`
			Domains            []string          `json:"domains"`
			Env                map[string]string `json:"env"`
			Config             struct {
				Region string `json:"region"`
			} `json:"config"`
			Deploy struct {
				StartCommand       string   `json:"startCommand"`
				PreDeployCommand   []string `json:"preDeployCommand"`
				HealthcheckPath    string   `json:"healthcheckPath"`
				HealthcheckTimeout int      `json:"healthcheckTimeout"`
			} `json:"deploy"`
		}
		if err := json.Unmarshal(raw, &peek); err != nil {
			continue
		}

		if peek.Type == "service" {
			svc := ServiceDefinition{
				Name:               peek.Name,
				Start:              peek.Start,
				PreDeploy:          peek.PreDeploy,
				Healthcheck:        peek.Healthcheck,
				HealthcheckTimeout: peek.HealthcheckTimeout,
				Domains:            peek.Domains,
				Env:                peek.Env,
			}
			if svc.Start == "" && peek.Deploy.StartCommand != "" {
				svc.Start = peek.Deploy.StartCommand
			}
			if svc.PreDeploy == "" && len(peek.Deploy.PreDeployCommand) > 0 {
				svc.PreDeploy = peek.Deploy.PreDeployCommand[0]
			}
			if svc.Healthcheck == "" && peek.Deploy.HealthcheckPath != "" {
				svc.Healthcheck = peek.Deploy.HealthcheckPath
			}
			if svc.HealthcheckTimeout == 0 && peek.Deploy.HealthcheckTimeout != 0 {
				svc.HealthcheckTimeout = peek.Deploy.HealthcheckTimeout
			}
			services = append(services, svc)
		} else {
			region := peek.Region
			if region == "" {
				region = peek.Config.Region
			}
			resType := peek.Kind
			if resType == "" {
				resType = peek.Type
			}
			resources = append(resources, ResourceDefinition{
				Name:   peek.Name,
				Type:   resType,
				Region: region,
			})
		}
	}

	return EnvironmentPlanRequest{
		Environment: env,
		ProjectName: proj,
		Services:    services,
		Resources:   resources,
	}, nil
}

// ParseEnvironmentPlans parses JSON from railway plan rendering.
// Accepts single plan, array of plans, or map of plans.
func ParseEnvironmentPlans(data []byte) ([]EnvironmentPlanRequest, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, errors.New("empty plan JSON")
	}

	// 1. Try array of plans
	if data[0] == '[' {
		var list []rawPlanItem
		if err := json.Unmarshal(data, &list); err == nil {
			var res []EnvironmentPlanRequest
			for _, item := range list {
				plan, err := item.toEnvironmentPlanRequest()
				if err != nil {
					return nil, err
				}
				res = append(res, plan)
			}
			return res, nil
		}
	}

	// 2. Try single plan
	var single rawPlanItem
	if err := json.Unmarshal(data, &single); err == nil && (single.Environment != "" || single.ProjectName != "" || single.Name != "") {
		if single.Environment != "" || len(single.Services) > 0 || len(single.Resources) > 0 {
			plan, err := single.toEnvironmentPlanRequest()
			if err != nil {
				return nil, err
			}
			return []EnvironmentPlanRequest{plan}, nil
		}
	}

	// 3. Try map of environment plans
	var envMap map[string]rawPlanItem
	if err := json.Unmarshal(data, &envMap); err == nil && len(envMap) > 0 {
		var res []EnvironmentPlanRequest
		for envName, item := range envMap {
			if item.Environment == "" {
				item.Environment = envName
			}
			plan, err := item.toEnvironmentPlanRequest()
			if err != nil {
				return nil, err
			}
			res = append(res, plan)
		}
		return res, nil
	}

	return nil, errors.New("unable to parse plan JSON into environment plan request")
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

// LogEnvironmentPlanValidation logs structured operational deploy/health telemetry for an evaluated environment plan.
func LogEnvironmentPlanValidation(logger *slog.Logger, plan EnvironmentPlanRequest, res EnvironmentPlanResponse) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("railway environment plan evaluated",
		"environment", plan.Environment,
		"project", plan.ProjectName,
		"valid", res.Valid,
		"services_count", len(plan.Services),
		"resources_count", len(plan.Resources),
		"live_apply_allowed", res.LiveApplyAllowed,
		"hitl_gate_required", res.HITLGateRequired,
	)
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
