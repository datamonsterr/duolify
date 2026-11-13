//go:build integration

package pricing

import (
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

type lifecycleCapture struct{ events []capturedLifecycle }
type capturedLifecycle struct {
	id, name string
	props    map[string]any
}

func (c *lifecycleCapture) Capture(id, name string, props map[string]any) error {
	c.events = append(c.events, capturedLifecycle{id, name, props})
	return nil
}
func (*lifecycleCapture) Close() error { return nil }

func TestPricingLifecycleEmitterUsesUUIDAndHonorsOptOut(t *testing.T) {
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("pricing_events"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)
	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err = migrations.Up(ctx, url); err != nil {
		t.Fatal(err)
	}
	db, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var optedIn, optedOut string
	if err = db.Pool.QueryRow(ctx, `INSERT INTO learners(display_name) VALUES('analytics in') RETURNING id::text`).Scan(&optedIn); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, `INSERT INTO learners(display_name) VALUES('analytics out') RETURNING id::text`).Scan(&optedOut); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, `INSERT INTO learner_profiles(learner_id,analytics_opt_out) VALUES($1::uuid,false),($2::uuid,true)`, optedIn, optedOut); err != nil {
		t.Fatal(err)
	}
	sink := new(lifecycleCapture)
	service := NewAdmin(db.Pool, sink, "")
	service.emit(ctx, optedIn, "usd-events", "pricing_version_created", "adverse")
	service.emit(ctx, optedIn, "usd-events", "pricing_version_approved", "adverse")
	service.emit(ctx, optedIn, "usd-events", "pricing_version_rejected", "adverse")
	service.emit(ctx, optedOut, "usd-events", "pricing_version_rejected", "adverse")
	if len(sink.events) != 3 {
		t.Fatalf("opt-out was ignored or opted-in event lost: %+v", sink.events)
	}
	for i, event := range sink.events {
		want := []string{"pricing_version_created", "pricing_version_approved", "pricing_version_rejected"}[i]
		if event.id != optedIn || event.name != want || event.props["actor_id"] != optedIn || event.props["pricing_version"] != "usd-events" || event.props["source"] != "api" || event.props["platform"] != "web" || event.props["app_version"] == "" {
			t.Fatalf("invalid lifecycle event: %+v", event)
		}
		if _, ok := event.props["reason"]; ok {
			t.Fatal("reason text reached analytics")
		}
	}
}
