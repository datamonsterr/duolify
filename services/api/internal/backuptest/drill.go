package backuptest

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/backup"
	"github.com/datamonsterr/duolify/services/api/internal/cache"
	"github.com/datamonsterr/duolify/services/api/internal/gamification"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// DrillOptions wires the real drill path to this fixture: the read-only
// restorer identity, the age private key, a throwaway database in the fixture
// Postgres and a disposable Redis. Nothing outside the fixture is reachable.
func (f *Fixture) DrillOptions(t *testing.T, now time.Time) backup.DrillOptions {
	t.Helper()
	versions, err := migrations.Versions()
	if err != nil {
		t.Fatal(err)
	}
	restorer := f.Store("restorer")
	redis := f.redis(t)
	return backup.DrillOptions{
		Store: restorer, Identity: f.identity, Source: "ci", Events: f.events, Now: func() time.Time { return now },
		Restore:            backup.DockerRestorer{Container: f.container.GetContainerID(), AdminURL: f.databaseURL, User: "duolify"}.Restore,
		ExpectedMigrations: versions,
		ReplayDeletions: func(ctx context.Context, pool *pgxpool.Pool) error {
			return accounts.ReplayDeletionLedger(ctx, pool, backup.ReadOnlyLedger{Objects: restorer})
		},
		RebuildRedis: func(ctx context.Context, pool *pgxpool.Pool) (int, error) {
			service := gamification.New(pool, nil)
			service.UseCache(redis)
			return service.RebuildLeagueCache(ctx)
		},
		Checks: []backup.Check{backup.ObjectReferences("application documents exist", func(context.Context, *pgxpool.Pool) ([]string, error) {
			keys := make([]string, 0, len(authoredObjects()))
			for key := range authoredObjects() {
				keys = append(keys, key)
			}
			return keys, nil
		}, f.ApplicationStorage().Exists)},
		ForbiddenValues: []string{"Synthetic Learner", "synthetic@example.test", "synthetic-google-subject"},
	}
}

func (f *Fixture) redis(t *testing.T) *redis.Client {
	t.Helper()
	container, err := testcontainers.GenericContainer(t.Context(), testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{Image: "redis:7-alpine", ExposedPorts: []string{"6379/tcp"}, WaitingFor: wait.ForLog("Ready to accept connections")},
		Started:          true,
	})
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)
	endpoint, err := container.Endpoint(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	client, err := cache.New("redis://" + endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// DeleteLearnerInLedgerOnly records an erasure only in the object ledger, as if
// it happened after the dump was taken.
func (f *Fixture) DeleteLearnerInLedgerOnly(t *testing.T, learner string) {
	t.Helper()
	if err := (backup.DeletionLedger{Objects: f.objects}).Append(t.Context(), accounts.LearnerReference(learner)); err != nil {
		t.Fatal(err)
	}
}

// LoseApplicationObject deletes one authored object from the application bucket.
func (f *Fixture) LoseApplicationObject(t *testing.T) {
	t.Helper()
	keys := make([]string, 0)
	for key := range authoredObjects() {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	if _, err := f.root.Client.DeleteObject(t.Context(), &s3.DeleteObjectInput{Bucket: aws.String("application-assets"), Key: aws.String(keys[0])}); err != nil {
		t.Fatal(err)
	}
}

// AssertDrillEvents closes the PostHog double and asserts every drill outcome:
// restore_drill_completed(ok) in order, one backup_failed per failed drill,
// source=ci, the system UUID, and no properties beyond the catalogue.
func (f *Fixture) AssertDrillEvents(t *testing.T, outcomes []bool) {
	t.Helper()
	if err := f.events.Close(); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var got []bool
	failures, want := 0, 0
	for _, event := range f.captured {
		switch event.Event {
		case "restore_drill_completed", "backup_failed":
		default:
			continue
		}
		if event.Properties["source"] == "api" && event.Event == "backup_failed" {
			continue
		}
		if event.DistinctID != "00000000-0000-4000-8000-000000000011" || event.Properties["source"] != "ci" || event.Properties["platform"] != "web" || event.Properties["app_version"] == "" {
			t.Fatalf("invalid drill common properties: %+v", event)
		}
		if _, ok := event.Properties["duration_ms"].(float64); !ok {
			t.Fatalf("duration_ms missing: %+v", event)
		}
		for key := range event.Properties {
			switch key {
			case "source", "platform", "app_version", "duration_ms", "ok", "$lib", "$lib_version", "$os", "$os_version", "$os_distro", "$go_version", "$geoip_disable", "$is_server":
			default:
				t.Fatalf("unexpected drill property %q", key)
			}
		}
		if event.Event == "backup_failed" {
			failures++
			continue
		}
		got = append(got, event.Properties["ok"] == true)
	}
	for _, ok := range outcomes {
		if !ok {
			want++
		}
	}
	if !slices.Equal(got, outcomes) || failures != want {
		t.Fatalf("restore_drill_completed ok=%v backup_failed=%d, want %v and %d", got, failures, outcomes, want)
	}
}
