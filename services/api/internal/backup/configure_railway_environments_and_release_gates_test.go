package backup_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/backup"
)

func validBasePlan() backup.EnvironmentPlanRequest {
	return backup.EnvironmentPlanRequest{
		Environment: "dev",
		ProjectName: "duolify",
		Services: []backup.ServiceDefinition{
			{
				Name:        "api",
				Start:       "/api serve",
				PreDeploy:   "/api migrate",
				Healthcheck: "/readyz",
				Env: map[string]string{
					"DATABASE_URL":            "${{postgres.DATABASE_URL}}",
					"REDIS_URL":               "${{redis.REDIS_URL}}",
					"ENGINE_HOST":             "${{engine.RAILWAY_PRIVATE_DOMAIN}}",
					"S3_ENDPOINT":             "${{lesson-assets.ENDPOINT}}",
					"BACKUP_S3_ENDPOINT":      "${{backups.ENDPOINT}}",
					"BACKUP_S3_ACCESS_KEY_ID": "preserve()",
					"JWT_SECRET":              "preserve()",
					"POSTHOG_API_KEY":         "preserve()",
				},
			},
			{
				Name:      "worker",
				Start:     "/api worker",
				PreDeploy: "/api migrate",
				Env: map[string]string{
					"DATABASE_URL":            "${{postgres.DATABASE_URL}}",
					"REDIS_URL":               "${{redis.REDIS_URL}}",
					"BACKUP_S3_ACCESS_KEY_ID": "preserve()",
					"BACKUP_AGE_RECIPIENT":    "preserve()",
				},
			},
			{
				Name:    "engine",
				Start:   "/duolify-engine",
				Domains: []string{}, // Must be strictly private
				Env: map[string]string{
					"ENGINE_ADDR_LISTEN": "[::]:50051",
					"ANTHROPIC_API_KEY":  "preserve()",
					"OPENROUTER_API_KEY": "preserve()",
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
}

func TestValidateEnvironmentPlanBoundaryCases(t *testing.T) {
	t.Run("valid environments dev staging prod succeed", func(t *testing.T) {
		for _, env := range []string{"dev", "staging", "prod"} {
			plan := validBasePlan()
			plan.Environment = env
			res := backup.ValidateEnvironmentPlan(plan)
			if !res.Valid {
				t.Fatalf("expected environment %q to be valid, got: %v", env, res.Violations)
			}
			if res.LiveApplyAllowed {
				t.Fatal("live apply must be false")
			}
			if res.HITLGateRequired != "E11-07" {
				t.Fatalf("expected E11-07 gate, got %q", res.HITLGateRequired)
			}
		}
	})

	t.Run("rejects unsupported environment or project", func(t *testing.T) {
		plan := validBasePlan()
		plan.Environment = "unknown-env"
		res := backup.ValidateEnvironmentPlan(plan)
		if res.Valid {
			t.Fatal("expected invalid environment to fail")
		}

		plan2 := validBasePlan()
		plan2.ProjectName = "wrong-project"
		res2 := backup.ValidateEnvironmentPlan(plan2)
		if res2.Valid {
			t.Fatal("expected wrong project to fail")
		}
	})

	t.Run("rejects public engine domain", func(t *testing.T) {
		plan := validBasePlan()
		for i, svc := range plan.Services {
			if svc.Name == "engine" {
				plan.Services[i].Domains = []string{"public.engine.railway.app"}
			}
		}
		res := backup.ValidateEnvironmentPlan(plan)
		if res.Valid {
			t.Fatal("expected public engine to be rejected")
		}
		found := false
		for _, v := range res.Violations {
			if strings.Contains(strings.ToLower(v), "engine") && strings.Contains(strings.ToLower(v), "public") {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected public engine violation, got: %v", res.Violations)
		}
	})

	t.Run("rejects missing dependencies", func(t *testing.T) {
		// Missing postgres
		planNoDB := validBasePlan()
		planNoDB.Resources = []backup.ResourceDefinition{
			{Name: "redis", Type: "redis"},
			{Name: "lesson-assets", Type: "bucket"},
			{Name: "backups", Type: "bucket"},
		}
		res := backup.ValidateEnvironmentPlan(planNoDB)
		if res.Valid {
			t.Fatal("expected missing database to fail")
		}

		// Missing redis
		planNoRedis := validBasePlan()
		planNoRedis.Resources = []backup.ResourceDefinition{
			{Name: "postgres", Type: "postgres"},
			{Name: "lesson-assets", Type: "bucket"},
			{Name: "backups", Type: "bucket"},
		}
		resRedis := backup.ValidateEnvironmentPlan(planNoRedis)
		if resRedis.Valid {
			t.Fatal("expected missing redis to fail")
		}

		// Missing buckets
		planNoBuckets := validBasePlan()
		planNoBuckets.Resources = []backup.ResourceDefinition{
			{Name: "postgres", Type: "postgres"},
			{Name: "redis", Type: "redis"},
		}
		resBuckets := backup.ValidateEnvironmentPlan(planNoBuckets)
		if resBuckets.Valid {
			t.Fatal("expected missing storage buckets to fail")
		}

		// Missing engine service
		planNoEngine := validBasePlan()
		planNoEngine.Services = []backup.ServiceDefinition{
			validBasePlan().Services[0],
			validBasePlan().Services[1],
		}
		resEngine := backup.ValidateEnvironmentPlan(planNoEngine)
		if resEngine.Valid {
			t.Fatal("expected missing engine service to fail")
		}
	})

	t.Run("rejects plaintext secrets and redacts them in violations", func(t *testing.T) {
		sensitiveSecret := "sk-ant-live-token-secret-999" // #nosec G101
		plan := validBasePlan()
		plan.Services[0].Env["JWT_SECRET"] = sensitiveSecret

		res := backup.ValidateEnvironmentPlan(plan)
		if res.Valid {
			t.Fatal("expected plaintext secret in config to fail")
		}

		// Verify secret is NOT in violation messages
		for _, v := range res.Violations {
			if strings.Contains(v, sensitiveSecret) {
				t.Fatalf("secret was not redacted in violation message: %s", v)
			}
		}

		// Check plan summary also redacts secrets
		if strings.Contains(res.PlanSummary, sensitiveSecret) {
			t.Fatalf("secret was not redacted in plan summary: %s", res.PlanSummary)
		}
	})
}

func TestRedactSecretsHelper(t *testing.T) {
	input := "Error with token sk-ant-live-1234567890abcdef and JWT eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.e30.t-IDN secretly postgres://user:superpass123@host:5432/db"
	redacted := backup.RedactSecrets(input)

	for _, secret := range []string{"sk-ant-live-1234567890abcdef", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9", "superpass123"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("expected secret %q to be redacted from: %s", secret, redacted)
		}
	}
	if !strings.Contains(redacted, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] placeholder in: %s", redacted)
	}
}

func TestParseEnvironmentPlans(t *testing.T) {
	t.Run("parses single plan object", func(t *testing.T) {
		single := validBasePlan()
		data, err := json.Marshal(single)
		if err != nil {
			t.Fatal(err)
		}
		plans, err := backup.ParseEnvironmentPlans(data)
		if err != nil {
			t.Fatalf("failed to parse single plan: %v", err)
		}
		if len(plans) != 1 || plans[0].Environment != "dev" {
			t.Fatalf("unexpected parsed result: %+v", plans)
		}
	})

	t.Run("parses array of plans", func(t *testing.T) {
		p1 := validBasePlan()
		p1.Environment = "dev"
		p2 := validBasePlan()
		p2.Environment = "staging"
		p3 := validBasePlan()
		p3.Environment = "prod"

		data, err := json.Marshal([]backup.EnvironmentPlanRequest{p1, p2, p3})
		if err != nil {
			t.Fatal(err)
		}
		plans, err := backup.ParseEnvironmentPlans(data)
		if err != nil {
			t.Fatalf("failed to parse plan array: %v", err)
		}
		if len(plans) != 3 {
			t.Fatalf("expected 3 plans, got %d", len(plans))
		}
	})

	t.Run("parses map of plans", func(t *testing.T) {
		p1 := validBasePlan()
		p2 := validBasePlan()
		envMap := map[string]backup.EnvironmentPlanRequest{
			"dev":  p1,
			"prod": p2,
		}
		data, err := json.Marshal(envMap)
		if err != nil {
			t.Fatal(err)
		}
		plans, err := backup.ParseEnvironmentPlans(data)
		if err != nil {
			t.Fatalf("failed to parse map: %v", err)
		}
		if len(plans) != 2 {
			t.Fatalf("expected 2 plans, got %d", len(plans))
		}
	})

	t.Run("rejects empty data", func(t *testing.T) {
		if _, err := backup.ParseEnvironmentPlans([]byte("   ")); err == nil {
			t.Fatal("expected error on empty data")
		}
	})
}

func TestLogEnvironmentPlanValidation(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	plan := validBasePlan()
	res := backup.ValidateEnvironmentPlan(plan)

	backup.LogEnvironmentPlanValidation(logger, plan, res)
	logOutput := buf.String()
	if !strings.Contains(logOutput, "railway environment plan evaluated") {
		t.Fatalf("expected operational log message, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, `"environment":"dev"`) {
		t.Fatalf("missing environment in log: %s", logOutput)
	}
}
