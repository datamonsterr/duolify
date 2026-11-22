// Command posthog-setup verifies PostHog ingestion and provisions the AI dashboard.
//
//	posthog-setup verify [--ai]   send labelled test events through the API wrapper and confirm ingestion
//	posthog-setup apply           create or update the AI reliability and cost dashboard (idempotent)
//
// Needs POSTHOG_HOST (ingestion host), POSTHOG_PROJECT_ID, POSTHOG_API_KEY (project key)
// and POSTHOG_PERSONAL_API_KEY. Secrets are never printed.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/analytics"
)

// checkLearner is a fixed synthetic UUID; the wrapper only accepts learner UUIDs.
const checkLearner = "5eed0000-0000-4000-8000-00000000c4ec"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "posthog-setup:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: posthog-setup verify [--ai] | apply")
	}
	host, project, key, personal := os.Getenv("POSTHOG_HOST"), os.Getenv("POSTHOG_PROJECT_ID"), os.Getenv("POSTHOG_API_KEY"), os.Getenv("POSTHOG_PERSONAL_API_KEY")
	if host == "" {
		host = "https://us.i.posthog.com"
	}
	if project == "" || personal == "" {
		return errors.New("POSTHOG_PROJECT_ID and POSTHOG_PERSONAL_API_KEY are required")
	}
	api := &analytics.PostHogAPI{Base: analytics.AppHost(host), ProjectID: project, Token: personal}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	switch args[0] {
	case "apply":
		if err := api.ApplyAIDashboard(ctx); err != nil {
			return err
		}
		fmt.Println("dashboard applied:", analytics.AIDashboardName)
		return nil
	case "verify":
		if key == "" {
			return errors.New("POSTHOG_API_KEY is required")
		}
		return verify(ctx, api, host, key, len(args) > 1 && args[1] == "--ai")
	}
	return fmt.Errorf("unknown command %q", args[0])
}

func verify(ctx context.Context, api *analytics.PostHogAPI, host, key string, ai bool) error {
	before, err := api.CountEvents(ctx, "duolify_integration_check")
	if err != nil {
		return err
	}
	// The wrapper fails closed without a consent resolver; the synthetic check learner has no profile.
	events, err := analytics.New(key, host, func(context.Context, string) (bool, error) { return true, nil })
	if err != nil {
		return err
	}
	props := map[string]any{"source": "api", "platform": "web", "app_version": "integration-check"}
	if err := events.Capture(checkLearner, "duolify_integration_check", props); err != nil {
		return err
	}
	if ai {
		_ = events.Capture(checkLearner, "$ai_generation", map[string]any{
			"source": "api", "platform": "web", "app_version": "integration-check",
			"feature": "integration_check", "provider": "openrouter", "model": "tencent/hy4-flash",
			"input_tokens": 10, "output_tokens": 5, "latency_ms": 1500, "provider_cost_nanos": 1000, "succeeded": true,
		})
	}
	if err := events.Close(); err != nil {
		return err
	}
	for deadline := time.Now().Add(90 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Second) {
		n, err := api.CountEvents(ctx, "duolify_integration_check")
		if err != nil {
			return err
		}
		if n > before {
			fmt.Printf("ingestion confirmed: duolify_integration_check %d -> %d\n", before, n)
			return nil
		}
	}
	return errors.New("event not visible in PostHog after 90s")
}
