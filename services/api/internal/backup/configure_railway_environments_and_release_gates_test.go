package backup_test

import (
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/backup"
)

type testEventRecorder struct {
	names      []string
	properties []map[string]any
	ids        []string
}

func (r *testEventRecorder) Capture(id, name string, p map[string]any) error {
	r.ids = append(r.ids, id)
	r.names = append(r.names, name)
	r.properties = append(r.properties, p)
	return nil
}

func (r *testEventRecorder) Close() error { return nil }

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

func TestEnvironmentPlanEventEmitter(t *testing.T) {
	recorder := &testEventRecorder{}
	plan := validBasePlan()

	// Evaluate with event recorder
	backup.EmitEnvironmentPlanEvent(plan, true, recorder, false)

	if len(recorder.names) != 1 {
		t.Fatalf("expected 1 event, got %d", len(recorder.names))
	}
	if recorder.names[0] != "environment_plan_validated" {
		t.Fatalf("expected event 'environment_plan_validated', got %q", recorder.names[0])
	}
	if recorder.ids[0] != "00000000-0000-4000-8000-000000000011" {
		t.Fatalf("expected operations UUID, got %q", recorder.ids[0])
	}

	props := recorder.properties[0]
	if props["source"] != "api" || props["platform"] != "web" || props["app_version"] == "" {
		t.Fatalf("missing common properties: %+v", props)
	}
	if props["environment"] != "dev" || props["ok"] != true {
		t.Fatalf("invalid payload properties: %+v", props)
	}

	// Verify no private text or secret keys in properties
	for k, v := range props {
		if strings.Contains(k, "secret") || strings.Contains(k, "token") || strings.Contains(k, "prompt") {
			t.Fatalf("private property key %q in event", k)
		}
		if s, ok := v.(string); ok && (strings.Contains(s, "Bearer") || strings.Contains(s, "sk-")) {
			t.Fatalf("private property value %q for key %q", s, k)
		}
	}

	// Test opt-out suppresses event
	optOutRecorder := &testEventRecorder{}
	backup.EmitEnvironmentPlanEvent(plan, true, optOutRecorder, true) // optOut = true
	if len(optOutRecorder.names) != 0 {
		t.Fatalf("opt-out must suppress event emission, got %d events", len(optOutRecorder.names))
	}
}
