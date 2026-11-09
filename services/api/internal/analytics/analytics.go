// Package analytics defines the server event boundary. Bootstrap emits no learner events.
package analytics

import (
	"time"

	"github.com/posthog/posthog-go"
)

// Events supports server-side events and flushes queued events during shutdown.
type Events interface {
	Capture(learnerID, event string, properties map[string]any) error
	Close() error
}
type noop struct{}

func (noop) Capture(string, string, map[string]any) error { return nil }
func (noop) Close() error                                 { return nil }

type client struct{ posthog.Client }

func (c client) Capture(learnerID, event string, properties map[string]any) error {
	if !allowlisted(event) || learnerID == "" {
		return nil
	}
	return c.Enqueue(posthog.Capture{DistinctId: learnerID, Event: event, Properties: posthog.Properties(properties)})
}

func allowlisted(event string) bool {
	switch event {
	case "learner_profile_updated", "onboarding_completed", "analytics_opt_out_changed",
		"backup_completed", "backup_failed", "restore_drill_completed", "eval_run_completed",
		"$ai_generation", "ui_control_activated",
		"pricing_version_created", "pricing_version_approved", "pricing_version_rejected",
		"pricing_version_rolled_back", "pricing_version_activated", "price_table_activated":
		return true
	default:
		return false
	}
}

// New disables analytics completely when the key is empty.
func New(key, host string) (Events, error) {
	if key == "" {
		return noop{}, nil
	}
	sdk, err := posthog.NewWithConfig(key, posthog.Config{Endpoint: host, ShutdownTimeout: 3 * time.Second, BatchUploadTimeout: 2 * time.Second})
	if err != nil {
		return nil, err
	}
	return client{sdk}, nil
}
