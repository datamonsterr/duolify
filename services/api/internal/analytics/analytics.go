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
	return c.Enqueue(posthog.Capture{DistinctId: learnerID, Event: event, Properties: posthog.Properties(properties)})
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
