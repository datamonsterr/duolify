// Package backuptest provides disposable synthetic Postgres/RustFS fixtures for
// the backup e2e and integration suites. It never accesses configured services.
package backuptest

import (
	"bytes"
	"context"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/backup"
	"github.com/datamonsterr/duolify/services/api/internal/backupadmin"
	"github.com/datamonsterr/duolify/services/api/internal/characters"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/credits/creditstest"
	"github.com/datamonsterr/duolify/services/api/internal/gamification"
	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/datamonsterr/duolify/services/api/internal/player/playerfixture"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/internal/telemetry"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/moby/moby/api/types/container"
	"github.com/riverqueue/river"
	"github.com/testcontainers/testcontainers-go"
	containerexec "github.com/testcontainers/testcontainers-go/exec"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Fixture owns disposable databases, storage, a River worker and PostHog double.
type Fixture struct {
	paymentFixture      *creditstest.Stripe
	paymentOrderID      string
	subscriptionOrderID string
	DB                  *store.Store
	Logger              *slog.Logger
	container           *postgres.PostgresContainer
	databaseURL         string
	identity            *age.X25519Identity
	objects             *backup.S3
	root                *backup.S3
	faults              *faultStore
	queue               *river.Client[pgx.Tx]
	events              analytics.Events
	mu                  sync.Mutex
	captured            []capture
	posthogURL          string
	monitor             *telemetry.Monitor
	writerOptions       backup.S3Options
	packaged            testcontainers.Container
	schemaVersion       int64
	lastDate            string
}

type capture struct {
	Event      string         `json:"event"`
	DistinctID string         `json:"distinct_id"`
	Properties map[string]any `json:"properties"`
}

// AssertPreDeletionRestoreErased restores a real dump that predates a deletion.
// Only object storage knows the deletion; the restored SQL ledger has no record.
func (f *Fixture) AssertPreDeletionRestoreErased(t *testing.T, ciphertext []byte) {
	t.Helper()
	const learner = "a1111111-1111-4111-8111-111111111111"
	ref := accounts.LearnerReference(learner)
	ledger := backup.DeletionLedger{Objects: f.objects}
	if err := ledger.Append(t.Context(), ref); err != nil {
		t.Fatal(err)
	}
	if _, err := f.DB.Pool.Exec(t.Context(), "DELETE FROM learners WHERE id=$1::uuid", learner); err != nil {
		t.Fatal(err)
	}
	restored := f.Restore(t, ciphertext)
	if err := accounts.ReplayDeletionLedger(t.Context(), restored.Pool, ledger); err != nil {
		t.Fatal(err)
	}
	var resurrected bool
	if err := restored.Pool.QueryRow(t.Context(), "SELECT EXISTS(SELECT 1 FROM learners WHERE id=$1::uuid)", learner).Scan(&resurrected); err != nil {
		t.Fatal(err)
	}
	if resurrected {
		t.Fatal("restoring a pre-deletion dump resurrected the learner")
	}
	if err := accounts.ReplayDeletionLedger(t.Context(), restored.Pool, ledger); err != nil {
		t.Fatal("replay must be idempotent", err)
	}
}

// New starts isolated containers and all production migrations, never live providers.
func New(t *testing.T) *Fixture {
	return NewObserved(t, nil, slog.New(slog.NewJSONHandler(io.Discard, nil)))
}

// NewObserved shares the real worker's operational collector with an HTTP action.
func NewObserved(t *testing.T, monitor *telemetry.Monitor, logger *slog.Logger) *Fixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	t.Cleanup(cancel)
	f := &Fixture{Logger: logger, monitor: monitor}
	var err error
	f.container, err = postgres.Run(ctx, "postgres:18", postgres.WithDatabase("duolify"), postgres.WithUsername("duolify"), postgres.WithPassword("synthetic-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, f.container)
	f.databaseURL, err = f.container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.Up(ctx, f.databaseURL); err != nil {
		t.Fatal(err)
	}
	f.DB, err = store.New(ctx, f.databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.DB.Close)
	if err := f.DB.Pool.QueryRow(ctx, "SELECT max(version_id) FROM goose_db_version WHERE is_applied").Scan(&f.schemaVersion); err != nil {
		t.Fatal(err)
	}
	if f.schemaVersion < 1 {
		t.Fatal("fixture has no applied application schema")
	}
	if err := worker.Migrate(ctx, f.DB.Pool, f.Logger); err != nil {
		t.Fatal(err)
	}
	objectContainer, err := testcontainers.Run(ctx, "rustfs/rustfs:1.0.4",
		testcontainers.WithEnv(map[string]string{"RUSTFS_ACCESS_KEY": "root-synthetic", "RUSTFS_SECRET_KEY": "root-synthetic-secret", "RUSTFS_VOLUMES": "/data", "RUSTFS_CONSOLE_ENABLE": "false"}),
		testcontainers.WithExposedPorts("9000/tcp"), testcontainers.WithCmd("/data"),
		testcontainers.WithWaitStrategy(wait.ForHTTP("/health/ready").WithPort("9000/tcp")))
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, objectContainer)
	endpoint, err := objectContainer.PortEndpoint(ctx, "9000/tcp", "http")
	if err != nil {
		t.Fatal(err)
	}
	rootOptions := backup.S3Options{Endpoint: endpoint, Region: "us-east-1", Bucket: "synthetic-backups", ApplicationBucket: "application-assets", AccessKey: "root-synthetic", SecretKey: "root-synthetic-secret"}
	if err := backupadmin.Provision(ctx, rootOptions, "backup-writer", "writer-synthetic-secret"); err != nil {
		t.Fatal(err)
	}
	if err := backupadmin.ProvisionApplication(ctx, rootOptions, "application-writer", "application-synthetic-secret"); err != nil {
		t.Fatal(err)
	}
	f.root, err = backup.NewS3(rootOptions)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []struct {
		key       string
		provision func(context.Context, backup.S3Options, string, string) error
	}{{"backup-expirer", backupadmin.ProvisionRetention}, {"backup-restorer", backupadmin.ProvisionRestorer}, {"asset-recovery", backupadmin.ProvisionAssetRecovery}} {
		if err := id.provision(ctx, rootOptions, id.key, id.key+"-synthetic-secret"); err != nil {
			t.Fatal(err)
		}
	}
	// application-assets is created and versioned by ProvisionApplication.
	for _, bucket := range []string{"protected-player-assets"} {
		if _, err := f.root.Client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.root.Client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{Bucket: aws.String(bucket), VersioningConfiguration: &types.VersioningConfiguration{Status: types.BucketVersioningStatusEnabled}}); err != nil {
			t.Fatal(err)
		}
	}

	writerOptions := rootOptions
	writerOptions.AccessKey = "backup-writer"
	writerOptions.SecretKey = "writer-synthetic-secret"
	f.writerOptions = writerOptions
	f.objects, err = backup.NewS3(writerOptions)
	if err != nil {
		t.Fatal(err)
	}
	f.identity, err = age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	posthog := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var batch struct {
			Batch []capture `json:"batch"`
		}
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			t.Errorf("PostHog decode: %v", err)
			w.WriteHeader(400)
			return
		}
		f.mu.Lock()
		f.captured = append(f.captured, batch.Batch...)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"Ok"}`)
	}))
	t.Cleanup(posthog.Close)
	f.posthogURL = posthog.URL
	f.events, err = analytics.New("synthetic-project-key", posthog.URL, func(context.Context, string) (bool, error) { return true, nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.events.Close() })
	f.faults = &faultStore{Objects: f.objects}
	f.startQueue(ctx, t, false)
	return f
}

func (f *Fixture) startQueue(ctx context.Context, t *testing.T, financial bool) {
	t.Helper()
	source := containerSource{Postgres: backup.Postgres{Pool: f.DB.Pool}, container: f.container}
	service, err := backup.New(backup.Options{Source: source, Objects: f.faults, Recipient: f.identity.Recipient().String(), Events: f.events, TempDir: t.TempDir(), Monitor: f.monitor, FinancialCopy: financial})
	if err != nil {
		t.Fatal(err)
	}
	f.queue, err = worker.New(f.DB.Pool, f.Logger, service)
	if err != nil {
		t.Fatal(err)
	}
	// The tests insert known dates. Calendar behavior is independently unit-tested.
	f.queue.PeriodicJobs().Clear()
	if err := f.queue.Start(ctx); err != nil {
		t.Fatal(err)
	}
	queue := f.queue
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		// A queue stopped by a test is already down; a second Stop only reports that.
		_ = queue.Stop(stopCtx)
	})
}

// UseFinancialCopy restarts the real River worker with the 35-day financial copy enabled.
func (f *Fixture) UseFinancialCopy(t *testing.T) {
	t.Helper()
	stopCtx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := f.queue.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
	f.startQueue(t.Context(), t, true)
}

// SchemaVersionString is the applied Goose version used in archive keys.
func (f *Fixture) SchemaVersionString() string { return fmt.Sprint(f.schemaVersion) }

// RootClient exposes the disposable store's administrator client for assertions only.
func (f *Fixture) RootClient() *s3.Client { return f.root.Client }

// SeedBackupObject plants a dump-shaped fixture with the administrator identity.
func (f *Fixture) SeedBackupObject(t *testing.T, key string, body []byte) {
	t.Helper()
	if _, err := f.root.Client.PutObject(t.Context(), &s3.PutObjectInput{Bucket: aws.String(f.root.Bucket), Key: aws.String(key), Body: bytes.NewReader(body)}); err != nil {
		t.Fatal(err)
	}
}

// BackupKeys lists every key in the isolated backup bucket.
func (f *Fixture) BackupKeys(t *testing.T) []string {
	t.Helper()
	objects, err := f.root.List(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(objects))
	for _, o := range objects {
		keys = append(keys, o.Key)
	}
	return keys
}

// Store returns a client for one scoped identity: writer, expirer or restorer on the
// backup bucket.
func (f *Fixture) Store(role string) *backup.S3 {
	options := f.writerOptions
	switch role {
	case "writer":
	case "expirer":
		options.AccessKey, options.SecretKey = "backup-expirer", "backup-expirer-synthetic-secret"
	case "restorer":
		options.AccessKey, options.SecretKey = "backup-restorer", "backup-restorer-synthetic-secret"
	default:
		panic("unknown fixture role " + role)
	}
	store, err := backup.NewS3(options)
	if err != nil {
		panic(err)
	}
	return store
}

// Expirer builds the retention job with the separate expirer identity.
func (f *Fixture) Expirer(now time.Time) backup.Expirer {
	return backup.Expirer{Store: f.Store("expirer"), Retention: backup.DefaultRetention, Now: func() time.Time { return now }, Events: f.events, Logger: f.Logger}
}

// AssetOperator is the version-aware operator client for the application bucket.
func (f *Fixture) AssetOperator() *backup.S3 {
	options := f.writerOptions
	options.Bucket, options.ApplicationBucket = options.ApplicationBucket, ""
	options.AccessKey, options.SecretKey = "asset-recovery", "asset-recovery-synthetic-secret"
	store, err := backup.NewS3(options)
	if err != nil {
		panic(err)
	}
	return store
}

// Recovery builds tracked restore/regeneration with the operator identity.
func (f *Fixture) Recovery() backup.Recovery {
	return backup.Recovery{Objects: f.AssetOperator(), Logger: f.Logger, Events: f.events}
}

type containerSource struct {
	backup.Postgres
	container *postgres.PostgresContainer
}

func (s containerSource) Dump(ctx context.Context, w io.Writer) error {
	if err := s.PurgeExpiredBriefs(ctx); err != nil {
		return err
	}
	code, r, err := s.container.Exec(ctx, []string{"pg_dump", "--format=custom", "--no-owner", "--no-acl", "--no-password", "--username=duolify", "--dbname=duolify"}, containerexec.Multiplexed())
	if err != nil {
		return err
	}
	if code != 0 {
		return errors.New("fixture pg_dump failed")
	}
	_, err = io.Copy(w, r)
	return err
}

type faultStore struct {
	backup.Objects
	fail atomic.Bool
}

func (s *faultStore) Put(ctx context.Context, key string, body io.ReadSeeker, size int64) error {
	if s.fail.Swap(false) {
		_, _ = io.CopyN(io.Discard, body, 32)
		return errors.New("injected interrupted upload")
	}
	return s.Objects.Put(ctx, key, body, size)
}

// FailNextUpload interrupts one publication to exercise River's durable retry.
func (f *Fixture) FailNextUpload() { f.faults.fail.Store(true) }

// Events returns the fixture's PostHog events double.
func (f *Fixture) Events() analytics.Events { return f.events }

// ApplicationStorage returns an S3 client scoped to the application assets bucket.
func (f *Fixture) ApplicationStorage() *backup.S3 {
	opts := f.writerOptions
	opts.Bucket, opts.ApplicationBucket = opts.ApplicationBucket, ""
	opts.AccessKey = "application-writer"
	opts.SecretKey = "application-synthetic-secret"
	s3Client, err := backup.NewS3(opts)
	if err != nil {
		panic(err)
	}
	return s3Client
}

// UsePackagedWorker exercises the actual non-root worker image, pg_dump process,
// runtime config, schedule and PostHog lifecycle. Host networking is local-only
// and reaches only this test's randomly published disposable service ports.
func (f *Fixture) UsePackagedWorker(t *testing.T) {
	t.Helper()
	stopCtx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := f.queue.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
	var err error
	f.packaged, err = testcontainers.Run(t.Context(), "duolify-worker:local",
		testcontainers.WithHostConfigModifier(func(config *container.HostConfig) { config.NetworkMode = "host" }),
		testcontainers.WithEnv(map[string]string{
			"DATABASE_URL": f.databaseURL, "REDIS_URL": "redis://127.0.0.1:6379/0", "ENGINE_ADDR": "http://127.0.0.1:50051",
			"S3_BUCKET": "application-assets", "BACKUP_S3_ENDPOINT": f.writerOptions.Endpoint, "BACKUP_S3_REGION": f.writerOptions.Region, "BACKUP_S3_BUCKET": f.writerOptions.Bucket,
			"BACKUP_S3_ACCESS_KEY_ID": f.writerOptions.AccessKey, "BACKUP_S3_SECRET_ACCESS_KEY": f.writerOptions.SecretKey, "BACKUP_AGE_RECIPIENT": f.identity.Recipient().String(),
			"POSTHOG_API_KEY": "synthetic-project-key", "POSTHOG_HOST": f.posthogURL,
		}))
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, f.packaged)
	// Observe the real startup schedule before manual redelivery. Otherwise a
	// fast manual job can finish and be cleaned up before RunOnStart inserts,
	// introducing an unrelated third delivery into the idempotency assertion.
	date := time.Now().UTC().Format(time.DateOnly)
	timeout := time.NewTimer(40 * time.Second)
	defer timeout.Stop()
	poll := time.NewTicker(50 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-poll.C:
			var count int64
			if err := f.DB.Pool.QueryRow(t.Context(), "SELECT count(*) FROM river_job WHERE kind=$1 AND args->>'date'=$2", backup.Args{}.Kind(), date).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count > 0 {
				return
			}
		case <-timeout.C:
			t.Fatal("packaged worker did not schedule its startup backup")
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		}
	}
}

// Seed adds synthetic learner, auth state and append-only ledger data.
func (f *Fixture) Seed(t *testing.T) {
	t.Helper()
	outline := courses.Outline{{ID: "fixture-unit", Title: "Moon", Lessons: []courses.OutlineLesson{{ID: "fixture-lesson", Title: "Phases", Goal: "Recognize phases"}}}}
	for _, query := range []string{
		"INSERT INTO learners (id,display_name) VALUES ('a1111111-1111-4111-8111-111111111111','Synthetic Learner')",
		`INSERT INTO credit_checkout_orders(request_id,learner_id,owner_reference,pricing_version,sku,amount_minor,currency,credits,billing_period,platform,return_to)
SELECT gen_random_uuid(),'a1111111-1111-4111-8111-111111111111',encode(sha256(convert_to('a1111111-1111-4111-8111-111111111111','UTF8')),'hex'),version,config->'packs'->0->>'sku',(config->'packs'->0->>'amount_minor')::bigint,config->'packs'->0->>'currency',(config->'packs'->0->>'credits')::bigint,'one_time','web','/credits' FROM pricing_versions WHERE version='usd-launch-2026-11-08'`,
		"INSERT INTO credit_entries (learner_id,amount,kind,reference,pricing_version,provenance) VALUES ('a1111111-1111-4111-8111-111111111111',120,'purchase','synthetic-purchase','2026-11-09.approved.v1','{\"channel\":\"web\"}'::jsonb),('a1111111-1111-4111-8111-111111111111',-35,'spend','synthetic-spend','2026-11-09.approved.v1','{\"action\":\"lesson\"}'::jsonb)",
		"INSERT INTO credit_timezones (learner_id,time_zone,effective_at) VALUES ('a1111111-1111-4111-8111-111111111111','Asia/Ho_Chi_Minh',now())",
		`INSERT INTO course_brief_revisions(learner_id,brief,content_hash) VALUES ('a1111111-1111-4111-8111-111111111111','{"topic":"Synthetic sky","include_topics":"Moon","skip_topics":"History","maths":"Light maths"}',repeat('b',64))`,
		`INSERT INTO course_brief_revisions(learner_id,brief,content_hash,expires_at) VALUES ('a1111111-1111-4111-8111-111111111111','{"topic":"Expired synthetic sky"}',repeat('e',64),now()-interval '1 second')`,
		fmt.Sprintf(`INSERT INTO outline_drafts(id,learner_id,brief_revision,revision,outline,history,confirmed)
 SELECT 'c7222222-2222-4222-8222-222222222222',learner_id,id,3,'[{"id":"fixture-unit","title":"Moon","lessons":[{"id":"fixture-lesson","title":"Phases","goal":"Recognize phases"}]}]',
 '[{"value":[{"id":"fixture-unit","title":"Before edit","lessons":[{"id":"fixture-lesson","title":"Phases","goal":"Recognize phases"}]}],"focusId":"fixture-unit"}]',
 '{"draft_id":"c7222222-2222-4222-8222-222222222222","revision":3,"content_hash":"%s","outline":[{"id":"fixture-unit","title":"Moon","lessons":[{"id":"fixture-lesson","title":"Phases","goal":"Recognize phases"}]}]}'
 FROM course_brief_revisions WHERE expires_at>now()`, outline.Hash()),
		"INSERT INTO learner_profiles (learner_id,locale,time_zone,analytics_opt_out) VALUES ('a1111111-1111-4111-8111-111111111111','vi-VN','Asia/Ho_Chi_Minh',true)",
		"UPDATE learner_profiles SET goal='Explain the sky',topic='Astronomy',background='School physics',level='beginner',daily_minutes=10,daily_xp=20,reminder_enabled=false,reminder_time='09:30',setup_completed=true,appearance='dark',motion='reduce',narration=false,narration_speed=1.25,captions=false,text_size=1.2,ui_sounds=true,haptics='off' WHERE learner_id='a1111111-1111-4111-8111-111111111111'",
		"INSERT INTO auth_identities (email,learner_id) VALUES ('synthetic@example.test','a1111111-1111-4111-8111-111111111111')",
		"INSERT INTO auth_provider_identities (provider,subject,learner_id,verified_email) VALUES ('google','synthetic-google-subject','a1111111-1111-4111-8111-111111111111','synthetic@example.test')",
		"INSERT INTO auth_provider_identities (provider,subject,learner_id,verified_email) VALUES ('apple','synthetic-apple-subject','a1111111-1111-4111-8111-111111111111','relay@privaterelay.appleid.com')",
		"INSERT INTO auth_oauth_attempts (state_hash,nonce_hash,code_challenge,redirect_uri,expires_at) VALUES (repeat('d',64),repeat('e',64),repeat('f',43),'duolify://oauth/callback',now()+interval '5 minutes')",
		"INSERT INTO auth_codes (email,code_hash,code_ciphertext,last_sent_at,expires_at,attempts) VALUES ('synthetic@example.test',repeat('a',64),decode('01020304','hex'),now()-interval '2 minutes',now()-interval '1 minute',5)",
		"INSERT INTO auth_sessions (id,learner_id) VALUES ('b2222222-2222-4222-8222-222222222222','a1111111-1111-4111-8111-111111111111')",
		fmt.Sprintf("INSERT INTO auth_refresh_tokens (token_hash,session_id,expires_at,consumed_at,successor_ciphertext) VALUES ('%x','b2222222-2222-4222-8222-222222222222',now()+interval '30 days',now()-interval '1 minute',decode('05060708','hex'))", sha256.Sum256([]byte("synthetic-consumed-refresh"))),
		fmt.Sprintf("INSERT INTO auth_refresh_tokens (token_hash,session_id,expires_at) VALUES ('%x','b2222222-2222-4222-8222-222222222222',now()+interval '30 days')", sha256.Sum256([]byte("synthetic-active-refresh"))),
		"INSERT INTO auth_sessions (id,learner_id,revoked_at) VALUES ('d4444444-4444-4444-8444-444444444444','a1111111-1111-4111-8111-111111111111',now())",
		fmt.Sprintf("INSERT INTO auth_refresh_tokens (token_hash,session_id,expires_at) VALUES ('%x','d4444444-4444-4444-8444-444444444444',now()+interval '30 days')", sha256.Sum256([]byte("synthetic-revoked-refresh"))),
		"INSERT INTO auth_rate_limits (key_hash,email_hash,count,expires_at) VALUES (repeat('c',64),repeat('d',64),3,now()+interval '1 hour')",
		"INSERT INTO pricing_admin_grants(account_id) VALUES ('a1111111-1111-4111-8111-111111111111')",
		"INSERT INTO pricing_activation_events(version) VALUES ('usd-launch-2026-11-08') ON CONFLICT DO NOTHING",
		"INSERT INTO pricing_versions(version,effective_from,approved,config) SELECT 'usd-backup-scheduled',now()+interval '96 hours',true,jsonb_set(jsonb_set(config,'{version}','\"usd-backup-scheduled\"'),'{effective_from}',to_jsonb(now()+interval '96 hours')) FROM pricing_versions WHERE version='usd-launch-2026-11-08'",
		"INSERT INTO pricing_version_cancellations(version,cancelled_by,reason) VALUES ('usd-backup-scheduled','a1111111-1111-4111-8111-111111111111','synthetic restore cancellation')",
		"INSERT INTO pricing_verification_evidence(version,evidence) VALUES ('usd-backup-scheduled','{\"margin_floor_verified\":true,\"economics_verified\":true,\"mappings_verified\":true}'::jsonb)",
		"INSERT INTO pricing_drafts(version,parent_version,config,created_by,change_class) SELECT 'usd-backup-draft',version,jsonb_set(jsonb_set(config,'{version}','\"usd-backup-draft\"'),'{effective_from}',to_jsonb(now()+interval '96 hours')),'a1111111-1111-4111-8111-111111111111','non_adverse' FROM pricing_versions WHERE version='usd-launch-2026-11-08'",
		"INSERT INTO pricing_drafts(version,parent_version,config,created_by,change_class,rollback_source_version) SELECT 'usd-backup-rollback',version,jsonb_set(jsonb_set(config,'{version}','\"usd-backup-rollback\"'),'{effective_from}',to_jsonb(now()+interval '120 hours')),'a1111111-1111-4111-8111-111111111111','non_adverse','usd-launch-2026-11-08' FROM pricing_versions WHERE version='usd-launch-2026-11-08'",
		fmt.Sprintf("INSERT INTO pricing_audit(actor_id,action,version,change_class,reason,request_id,override_reason) VALUES ('%x','version_created','usd-backup-draft','non_adverse','synthetic restore audit','restore-fixture','synthetic audited override')", sha256.Sum256([]byte("a1111111-1111-4111-8111-111111111111"))),
		"INSERT INTO account_deletion_tombstones (learner_reference) VALUES (repeat('9',64))",
	} {
		if _, err := f.DB.Pool.Exec(t.Context(), query); err != nil {
			t.Fatal(err)
		}
	}
	quote, err := pricing.NewQuotes(f.DB.Pool).Create(t.Context(), "a1111111-1111-4111-8111-111111111111", pricing.QuoteInput{Action: "lesson_content", NarrationCharacters: 1440})
	if err != nil {
		t.Fatal(err)
	}
	if err := pricing.NewQuotes(f.DB.Pool).Accept(t.Context(), "a1111111-1111-4111-8111-111111111111", quote.ID); err != nil {
		t.Fatal(err)
	}

	proposalQuote, err := pricing.NewQuotes(f.DB.Pool).CreateDevelopmentOutline(t.Context(), "a1111111-1111-4111-8111-111111111111", pricing.QuoteInput{Action: "outline_chat_change", OutlineDraftID: "c7222222-2222-4222-8222-222222222222", OutlineRevision: 3, RequestHash: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO outline_proposals(learner_id,draft_id,quote_id,base_revision,base_outline,request,operations,result_outline,attempts,state,decision,applied_revision,decision_draft)
 SELECT learner_id,id,$1,2,outline,'Synthetic change','[{"kind":"retitled","id":"fixture-unit","title":"Moon"}]',outline,'[]','delivered','applied',revision,to_jsonb(d) FROM outline_drafts d WHERE id='c7222222-2222-4222-8222-222222222222'`, proposalQuote.ID); err != nil {
		t.Fatal(err)
	}

	outlineQuote, err := pricing.NewQuotes(f.DB.Pool).Create(t.Context(), "a1111111-1111-4111-8111-111111111111", pricing.QuoteInput{Action: "outline", OutlineDraftID: "c7222222-2222-4222-8222-222222222222", OutlineRevision: 3, RequestHash: outline.Hash()})
	if err != nil {
		t.Fatal(err)
	}
	if err := pricing.NewQuotes(f.DB.Pool).Accept(t.Context(), "a1111111-1111-4111-8111-111111111111", outlineQuote.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO course_creations(id,learner_id,draft_id,revision,quote_id,request,state,course_id) VALUES('e1111111-1111-4111-8111-111111111111','a1111111-1111-4111-8111-111111111111','c7222222-2222-4222-8222-222222222222',3,$1,'{"draft_id":"c7222222-2222-4222-8222-222222222222"}','enrolled','f1111111-1111-4111-8111-111111111111')`, outlineQuote.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO courses(id,learner_id,creation_id,title,description) VALUES('f1111111-1111-4111-8111-111111111111','a1111111-1111-4111-8111-111111111111','e1111111-1111-4111-8111-111111111111','Synthetic sky','')`); err != nil {
		t.Fatal(err)
	}

	charSpec := characters.DefaultGuideFixture()
	charRaw, _ := json.Marshal(charSpec)
	for _, query := range []string{
		`INSERT INTO characters(id,learner_id) VALUES('17111111-1111-4111-8111-111111111111','a1111111-1111-4111-8111-111111111111')`,
		`INSERT INTO character_versions(character_id,version,spec) VALUES('17111111-1111-4111-8111-111111111111',1,$1)`,
		`INSERT INTO character_screenings(learner_id,character_id,verdict,policy_version) VALUES('a1111111-1111-4111-8111-111111111111','17111111-1111-4111-8111-111111111111','approved','character-v1')`,
		`INSERT INTO course_casts(course_id,learner_id,selection) VALUES('f1111111-1111-4111-8111-111111111111','a1111111-1111-4111-8111-111111111111','{"guide":"","supporting":[]}')`,
	} {
		var args []any
		if strings.Contains(query, "$1") {
			args = []any{charRaw}
		}
		if _, err = f.DB.Pool.Exec(t.Context(), query, args...); err != nil {
			t.Fatal(err)
		}
	}
	// A deterministically invalid intent has no provider attempt, reservation or raw payload, but
	// its durable dedupe key and bounded verdict must survive encrypted restore.
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO character_create_jobs(id,learner_id,request_key,operation,state,reason_code,finished_at) VALUES('18111111-1111-4111-8111-111111111111','a1111111-1111-4111-8111-111111111111','synthetic-rejected-create','create','rejected','invalid_spec',now())`); err != nil {
		t.Fatal(err)
	}
	charSvc := characters.NewService(f.DB.Pool, nil, nil)
	if _, err = charSvc.PinCast(t.Context(), "a1111111-1111-4111-8111-111111111111", "f1111111-1111-4111-8111-111111111111", "fixture-character-lesson"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO course_units(id,learner_id,course_id,authored_id,position,title) VALUES('f2222222-2222-4222-8222-222222222222','a1111111-1111-4111-8111-111111111111','f1111111-1111-4111-8111-111111111111','fixture-unit',0,'Moon')`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO course_lesson_stubs(id,learner_id,unit_id,authored_id,position,title,goal,status) VALUES('f3333333-3333-4333-8333-333333333333','a1111111-1111-4111-8111-111111111111','f2222222-2222-4222-8222-222222222222','fixture-lesson',0,'Phases','Recognize phases','ready_to_generate')`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO course_enrollments(learner_id,course_id,level_override) VALUES('a1111111-1111-4111-8111-111111111111','f1111111-1111-4111-8111-111111111111','intermediate')`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO quick_notes(id, learner_id, action_key, course_id, lesson_id, slide_id, slide_title, source, raw_text, status, reviewed, reviewed_at, version, raw_version, enrichment_claim_id, enrichment_lease_until) VALUES('a9999999-9999-4999-8999-999999999999', 'a1111111-1111-4111-8111-111111111111', 'synthetic-action-key', 'f1111111-1111-4111-8111-111111111111', 'f3333333-3333-4333-8333-333333333333', 0, 'Slide 1 · Phases', 'slide', 'Synthetic raw note', 'enriching', true, now(), 2, 2, 'a7777777-7777-4777-8777-777777777777', '2026-11-11T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO quick_note_enrichments(id, note_id, learner_id, summary, tags, links, version, model, provider) VALUES('a8888888-8888-4888-8888-888888888888', 'a9999999-9999-4999-8999-999999999999', 'a1111111-1111-4111-8111-111111111111', 'Synthetic summary of phases', ARRAY['phases', 'moon'], ARRAY['f3333333-3333-4333-8333-333333333333'], 1, 'claude-haiku-4.5', 'anthropic')`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO bookmarks(id, learner_id, action_key, course_id, lesson_id, slide_id, target_type, course_title, lesson_title, slide_title) VALUES('b1111111-1111-4111-8111-111111111111', 'a1111111-1111-4111-8111-111111111111', 'synthetic-bookmark-key', 'f1111111-1111-4111-8111-111111111111', 'f3333333-3333-4333-8333-333333333333', 0, 'slide', 'Astronomy', 'Phases', 'Slide 1 · Phases')`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO lesson_generations (id, learner_id, lesson_id, course_id, quote_id, state, result_checkpoint) VALUES ('f4444444-4444-4444-8444-444444444444', 'a1111111-1111-4111-8111-111111111111', 'f3333333-3333-4333-8333-333333333333', 'f1111111-1111-4111-8111-111111111111', $1, 'validated', '{"lesson":{"slides":[]},"attempts":[],"title":"Restorable pending lesson"}'::jsonb)`, outlineQuote.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO generated_lessons (id, lesson_stub_id, learner_id, course_id, title, manifest) VALUES ('f5555555-5555-4555-8555-555555555555', 'f3333333-3333-4333-8333-333333333333', 'a1111111-1111-4111-8111-111111111111', 'f1111111-1111-4111-8111-111111111111', 'Phases of the Moon', '{"title":"Phases of the Moon"}'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO lesson_slides (id, lesson_stub_id, learner_id, position, slide_id, html, duration_ms, narration, topic_ids) VALUES ('f6666666-6666-4666-8666-666666666666', 'f3333333-3333-4333-8333-333333333333', 'a1111111-1111-4111-8111-111111111111', 0, 's1', '<div id="root"><h1>Moon</h1></div>', 3000, 'The moon orbits Earth.', ARRAY['top-moon'])`); err != nil {
		t.Fatal(err)
	}
	// The pending job's Cast is pinned under the job id; restored jobs keep the same Cast.
	if _, err = charSvc.PinCast(t.Context(), "a1111111-1111-4111-8111-111111111111", "f1111111-1111-4111-8111-111111111111", "f4444444-4444-4444-8444-444444444444"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `UPDATE lesson_slides SET segments='[{"speaker_key":"guide","speaker_label":"Glint","text":"Earth turns.","expression":"thinking","start_ms":0,"duration_ms":1}]'::jsonb WHERE id='f6666666-6666-4666-8666-666666666666'`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO lesson_questions (id, lesson_stub_id, learner_id, position, topic_id, prompt, choices, answer_index, explanation) VALUES ('f7777777-7777-4777-8777-777777777777', 'f3333333-3333-4333-8333-333333333333', 'a1111111-1111-4111-8111-111111111111', 0, 'top-moon', 'What causes moon phases?', '["Sunlight angle", "Earth shadow"]'::jsonb, 0, 'Relative angles between Earth, Moon and Sun.')`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO lesson_retry_pools (id, lesson_stub_id, learner_id, topic_id, variants) VALUES ('fbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb', 'f3333333-3333-4333-8333-333333333333', 'a1111111-1111-4111-8111-111111111111', 'top-moon', '[{"question_id":"v1-q1","prompt":"Moon phases?","choices":["Earth shadow","Sun angle"],"answer_index":1,"explanation":"Sun angle"}]'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO lesson_test_sessions (id, lesson_stub_id, learner_id, state, question_queue, resolved_topics) VALUES ('f8888888-8888-4888-8888-888888888888', 'f3333333-3333-4333-8333-333333333333', 'a1111111-1111-4111-8111-111111111111', 'passed', '[]'::jsonb, '["f7777777-7777-4777-8777-777777777777"]'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO lesson_test_sessions (id, lesson_stub_id, learner_id, state, question_queue, resolved_topics) VALUES ('f8888888-8888-4888-8888-888888888889', 'f3333333-3333-4333-8333-333333333333', 'a1111111-1111-4111-8111-111111111111', 'in_progress', '[{"question_id":"c41b8f99-8338-427e-b275-3f8533cc5640","source_id":"f7777777-7777-4777-8777-777777777777","original_id":"f7777777-7777-4777-8777-777777777777","topic_id":"top-moon","prompt":"What causes moon phases?","choices":["Sunlight angle","Earth shadow"]}]'::jsonb, '[]'::jsonb)`); err != nil {
		t.Fatal(err)
	}

	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO lesson_test_answers (id, session_id, learner_id, question_id, topic_id, selected_index, correct) VALUES ('f9999999-9999-4999-8999-999999999999', 'f8888888-8888-4888-8888-888888888888', 'a1111111-1111-4111-8111-111111111111', 'q1', 'top-moon', 0, true)`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO lesson_test_answer_operations (operation_key, session_id, learner_id, lesson_id, question_id, choice_index, response_payload) VALUES ('op-fixture-seed-001', 'f8888888-8888-4888-8888-888888888888', 'a1111111-1111-4111-8111-111111111111', 'f3333333-3333-4333-8333-333333333333', 'q1', 0, '{"correct":true,"passed":true}'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO lesson_completion_outbox (id, learner_id, course_id, lesson_id, state) VALUES ('faaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', 'a1111111-1111-4111-8111-111111111111', 'f1111111-1111-4111-8111-111111111111', 'f3333333-3333-4333-8333-333333333333', 'passed')`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO lesson_completion_summaries (id, learner_id, course_id, lesson_id, summary, weak_topics) VALUES ('fbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb', 'a1111111-1111-4111-8111-111111111111', 'f1111111-1111-4111-8111-111111111111', 'f3333333-3333-4333-8333-333333333333', 'Sample completed summary', '["top-moon"]'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	tutorQuote, err := pricing.NewQuotes(f.DB.Pool).Create(t.Context(), "a1111111-1111-4111-8111-111111111111", pricing.QuoteInput{Action: "tutor_short", InputTokens: 10, ContextTokens: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO tutor_threads (id, learner_id, course_id) VALUES ('f9111111-1111-4111-8111-111111111111', 'a1111111-1111-4111-8111-111111111111', 'f1111111-1111-4111-8111-111111111111')`,
		`INSERT INTO tutor_messages (id, thread_id, learner_id, role, content, status) VALUES ('f9222222-2222-4222-8222-222222222222', 'f9111111-1111-4111-8111-111111111111', 'a1111111-1111-4111-8111-111111111111', 'user', 'Why does the moon change?', 'complete'), ('f9333333-3333-4333-8333-333333333333', 'f9111111-1111-4111-8111-111111111111', 'a1111111-1111-4111-8111-111111111111', 'assistant', 'Sunlight angle', 'stopped')`,
	} {
		if _, err = f.DB.Pool.Exec(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO tutor_turns (learner_id, course_id, idempotency_key, quote_id, state, end_reason, job_id, usage_estimated, thread_id, user_message_id, assistant_message_id, speaker_key, character_id, character_version, cast_source, cast_version) VALUES ('a1111111-1111-4111-8111-111111111111', 'f1111111-1111-4111-8111-111111111111', 'backup-fixture-key', $1, 'stopped', 'stop', 'f9444444-4444-4444-8444-444444444444', true, 'f9111111-1111-4111-8111-111111111111', 'f9222222-2222-4222-8222-222222222222', 'f9333333-3333-4333-8333-333333333333', 'supporter', 'default-supporter', 1, 'course', 2)`, tutorQuote.ID); err != nil {
		t.Fatal(err)
	}
	// E8-05: one note still linked to its tutor reply, one whose thread was deleted (provenance cleared,
	// text-free tombstone kept). Deleted thread text must not be in the dump.
	for _, statement := range []string{
		`INSERT INTO quick_notes(id, learner_id, action_key, course_id, source, raw_text, status) VALUES ('fa111111-1111-4111-8111-111111111111', 'a1111111-1111-4111-8111-111111111111', 'tutor-message:f9333333-3333-4333-8333-333333333333', 'f1111111-1111-4111-8111-111111111111', 'chat', 'Sunlight angle', 'enrichment_pending'), ('fa222222-2222-4222-8222-222222222222', 'a1111111-1111-4111-8111-111111111111', 'tutor-message:fa999999-9999-4999-8999-999999999999', 'f1111111-1111-4111-8111-111111111111', 'chat', 'Saved before its thread was deleted', 'enrichment_pending')`,
		`INSERT INTO quick_note_chat_sources(note_id, learner_id, thread_id, message_id, lesson_id) VALUES ('fa111111-1111-4111-8111-111111111111', 'a1111111-1111-4111-8111-111111111111', 'f9111111-1111-4111-8111-111111111111', 'f9333333-3333-4333-8333-333333333333', NULL)`,
		`INSERT INTO quick_note_chat_sources(note_id, learner_id, unavailable) VALUES ('fa222222-2222-4222-8222-222222222222', 'a1111111-1111-4111-8111-111111111111', true)`,
		`INSERT INTO tutor_thread_deletion_tombstones(thread_id) VALUES ('fa333333-3333-4333-8333-333333333333')`,
	} {
		if _, err = f.DB.Pool.Exec(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, query := range []string{
		`INSERT INTO xp_events(learner_id,lesson_id,course_id,completion_id,amount,perfect,config_version,earned_at,local_date,time_zone)
 SELECT o.learner_id,o.lesson_id,o.course_id,o.id,c.base_xp,false,c.version,o.passed_at,(o.passed_at AT TIME ZONE 'Asia/Ho_Chi_Minh')::date,'Asia/Ho_Chi_Minh'
 FROM lesson_completion_outbox o CROSS JOIN gamification_config c WHERE o.id='faaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'`,
		`INSERT INTO learner_usernames(learner_id,username) VALUES('a1111111-1111-4111-8111-111111111111','Backup.Learner')`,
		`INSERT INTO learner_social_settings(learner_id,discoverable,request_policy,show_streak_to_friends,course_invites_from) VALUES('a1111111-1111-4111-8111-111111111111',false,'friends_of_friends',false,'nobody')`,
		`UPDATE learner_usernames SET username='Backup.Renamed', changed_at='2026-11-01T00:00:00Z' WHERE learner_id='a1111111-1111-4111-8111-111111111111'`,
		`INSERT INTO social_rate_limits(learner_id,bucket,count) VALUES('a1111111-1111-4111-8111-111111111111','search',3)`,
		`INSERT INTO username_holds(canonical,previous_owner_ref,held_until) VALUES('released.name',repeat('a',64),now()+interval '30 days')`,
		`INSERT INTO learners(id,display_name) VALUES('a2222222-2222-4222-8222-222222222222','Backup Friend')`,
		`INSERT INTO learner_usernames(learner_id,username) VALUES('a2222222-2222-4222-8222-222222222222','Backup.Friend')`,
		`INSERT INTO friend_requests(sender_id,recipient_id,status,method,decided_at) VALUES('a2222222-2222-4222-8222-222222222222','a1111111-1111-4111-8111-111111111111','accepted','invite_link','2026-11-02T00:00:00Z'),('a1111111-1111-4111-8111-111111111111','a2222222-2222-4222-8222-222222222222','declined','username','2026-11-03T00:00:00Z')`,
		`INSERT INTO friendships(learner_low,learner_high) VALUES('a1111111-1111-4111-8111-111111111111','a2222222-2222-4222-8222-222222222222')`,
		`INSERT INTO friend_streaks(learner_low,learner_high,current_days,longest_days,run_no,cursor_low,cursor_high,run_start_low,run_start_high,updated_at) VALUES('a1111111-1111-4111-8111-111111111111','a2222222-2222-4222-8222-222222222222',2,2,1,'2026-11-08','2026-11-07','2026-11-07','2026-11-06','2026-11-08T00:00:00Z')`,
		`INSERT INTO friend_streak_days(learner_low,learner_high,low_date,high_date,run_no,outcome,length,created_at) VALUES('a1111111-1111-4111-8111-111111111111','a2222222-2222-4222-8222-222222222222','2026-11-07','2026-11-06',1,'extended',1,'2026-11-07T00:00:00Z'),('a1111111-1111-4111-8111-111111111111','a2222222-2222-4222-8222-222222222222','2026-11-08','2026-11-07',1,'extended',2,'2026-11-08T00:00:00Z')`,
		`INSERT INTO friend_streak_milestones(learner_low,learner_high,run_no,milestone_days,reached_low,reached_high,created_at) VALUES('a1111111-1111-4111-8111-111111111111','a2222222-2222-4222-8222-222222222222',1,2,'2026-11-08','2026-11-07','2026-11-08T00:00:00Z')`,
		`INSERT INTO friend_nudges(id,sender_id,recipient_id,operation_key,sender_local_date,delivered,read_at,created_at) VALUES('f6700000-0000-4000-8000-000000000001','a2222222-2222-4222-8222-222222222222','a1111111-1111-4111-8111-111111111111','f6700000-0000-4000-8000-0000000000a1','2026-11-08',true,NULL,'2026-11-08T09:00:00Z')`,
		`INSERT INTO friend_links(owner_id,token_hash,expires_at) VALUES('a1111111-1111-4111-8111-111111111111',repeat('b',64),now()+interval '30 days')`,
		`INSERT INTO learners(id,display_name) VALUES('a3333333-3333-4333-8333-333333333333','Backup Blocked')`,
		`INSERT INTO learner_blocks(blocker_id,blocked_id,created_at) VALUES('a1111111-1111-4111-8111-111111111111','a3333333-3333-4333-8333-333333333333','2026-11-04T00:00:00Z')`,
		`INSERT INTO publication_drafts(id,course_id,owner_id,number,content_hash,snapshot,lesson_count,verdict,reason_codes,sampled_lessons,screened_at,created_at) VALUES('e1500000-1111-4111-8111-111111111111','f1111111-1111-4111-8111-111111111111','a1111111-1111-4111-8111-111111111111',1,'` + strings.Repeat("ab", 32) + `','{"title":"Synthetic sky","units":[]}'::jsonb,1,'needs_review','{personal_data}',1,'2026-11-05T00:00:00Z','2026-11-05T00:00:00Z')`,
		`INSERT INTO publication_screenings(draft_id,owner_id,verdict,reason_codes,policy_version,attempts,cost_nanos,created_at) VALUES('e1500000-1111-4111-8111-111111111111','a1111111-1111-4111-8111-111111111111','needs_review','{personal_data}','course-v1',1,1234,now())`,
		`INSERT INTO publication_links(id,course_id,token_hash,created_at,revoked_at) VALUES('e1502000-1111-4111-8111-111111111111','f1111111-1111-4111-8111-111111111111',encode(sha256(convert_to('fixture-revoked-token','UTF8')),'hex'),'2026-11-06T00:00:00Z','2026-11-07T00:00:00Z')`,
		`INSERT INTO courses(id,learner_id,title) VALUES('e1503000-1111-4111-8111-111111111111','a2222222-2222-4222-8222-222222222222','Pinned copy')`,
		`INSERT INTO course_copies(copy_course_id,learner_id,source_course_id,draft_id,version,content_hash) VALUES('e1503000-1111-4111-8111-111111111111','a2222222-2222-4222-8222-222222222222','f1111111-1111-4111-8111-111111111111','e1500000-1111-4111-8111-111111111111',1,'` + strings.Repeat("ab", 32) + `')`,
		`INSERT INTO daily_goal_achievements(learner_id,local_date,goal_xp) SELECT learner_id,local_date,amount FROM xp_events WHERE completion_id='faaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'`,
		`INSERT INTO streak_days(learner_id,local_date,first_event_id) SELECT learner_id,local_date,id FROM xp_events WHERE completion_id='faaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'`,
		`INSERT INTO streak_state(learner_id,current_days,longest_days,last_active_date,decided_through) SELECT learner_id,1,3,local_date,local_date FROM xp_events WHERE completion_id='faaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'`,
		`INSERT INTO streak_freeze_inventory(learner_id,held) SELECT learner_id,1 FROM xp_events WHERE completion_id='faaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'`,
		`INSERT INTO streak_freeze_milestones(learner_id,milestone_days,run_started_on,reached_on,granted) SELECT learner_id,7,local_date-9,local_date-3,true FROM xp_events WHERE completion_id='faaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'`,
		`INSERT INTO streak_day_protections(learner_id,protected_on,remaining,streak_length) SELECT learner_id,local_date-2,0,7 FROM xp_events WHERE completion_id='faaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'`,
		`INSERT INTO league_groups(id,week_start,tier,size) SELECT 'b5000000-0000-4000-8000-000000000001',date_trunc('week',local_date)::date,2,1 FROM xp_events WHERE completion_id='faaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'`,
		`INSERT INTO league_memberships(learner_id,week_start,group_id,tier,slot) SELECT learner_id,date_trunc('week',local_date)::date,'b5000000-0000-4000-8000-000000000001',2,1 FROM xp_events WHERE completion_id='faaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'`,
		`INSERT INTO streak_breaks(learner_id,broken_on,previous_length) SELECT learner_id,local_date-3,3 FROM xp_events WHERE completion_id='faaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'`,
		`INSERT INTO league_week_results(id,learner_id,week_start,group_id,tier_from,tier_to,final_rank,members,weekly_xp,outcome) SELECT 'b5000000-0000-4000-8000-000000000002',learner_id,date_trunc('week',local_date)::date,'b5000000-0000-4000-8000-000000000001',2,3,1,1,amount,'promoted' FROM xp_events WHERE completion_id='faaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'`,
	} {
		if _, err = f.DB.Pool.Exec(t.Context(), query); err != nil {
			t.Fatal(err)
		}
	}
	// E15-04: a Lesson generated inside a pinned copy owned by its own learner, with the consent record.
	const generator, genCopy, genStub = "e1504000-0000-4000-8000-000000000000", "e1504000-5555-4555-8555-555555555555", "e1504000-2222-4222-8222-222222222222"
	for _, stmt := range []string{
		`INSERT INTO learners(id,display_name) VALUES('` + generator + `','Backup Copy Generator')`,
		`INSERT INTO courses(id,learner_id,title) VALUES('` + genCopy + `','` + generator + `','Generated copy')`,
		`INSERT INTO course_copies(copy_course_id,learner_id,source_course_id,draft_id,version,content_hash) VALUES('` + genCopy + `','` + generator + `','f1111111-1111-4111-8111-111111111111','e1500000-1111-4111-8111-111111111111',1,'` + strings.Repeat("ab", 32) + `')`,
		`INSERT INTO course_units(id,learner_id,course_id,authored_id,position,title) VALUES('e1504000-1111-4111-8111-111111111111','` + generator + `','` + genCopy + `','u1',0,'Copy unit')`,
		`INSERT INTO course_lesson_stubs(id,learner_id,unit_id,authored_id,position,title,goal,status) VALUES('` + genStub + `','` + generator + `','e1504000-1111-4111-8111-111111111111','l3',0,'Generated in the copy','Goal','current')`,
		`INSERT INTO generated_lessons(id,lesson_stub_id,learner_id,course_id,title,manifest) VALUES('e1504000-3333-4333-8333-333333333333','` + genStub + `','` + generator + `','` + genCopy + `','Generated in the copy','{}'::jsonb)`,
	} {
		if _, err = f.DB.Pool.Exec(t.Context(), stmt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `WITH g AS (INSERT INTO lesson_generations(id,learner_id,lesson_id,course_id,quote_id,state) VALUES('e1504000-4444-4444-8444-444444444444','`+generator+`','`+genStub+`','`+genCopy+`',$1,'completed') RETURNING id) INSERT INTO copy_generation_consents(generation_id,learner_id,copy_course_id,lesson_id,quote_id,pricing_version) SELECT id,'`+generator+`','`+genCopy+`','`+genStub+`',$1,$2 FROM g`, outlineQuote.ID, outlineQuote.PricingVersion); err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO lesson_test_variant_snapshots (id, session_id, learner_id, lesson_stub_id, topic_id, question_id, variant_of, source, reason, prompt, choices, answer_index, explanation, instruction_snapshot) VALUES ('fccccccc-cccc-4ccc-8ccc-cccccccccccc', 'f8888888-8888-4888-8888-888888888888', 'a1111111-1111-4111-8111-111111111111', 'f3333333-3333-4333-8333-333333333333', 'top-moon', 'v1-top-moon', 'q-moon', 'pool', NULL, 'What causes moon phases?', '["Sun angle", "Earth shadow"]'::jsonb, 0, 'Angle of sunlight on moon', '{"topic":"moon"}'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `UPDATE learner_profiles SET active_course_id='f1111111-1111-4111-8111-111111111111' WHERE learner_id='a1111111-1111-4111-8111-111111111111'`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO eval_alert_events (scope, alert_key, actual_usd, budget_usd, properties, status) VALUES ('platform_daily_80', 'synthetic-key-1', 40.0, 50.0, '{"note":"fixture"}'::jsonb, 'firing')`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO eval_reports (task, dataset_revision, prompt_revision, model, score, threshold, passed, report_json) VALUES ('course_preview', 'rev-fixture', 'v1', 'claude-sonnet-5.5', 0.95, 0.85, true, '{"score":0.95}'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO eval_operational_aggregates (day, task, total_calls, fallback_calls, error_calls, total_latency_ms, input_tokens, output_tokens, cache_write_tokens, cache_read_tokens, cost_nanos, credit_revenue_cents) VALUES ('2026-11-10', 'course_preview', 10, 1, 0, 4500, 2000, 1000, 500, 1500, 30000000, 250)`); err != nil {
		t.Fatal(err)
	}

	if err := playerfixture.Publish(t.Context(), f.DB.Pool, f.ApplicationObjects(), "a1111111-1111-4111-8111-111111111111", "f3333333-3333-4333-8333-333333333333"); err != nil {
		t.Fatal(err)
	}
	f.BackupPlayerObjects(t)
	if _, err := f.DB.Pool.Exec(t.Context(), `INSERT INTO lesson_playback_positions(learner_id,lesson_id,version,slide_id,time_ms) VALUES('a1111111-1111-4111-8111-111111111111','f3333333-3333-4333-8333-333333333333',1,'sky-2',1800)`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.DB.Pool.Exec(t.Context(), `INSERT INTO lesson_playback_sync_events(action_key,learner_id,lesson_id,version,slide_id,time_ms,occurred_at) VALUES('act-11111111-1111-4111-8111-111111111111','a1111111-1111-4111-8111-111111111111','f3333333-3333-4333-8333-333333333333',1,'sky-2',1800,now())`); err != nil {
		t.Fatal(err)
	}
	catalogue, err := pricing.Load(t.Context(), f.DB.Queries)
	if err != nil {
		t.Fatal(err)
	}
	creditService := credits.NewServiceWithCatalogue(f.DB.Pool, nil, catalogue, nil)
	for _, action := range []string{"quick_note", "preview"} {
		input := pricing.QuoteInput{Action: action}
		if action == "preview" {
			if err := f.DB.Pool.QueryRow(t.Context(), "SELECT id FROM course_brief_revisions WHERE expires_at>now() LIMIT 1").Scan(&input.BriefRevision); err != nil {
				t.Fatal(err)
			}
		}
		q, e := pricing.NewQuotes(f.DB.Pool).Create(t.Context(), "a1111111-1111-4111-8111-111111111111", input)
		if e != nil {
			t.Fatal(e)
		}
		if e = pricing.NewQuotes(f.DB.Pool).Accept(t.Context(), "a1111111-1111-4111-8111-111111111111", q.ID); e != nil {
			t.Fatal(e)
		}
		job, e := creditService.Reserve(t.Context(), "a1111111-1111-4111-8111-111111111111", q.ID)
		if e != nil {
			t.Fatal(e)
		}
		if action == "quick_note" {
			if _, e = creditService.Settle(t.Context(), "a1111111-1111-4111-8111-111111111111", job.ID, []*enginev1.Usage{{Provider: q.Rate.Provider, Model: q.Rate.Model, InputTokens: 100, OutputTokens: 20, Component: "content", Attempt: 1, Delivered: true, ArtifactId: "synthetic-backup-artifact"}}); e != nil {
				t.Fatal(e)
			}
		}
	}
	f.paymentFixture = creditstest.NewStripe()
	if err := creditService.ConfigurePayments(credits.PaymentOptions{Mode: "test", Provider: f.paymentFixture, WebhookSecret: creditstest.WebhookSecret}); err != nil {
		t.Fatal(err)
	}
	pack := catalogue.Config().Packs[0]
	order, err := creditService.CreateCheckout(t.Context(), "a1111111-1111-4111-8111-111111111111", credits.CheckoutInput{RequestID: "a7777777-7777-4777-8777-777777777777", SKU: pack.SKU, PricingVersion: catalogue.Config().Version, Platform: "web", ReturnTo: "/credits"})
	if err != nil {
		t.Fatal(err)
	}
	f.paymentOrderID = order.ID
	f.replayPayment(t, creditService, "checkout.session.async_payment_succeeded")
	plan := catalogue.Config().Plans[0]
	// A prior refunded annual term must survive restore without future grants.
	revoked, err := creditService.CreateCheckout(t.Context(), "a1111111-1111-4111-8111-111111111111", credits.CheckoutInput{RequestID: "a9999999-9999-4999-8999-999999999999", SKU: plan.Yearly.SKU, PricingVersion: catalogue.Config().Version, Platform: "web", ReturnTo: "/credits"})
	if err != nil {
		t.Fatal(err)
	}
	oldStart := time.Date(time.Now().UTC().Year()-2, 1, 1, 12, 0, 0, 0, time.UTC)
	if err = f.paymentFixture.SetAnnualStart("cs_test_"+revoked.ID, oldStart); err != nil {
		t.Fatal(err)
	}
	f.subscriptionOrderID = revoked.ID
	f.replaySubscription(t, creditService, "invoice.paid")
	raw, signature, err := f.paymentFixture.Notification("cs_test_"+revoked.ID, "refunded", "invoice.updated")
	if err != nil {
		t.Fatal(err)
	}
	receipt := httptest.NewRequestWithContext(t.Context(), "POST", "/v1/payments/stripe/webhook", bytes.NewReader(raw))
	receipt.Header.Set("Stripe-Signature", signature)
	response := httptest.NewRecorder()
	creditService.ServeHTTP(response, receipt)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	if err = creditService.ProcessPayments(t.Context()); err != nil {
		t.Fatal(err)
	}
	annual, err := creditService.CreateCheckout(t.Context(), "a1111111-1111-4111-8111-111111111111", credits.CheckoutInput{RequestID: "a8888888-8888-4888-8888-888888888888", SKU: plan.Yearly.SKU, PricingVersion: catalogue.Config().Version, Platform: "web", ReturnTo: "/credits"})
	if err != nil {
		t.Fatal(err)
	}
	f.subscriptionOrderID = annual.ID
	f.replaySubscription(t, creditService, "invoice.paid")
	raw, signature, err = f.paymentFixture.Notification("cs_test_"+annual.ID, "cancel_scheduled", "customer.subscription.updated")
	if err != nil {
		t.Fatal(err)
	}
	receipt = httptest.NewRequestWithContext(t.Context(), "POST", "/v1/payments/stripe/webhook", bytes.NewReader(raw))
	receipt.Header.Set("Stripe-Signature", signature)
	response = httptest.NewRecorder()
	creditService.ServeHTTP(response, receipt)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	if err = creditService.ProcessPayments(t.Context()); err != nil {
		t.Fatal(err)
	}
	for i, kind := range []string{"resume", "cancel"} {
		_, err = creditService.ChangeSubscription(t.Context(), "a1111111-1111-4111-8111-111111111111", credits.ChangeSubscriptionInput{RequestID: fmt.Sprintf("b9999999-9999-4999-8999-%012d", i), Kind: kind})
		if err != nil {
			t.Fatal(err)
		}
	}
	// Drain derived delivery markers before taking snapshots; the packaged worker
	// also dispatches outbox work and must not race the full-row restore comparison.
	if err = creditService.DispatchOutbox(t.Context()); err != nil {
		t.Fatal(err)
	}

}

func (f *Fixture) replaySubscription(t *testing.T, service *credits.Service, kind string) {
	t.Helper()
	pack := f.paymentOrderID
	f.paymentOrderID = f.subscriptionOrderID
	defer func() { f.paymentOrderID = pack }()
	f.replayPayment(t, service, kind)
}

func (f *Fixture) replayPayment(t *testing.T, service *credits.Service, kind string) {
	t.Helper()
	raw, signature, err := f.paymentFixture.Notification("cs_test_"+f.paymentOrderID, "paid", kind)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/v1/payments/stripe/webhook", bytes.NewReader(raw))
	req.Header.Set("Stripe-Signature", signature)
	w := httptest.NewRecorder()
	service.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("signed restore receipt %d %s", w.Code, w.Body.String())
	}
	if err = service.ProcessPayments(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// RunJob waits for the real River worker, including failed attempts and retries.
func (f *Fixture) RunJob(t *testing.T, date string) {
	t.Helper()
	inserted, err := f.queue.Insert(t.Context(), backup.Args{Date: date}, nil)
	if err != nil {
		t.Fatal(err)
	}
	timeout := time.NewTimer(40 * time.Second)
	defer timeout.Stop()
	poll := time.NewTicker(50 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-poll.C:
			job, err := f.queue.JobGet(t.Context(), inserted.Job.ID)
			if err != nil {
				t.Fatal(err)
			}
			if job.State == "completed" {
				f.lastDate = date
				// Simulate queue cleanup between deliveries, proving idempotency
				// comes from the stored object as well as River uniqueness.
				if _, err := f.queue.JobDelete(t.Context(), job.ID); err != nil {
					t.Fatal(err)
				}
				return
			}
			if job.State == "discarded" {
				t.Fatalf("backup exhausted retries: %+v", job.Errors)
			}
		case <-timeout.C:
			t.Fatal("backup worker did not complete")
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		}
	}
}

// ArchiveKey names the fixture's independently observed applied schema version.
func (f *Fixture) ArchiveKey(date string) string {
	return fmt.Sprintf("%s/schema-%d.dump.age", date, f.schemaVersion)
}

// Ciphertext downloads only from this fixture's isolated backup bucket.
func (f *Fixture) Ciphertext(t *testing.T, key string) []byte {
	t.Helper()
	response, err := f.root.Client.GetObject(t.Context(), &s3.GetObjectInput{Bucket: aws.String(f.root.Bucket), Key: aws.String(key)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("Synthetic Learner")) || !bytes.HasPrefix(data, []byte("age-encryption.org/v1")) {
		t.Fatal("object is not encrypted")
	}
	return data
}

// Restore decrypts synthetic bytes in memory and recovers a separate database.
func (f *Fixture) Restore(t *testing.T, ciphertext []byte) *store.Store {
	t.Helper()
	reader, err := age.Decrypt(bytes.NewReader(ciphertext), f.identity)
	if err != nil {
		t.Fatal(err)
	}
	dump, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(dump, []byte("PGDMP")) {
		t.Fatal("not a custom PostgreSQL archive")
	}
	if err := f.container.CopyToContainer(t.Context(), dump, "/tmp/synthetic.dump", 0o600); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range [][]string{{"createdb", "-U", "duolify", "restored"}, {"pg_restore", "--exit-on-error", "--no-owner", "--no-acl", "-U", "duolify", "-d", "restored", "/tmp/synthetic.dump"}, {"rm", "/tmp/synthetic.dump"}} {
		code, r, err := f.container.Exec(t.Context(), cmd, containerexec.Multiplexed())
		if err != nil {
			t.Fatal(err)
		}
		output, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		if code != 0 {
			t.Fatalf("restore command failed: %s", output)
		}
	}
	targetURL, err := url.Parse(f.databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	targetURL.Path = "/restored"
	db, err := store.New(t.Context(), targetURL.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

// AssertRestored verifies schema versions, exact data, balances and immutability.
func (f *Fixture) AssertRestored(t *testing.T, restored *store.Store) {
	t.Helper()
	f.AssertPlayerObjectsRestored(t)
	f.AssertPlaybackPositionRestored(t, restored.Pool)
	var version, learners, entries, balance, riverVersions int64
	var name string
	if err := restored.Pool.QueryRow(t.Context(), "SELECT max(version_id) FROM goose_db_version WHERE is_applied").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := restored.Pool.QueryRow(t.Context(), "SELECT count(*) FROM river_migration").Scan(&riverVersions); err != nil {
		t.Fatal(err)
	}
	if err := restored.Pool.QueryRow(t.Context(), "SELECT count(*) FROM learners").Scan(&learners); err != nil {
		t.Fatal(err)
	}
	if err := restored.Pool.QueryRow(t.Context(), "SELECT display_name FROM learners WHERE id='a1111111-1111-4111-8111-111111111111'").Scan(&name); err != nil {
		t.Fatal(err)
	}
	if err := restored.Pool.QueryRow(t.Context(), "SELECT count(*),sum(amount) FROM credit_entries WHERE learner_id='a1111111-1111-4111-8111-111111111111'").Scan(&entries, &balance); err != nil {
		t.Fatal(err)
	}
	var packCredits int64
	if err := restored.Pool.QueryRow(t.Context(), "SELECT credits FROM credit_checkout_orders WHERE id=$1", f.paymentOrderID).Scan(&packCredits); err != nil {
		t.Fatal(err)
	}
	var monthlyCredits int64
	if err := restored.Pool.QueryRow(t.Context(), "SELECT monthly_credits FROM credit_subscription_terms LIMIT 1").Scan(&monthlyCredits); err != nil {
		t.Fatal(err)
	}
	// Four learners: the synthetic learner, the E6-03 friend, the E6-04 blocked row and the E15-04 copy generator.
	if version != f.schemaVersion || riverVersions < 1 || learners != 4 || entries != 10 || balance != 104+packCredits+2*monthlyCredits || name != "Synthetic Learner" {
		t.Fatalf("invalid restored schema/learner/ledger: %d %d %d %d %d %s", version, riverVersions, learners, entries, balance, name)
	}
	var pricingVersion string
	var provenance map[string]any
	if err := restored.Pool.QueryRow(t.Context(), "SELECT pricing_version, provenance FROM credit_entries WHERE reference='synthetic-purchase'").Scan(&pricingVersion, &provenance); err != nil {
		t.Fatal(err)
	}
	if pricingVersion != "2026-11-09.approved.v1" || provenance["channel"] != "web" {
		t.Fatalf("invalid restored pricing_version/provenance: %s %v", pricingVersion, provenance)
	}
	var expiredBriefs int
	if err := restored.Pool.QueryRow(t.Context(), "SELECT count(*) FROM course_brief_revisions WHERE expires_at<=now()").Scan(&expiredBriefs); err != nil || expiredBriefs != 0 {
		t.Fatalf("expired briefs entered backup: %d %v", expiredBriefs, err)
	}
	beforePricing, err := pricing.Load(t.Context(), f.DB.Queries)
	if err != nil {
		t.Fatal(err)
	}
	afterPricing, err := pricing.Load(t.Context(), restored.Queries)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforePricing.Config(), afterPricing.Config()) {
		t.Fatal("encrypted backup changed the restored pricing catalogue")
	}
	for _, check := range []struct {
		query string
		want  int
	}{
		{"SELECT count(*) FROM pricing_audit WHERE request_id='restore-fixture' AND override_reason='synthetic audited override'", 1},
		{"SELECT count(*) FROM pricing_activation_events WHERE version='usd-launch-2026-11-08'", 1},
		{"SELECT count(*) FROM pricing_version_cancellations WHERE version='usd-backup-scheduled'", 1},
		{"SELECT count(*) FROM pricing_drafts WHERE version='usd-backup-rollback' AND parent_version='usd-launch-2026-11-08' AND rollback_source_version='usd-launch-2026-11-08'", 1},
		{"SELECT count(*) FROM pricing_verification_evidence WHERE version='usd-launch-2026-11-08'", 1},
		{"SELECT count(*) FROM pricing_verification_evidence WHERE version='usd-backup-scheduled'", 1},
	} {
		var got int
		if err := restored.Pool.QueryRow(t.Context(), check.query).Scan(&got); err != nil || got != check.want {
			t.Fatalf("restored pricing history count=%d want=%d err=%v", got, check.want, err)
		}
	}
	// Compare complete auth rows, including hashes, expiry/consumption, age gate and
	// analytics preference. Table names are a fixed fixture allowlist.
	for _, table := range []string{"league_groups", "league_memberships", "league_week_results", "quick_notes", "tutor_threads", "tutor_messages", "tutor_turns", "quick_note_enrichments", "quick_note_chat_sources", "tutor_thread_deletion_tombstones", "bookmarks", "character_create_jobs", "characters", "character_versions", "character_screenings", "course_casts", "lesson_cast_snapshots", "lesson_manifests", "course_creations", "courses", "course_units", "course_lesson_stubs", "course_enrollments", "lesson_generations", "generated_lessons", "lesson_slides", "lesson_questions", "lesson_retry_pools", "lesson_test_sessions", "lesson_test_answers", "lesson_test_answer_operations", "lesson_completion_outbox", "lesson_completion_summaries", "lesson_test_variant_snapshots", "outline_proposals", "outline_drafts", "credit_subscription_lifecycle", "credit_subscription_transitions", "credit_subscription_schedule", "credit_subscription_revocations", "credit_subscription_cancellations", "credit_subscriptions", "credit_subscription_terms", "credit_payment_sessions", "credit_payment_inbox", "credit_payment_payloads", "credit_payment_results", "credit_checkout_orders", "credit_jobs", "credit_allocations", "credit_usage", "credit_cost_counters", "credit_artifacts", "credit_outbox", "credit_timezones", "course_brief_revisions", "pricing_quote_policies", "pricing_quotes", "pricing_quote_consents", "pricing_admin_grants", "pricing_drafts", "pricing_audit", "pricing_activation_events", "pricing_version_cancellations", "pricing_verification_evidence", "eval_alert_configs", "eval_alert_events", "eval_reports", "eval_operational_aggregates", "learner_profiles", "auth_identities", "auth_codes", "auth_sessions", "auth_refresh_tokens", "auth_rate_limits", "auth_provider_identities", "auth_oauth_attempts", "account_deletion_tombstones", "gamification_config", "xp_events", "daily_goal_achievements", "username_config", "learner_usernames", "learner_social_settings", "username_holds", "social_rate_limits", "friend_requests", "friendships", "friend_links", "friend_streaks", "friend_streak_days", "friend_streak_milestones", "friend_nudges", "streak_state", "streak_days", "streak_breaks", "streak_freeze_inventory", "streak_freeze_milestones", "streak_day_protections", "publication_drafts", "publication_screenings", "publication_links", "course_copies"} {
		query := "SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text FROM " + table + " t"
		var before, after string
		if err := f.DB.Pool.QueryRow(t.Context(), query).Scan(&before); err != nil {
			t.Fatal(err)
		}
		if err := restored.Pool.QueryRow(t.Context(), query).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if before != after || after == "[]" {
			t.Fatalf("encrypted backup changed or omitted %s", table)
		}
	}
	paidService := credits.NewServiceWithCatalogue(restored.Pool, nil, afterPricing, nil)
	// Restored E4 outbox work cannot award again or duplicate an earned goal.
	if err := gamification.New(restored.Pool, nil).Consume(t.Context(), "a1111111-1111-4111-8111-111111111111"); err != nil {
		t.Fatal(err)
	}
	var xpCount, goalCount, xpTotal, configuredBase int
	if err := restored.Pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM xp_events),(SELECT count(*) FROM daily_goal_achievements),(SELECT sum(amount) FROM xp_events),base_xp FROM gamification_config`).Scan(&xpCount, &goalCount, &xpTotal, &configuredBase); err != nil || xpCount != 1 || goalCount != 1 || xpTotal != configuredBase {
		t.Fatalf("restored XP replay changed totals/markers: %d %d %d %d %v", xpCount, goalCount, xpTotal, configuredBase, err)
	}
	// The restored streak projection is stable under the rollover worker and replay.
	if err := gamification.New(restored.Pool, nil).Rollover(t.Context()); err != nil {
		t.Fatal(err)
	}
	var streakDays, streakBreaks, streakLongest int
	if err := restored.Pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM streak_days),(SELECT count(*) FROM streak_breaks),(SELECT longest_days FROM streak_state)`).Scan(&streakDays, &streakBreaks, &streakLongest); err != nil || streakDays != 1 || streakBreaks < 1 || streakLongest < 3 {
		t.Fatalf("restored streak projection changed: %d %d %d %v", streakDays, streakBreaks, streakLongest, err)
	}
	// Freeze rows and inventory survive restore unchanged.
	var freezeMilestones, freezeProtections, freezeHeld int
	if err := restored.Pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM streak_freeze_milestones),(SELECT count(*) FROM streak_day_protections),(SELECT held FROM streak_freeze_inventory)`).Scan(&freezeMilestones, &freezeProtections, &freezeHeld); err != nil || freezeMilestones != 1 || freezeProtections != 1 || freezeHeld != 1 {
		t.Fatalf("restored freeze state changed: %d %d %d %v", freezeMilestones, freezeProtections, freezeHeld, err)
	}
	// League membership restores; the weekly XP it ranks is recomputed from restored xp_events, never stored.
	var leagueMembers, leagueXP int
	if err := restored.Pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM league_memberships),(SELECT COALESCE(sum(x.amount),0) FROM league_memberships m JOIN xp_events x ON x.learner_id=m.learner_id AND x.local_date BETWEEN m.week_start AND m.week_start+6)`).Scan(&leagueMembers, &leagueXP); err != nil || leagueMembers != 1 || leagueXP <= 0 {
		t.Fatalf("restored league state changed: %d %d %v", leagueMembers, leagueXP, err)
	}
	// The frozen weekly result restores and still matches the XP recomputed from restored xp_events; nothing grants Credits.
	var resultMatches int
	if err := restored.Pool.QueryRow(t.Context(), `SELECT count(*) FROM league_week_results r WHERE r.outcome='promoted' AND r.weekly_xp=(SELECT COALESCE(sum(x.amount),0) FROM xp_events x WHERE x.learner_id=r.learner_id AND x.local_date BETWEEN r.week_start AND r.week_start+6)`).Scan(&resultMatches); err != nil || resultMatches != 1 {
		t.Fatalf("restored league result changed: %d %v", resultMatches, err)
	}
	// Restored usernames stay unique and an active hold still blocks other learners.
	// The rename wrote a hold for the previous name; privacy settings and the
	// rename timestamp (cooldown anchor) survive the dump.
	var claimedName, policy string
	var holds int
	var discoverable bool
	var renamedOn string
	if err := restored.Pool.QueryRow(t.Context(), `SELECT (SELECT username FROM learner_usernames WHERE learner_id='a1111111-1111-4111-8111-111111111111'),(SELECT count(*) FROM username_holds WHERE held_until>now()),(SELECT discoverable FROM learner_social_settings),(SELECT request_policy FROM learner_social_settings),(SELECT to_char(changed_at AT TIME ZONE 'UTC','YYYY-MM-DD') FROM learner_usernames WHERE learner_id='a1111111-1111-4111-8111-111111111111')`).Scan(&claimedName, &holds, &discoverable, &policy, &renamedOn); err != nil || claimedName != "Backup.Renamed" || holds != 2 || discoverable || policy != "friends_of_friends" || renamedOn != "2026-11-01" {
		t.Fatalf("restored usernames, holds or privacy changed: %q %d %v %q %q %v", claimedName, holds, discoverable, policy, renamedOn, err)
	}
	var previousHeld int
	if err := restored.Pool.QueryRow(t.Context(), `SELECT count(*) FROM username_holds WHERE canonical='backup.learner' AND previous_owner_ref=encode(sha256(convert_to('a1111111-1111-4111-8111-111111111111','UTF8')),'hex')`).Scan(&previousHeld); err != nil || previousHeld != 1 {
		t.Fatalf("restored rename hold lost its previous-owner hash: %d %v", previousHeld, err)
	}
	if _, err := restored.Pool.Exec(t.Context(), `WITH l AS (INSERT INTO learners(display_name) VALUES('duplicate username probe') RETURNING id) INSERT INTO learner_usernames(learner_id,username) SELECT id,'BACKUP.renamed' FROM l`); err == nil {
		t.Fatal("restored canonical unique index allowed a duplicate username")
	}
	// Friendships keep their canonical pair, request states (a decline keeps its cooldown anchor) and
	// only the link's token hash; the pair and pending-pair uniqueness survive the restore.
	var friendships, accepted, declined, links int
	if err := restored.Pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM friendships WHERE learner_low<learner_high),(SELECT count(*) FROM friend_requests WHERE status='accepted' AND method='invite_link'),(SELECT count(*) FROM friend_requests WHERE status='declined' AND decided_at='2026-11-03T00:00:00Z'),(SELECT count(*) FROM friend_links WHERE token_hash=repeat('b',64) AND revoked_at IS NULL)`).Scan(&friendships, &accepted, &declined, &links); err != nil || friendships != 1 || accepted != 1 || declined != 1 || links != 1 {
		t.Fatalf("restored friend data changed: %d %d %d %d %v", friendships, accepted, declined, links, err)
	}
	if _, err := restored.Pool.Exec(t.Context(), `INSERT INTO friendships(learner_low,learner_high) VALUES('a2222222-2222-4222-8222-222222222222','a1111111-1111-4111-8111-111111111111')`); err == nil {
		t.Fatal("restored friendship check allowed a reversed pair")
	}
	// Friend streak pair state, its idempotent pair-day keys and milestone keys survive the restore.
	var pairStreaks, pairDays, pairMilestones int
	if err := restored.Pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM friend_streaks WHERE current_days=2 AND longest_days=2 AND cursor_low='2026-11-08'),(SELECT count(*) FROM friend_streak_days WHERE outcome='extended'),(SELECT count(*) FROM friend_streak_milestones WHERE milestone_days=2)`).Scan(&pairStreaks, &pairDays, &pairMilestones); err != nil || pairStreaks != 1 || pairDays != 2 || pairMilestones != 1 {
		t.Fatalf("restored friend streak data changed: %d %d %d %v", pairStreaks, pairDays, pairMilestones, err)
	}
	if _, err := restored.Pool.Exec(t.Context(), `INSERT INTO friend_streak_days(learner_low,learner_high,low_date,high_date,run_no,outcome,length) VALUES('a1111111-1111-4111-8111-111111111111','a2222222-2222-4222-8222-222222222222','2026-11-07','2026-11-06',1,'extended',9)`); err == nil {
		t.Fatal("restored pair-day key allowed a second settlement of the same shared day")
	}
	// Nudges (inbox state) survive the restore with their operation key and pair-day key.
	var nudges int
	if err := restored.Pool.QueryRow(t.Context(), `SELECT count(*) FROM friend_nudges WHERE id='f6700000-0000-4000-8000-000000000001' AND delivered AND read_at IS NULL AND sender_local_date='2026-11-08' AND created_at='2026-11-08T09:00:00Z'`).Scan(&nudges); err != nil || nudges != 1 {
		t.Fatalf("restored nudge data changed: %d %v", nudges, err)
	}
	if _, err := restored.Pool.Exec(t.Context(), `INSERT INTO friend_nudges(sender_id,recipient_id,operation_key,sender_local_date) VALUES('a2222222-2222-4222-8222-222222222222','a1111111-1111-4111-8111-111111111111',gen_random_uuid(),'2026-11-08')`); err == nil {
		t.Fatal("restored pair-day key allowed a second nudge on the same sender day")
	}
	if _, err := restored.Pool.Exec(t.Context(), `INSERT INTO friend_nudges(sender_id,recipient_id,operation_key,sender_local_date) VALUES('a2222222-2222-4222-8222-222222222222','a1111111-1111-4111-8111-111111111111','f6700000-0000-4000-8000-0000000000a1','2026-11-09')`); err == nil {
		t.Fatal("restored operation key allowed a replay to create a second nudge")
	}
	// Blocks keep their direction and date; the self-block check and the invite setting survive.
	var blocks, inviteSetting int
	if err := restored.Pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM learner_blocks WHERE blocker_id='a1111111-1111-4111-8111-111111111111' AND blocked_id='a3333333-3333-4333-8333-333333333333' AND created_at='2026-11-04T00:00:00Z'),(SELECT count(*) FROM learner_social_settings WHERE learner_id='a1111111-1111-4111-8111-111111111111' AND course_invites_from='nobody')`).Scan(&blocks, &inviteSetting); err != nil || blocks != 1 || inviteSetting != 1 {
		t.Fatalf("restored block data changed: %d %d %v", blocks, inviteSetting, err)
	}
	if _, err := restored.Pool.Exec(t.Context(), `INSERT INTO learner_blocks(blocker_id,blocked_id) VALUES('a1111111-1111-4111-8111-111111111111','a1111111-1111-4111-8111-111111111111')`); err == nil {
		t.Fatal("restored block check allowed a self block")
	}
	// Publication drafts keep number, hash and verdict, and stay immutable after restore.
	var drafts int
	if err := restored.Pool.QueryRow(t.Context(), `SELECT count(*) FROM publication_drafts WHERE id='e1500000-1111-4111-8111-111111111111' AND number=1 AND verdict='needs_review' AND content_hash=$1 AND (SELECT count(*) FROM publication_screenings WHERE draft_id=publication_drafts.id)=1`, strings.Repeat("ab", 32)).Scan(&drafts); err != nil || drafts != 1 {
		t.Fatalf("restored publication draft changed: %d %v", drafts, err)
	}
	if _, err := restored.Pool.Exec(t.Context(), `UPDATE publication_drafts SET snapshot='{}'::jsonb WHERE id='e1500000-1111-4111-8111-111111111111'`); err == nil {
		t.Fatal("restored publication draft snapshot is mutable")
	}
	// A pinned course copy restores with its pin to the source course and published version.
	var pins int
	if err := restored.Pool.QueryRow(t.Context(), `SELECT count(*) FROM course_copies WHERE copy_course_id='e1503000-1111-4111-8111-111111111111' AND source_course_id='f1111111-1111-4111-8111-111111111111' AND draft_id='e1500000-1111-4111-8111-111111111111' AND version=1 AND length(content_hash)=64`).Scan(&pins); err != nil || pins != 1 {
		t.Fatalf("restored course copy pin changed: %d %v", pins, err)
	}
	// Copy-local generated Lesson and its consent restore with the copy; the published hash is unchanged.
	var local int
	if err := restored.Pool.QueryRow(t.Context(), `SELECT count(*) FROM generated_lessons g JOIN copy_generation_consents c ON c.lesson_id=g.lesson_stub_id AND c.copy_course_id=g.course_id JOIN course_copies cc ON cc.copy_course_id=g.course_id WHERE g.id='e1504000-3333-4333-8333-333333333333' AND c.generation_id='e1504000-4444-4444-8444-444444444444' AND c.pricing_version<>'' AND cc.content_hash=$1`, strings.Repeat("ab", 32)).Scan(&local); err != nil || local != 1 {
		t.Fatalf("restored copy-local lesson or consent changed: %d %v", local, err)
	}
	// Share links restore as hashes only: visibility column present, revoked link still revoked, no plaintext.
	var shareLinks int
	if err := restored.Pool.QueryRow(t.Context(), `SELECT count(*) FROM publication_links WHERE id='e1502000-1111-4111-8111-111111111111' AND token_hash=encode(sha256(convert_to('fixture-revoked-token','UTF8')),'hex') AND revoked_at IS NOT NULL AND (SELECT visibility::text FROM courses WHERE id=publication_links.course_id)='private'`).Scan(&shareLinks); err != nil || shareLinks != 1 {
		t.Fatalf("restored share link changed: %d %v", shareLinks, err)
	}
	confirmed, err := courses.New(courses.Options{Pool: restored.Pool}).ResolveConfirmed(t.Context(), "a1111111-1111-4111-8111-111111111111", "c7222222-2222-4222-8222-222222222222", 3)
	if err != nil || confirmed.Outline.Hash() != confirmed.ContentHash || confirmed.Outline[0].Lessons[0].ID != "fixture-lesson" {
		t.Fatalf("restored confirmed outline unavailable: %+v %v", confirmed, err)
	}
	if err := paidService.ConfigurePayments(credits.PaymentOptions{Mode: "test", Provider: f.paymentFixture, WebhookSecret: creditstest.WebhookSecret}); err != nil {
		t.Fatal(err)
	}
	f.replayPayment(t, paidService, "checkout.session.async_payment_succeeded")
	f.replayPayment(t, paidService, "checkout.session.completed")
	f.replaySubscription(t, paidService, "invoice.payment_succeeded")
	f.replaySubscription(t, paidService, "checkout.session.completed")
	var grants int
	if err := restored.Pool.QueryRow(t.Context(), "SELECT count(*) FROM credit_subscription_terms").Scan(&grants); err != nil || grants != 2 {
		t.Fatalf("restored funded months %d %v", grants, err)
	}
	if err := restored.Pool.QueryRow(t.Context(), "SELECT count(*) FROM credit_entries WHERE kind='subscription_grant'").Scan(&grants); err != nil || grants != 2 {
		t.Fatalf("restored grants %d %v", grants, err)
	}
	plan, err := paidService.GetWallet(t.Context(), "a1111111-1111-4111-8111-111111111111")
	if err != nil || plan.Plan == nil || !plan.Plan.Active || plan.Subscription != 2*monthlyCredits {
		t.Fatalf("restored entitlement %+v %v", plan, err)
	}
	// Catch up part of the funded annual schedule after restore; revoked history
	// cannot issue any more. Replay and identity deletion must not add credits.
	var asOf time.Time
	if err := restored.Pool.QueryRow(t.Context(), `SELECT t.service_start + interval '3 months' FROM credit_subscription_terms t JOIN credit_subscriptions b USING(subscription_id) WHERE b.order_id=$1`, f.subscriptionOrderID).Scan(&asOf); err != nil {
		t.Fatal(err)
	}
	if count, err := paidService.IssueAnnualAllowances(t.Context(), asOf); err != nil || count != 3 {
		t.Fatalf("restored annual catch-up %d %v", count, err)
	}
	if count, err := paidService.IssueAnnualAllowances(t.Context(), asOf); err != nil || count != 0 {
		t.Fatalf("restored annual replay %d %v", count, err)
	}
	var purchases, outbox int
	if err := restored.Pool.QueryRow(t.Context(), "SELECT count(*) FROM credit_payment_results WHERE state='paid'").Scan(&purchases); err != nil || purchases != 1 {
		t.Fatalf("restore duplicated payment: %d %v", purchases, err)
	}
	if err := restored.Pool.QueryRow(t.Context(), "SELECT count(*) FROM credit_outbox WHERE event='credits_purchased'").Scan(&outbox); err != nil || outbox != 1 {
		t.Fatalf("restore duplicated payment analytics: %d %v", outbox, err)
	}
	for _, table := range []string{"credit_subscription_lifecycle", "credit_subscription_transitions", "credit_subscription_schedule", "credit_subscription_revocations", "credit_subscription_cancellations", "credit_subscriptions", "credit_subscription_terms", "credit_payment_sessions", "credit_payment_inbox", "credit_payment_results"} {
		for _, verb := range []string{"DELETE FROM ", "TRUNCATE "} {
			if _, err := restored.Pool.Exec(t.Context(), verb+table); err == nil {
				t.Fatalf("mutable restored payment history %s%s", verb, table)
			}
		}
	}
	// Admin deletion removes grants and pseudonymizes retained draft/audit actors.
	for _, query := range []string{"UPDATE credit_checkout_orders SET amount_minor=1", "DELETE FROM credit_checkout_orders", "TRUNCATE credit_checkout_orders"} {
		if _, err := restored.Pool.Exec(t.Context(), query); err == nil {
			t.Fatalf("restored order mutation permitted: %s", query)
		}
	}
	const leavingAdmin = "e5555555-5555-4555-8555-555555555555"
	for _, query := range []string{
		`INSERT INTO learners(id,display_name) VALUES($1::uuid,'Leaving pricing admin')`,
		`INSERT INTO pricing_admin_grants(account_id) VALUES($1::uuid)`,
		`INSERT INTO pricing_drafts(version,parent_version,config,created_by,change_class) SELECT 'usd-deleted-actor-fixture',version,jsonb_set(jsonb_set(config,'{version}','"usd-deleted-actor-fixture"'),'{effective_from}',to_jsonb(now()+interval '96 hours')),$1,'non_adverse' FROM pricing_versions WHERE version='usd-launch-2026-11-08'`,
		`INSERT INTO pricing_audit(actor_id,action,version,change_class,reason,request_id) VALUES(encode(sha256(convert_to($1,'UTF8')),'hex'),'version_created','usd-deleted-actor-fixture','non_adverse','deletion fixture','deletion-fixture')`,
	} {
		if _, err := restored.Pool.Exec(t.Context(), query, leavingAdmin); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := restored.Pool.Exec(t.Context(), "DELETE FROM learners WHERE id=$1::uuid", leavingAdmin); err != nil {
		t.Fatal(err)
	}
	var pseudonym string
	if err := restored.Pool.QueryRow(t.Context(), "SELECT actor_id FROM pricing_audit WHERE request_id='deletion-fixture'").Scan(&pseudonym); err != nil || len(pseudonym) != 64 {
		t.Fatalf("audit actor was not pseudonymized: %q %v", pseudonym, err)
	}
	var draftActor string
	if err := restored.Pool.QueryRow(t.Context(), "SELECT created_by FROM pricing_drafts WHERE version='usd-deleted-actor-fixture'").Scan(&draftActor); err != nil || !strings.HasPrefix(draftActor, "deleted:") {
		t.Fatalf("draft author was not pseudonymized: %q %v", draftActor, err)
	}
	// Every new context field is populated in Seed and compared byte-for-byte above.
	// Prove the restored schema also cascades current context on account erasure.
	var erasedID string
	if err := restored.Pool.QueryRow(t.Context(), "INSERT INTO learners(display_name) VALUES('Synthetic erasure check') RETURNING id::text").Scan(&erasedID); err != nil {
		t.Fatal(err)
	}
	if _, err := restored.Pool.Exec(t.Context(), `INSERT INTO learner_profiles(learner_id,locale,time_zone,background,goal,topic,level,daily_minutes,daily_xp,reminder_enabled,reminder_time,setup_completed,appearance,motion,narration,narration_speed,captions,text_size,ui_sounds,haptics,analytics_opt_out)
 SELECT $1::uuid,locale,time_zone,background,goal,topic,level,daily_minutes,daily_xp,reminder_enabled,reminder_time,setup_completed,appearance,motion,narration,narration_speed,captions,text_size,ui_sounds,haptics,analytics_opt_out FROM learner_profiles WHERE learner_id='a1111111-1111-4111-8111-111111111111'`, erasedID); err != nil {
		t.Fatal(err)
	}
	if _, err := restored.Pool.Exec(t.Context(), `INSERT INTO course_brief_revisions(learner_id,brief,content_hash) VALUES($1::uuid,'{"topic":"Erased brief"}',repeat('c',64))`, erasedID); err != nil {
		t.Fatal(err)
	}
	if _, err := restored.Pool.Exec(t.Context(), `INSERT INTO outline_drafts(learner_id,brief_revision,outline,history) SELECT learner_id,id,'[]','[]' FROM course_brief_revisions WHERE learner_id=$1::uuid`, erasedID); err != nil {
		t.Fatal(err)
	}
	if _, err := restored.Pool.Exec(t.Context(), `INSERT INTO quick_notes(learner_id, action_key, source, raw_text, status) VALUES($1::uuid, 'erased-key', 'chat', 'Erased note text', 'enrichment_pending')`, erasedID); err != nil {
		t.Fatal(err)
	}
	if _, err := restored.Pool.Exec(t.Context(), `INSERT INTO bookmarks(learner_id, action_key, target_type, course_title, lesson_title) VALUES($1::uuid, 'erased-bm-key', 'lesson', 'Course', 'Lesson')`, erasedID); err != nil {
		t.Fatal(err)
	}
	var restoredQuote string
	if err := restored.Pool.QueryRow(t.Context(), `SELECT id::text FROM pricing_quotes ORDER BY created_at LIMIT 1`).Scan(&restoredQuote); err != nil {
		t.Fatal(err)
	}
	if _, err := restored.Pool.Exec(t.Context(), `WITH c AS (INSERT INTO courses(learner_id,title) VALUES($1::uuid,'Erased copy') RETURNING id),
		cc AS (INSERT INTO course_copies(copy_course_id,learner_id,source_course_id,version,content_hash) SELECT id,$1::uuid,'f1111111-1111-4111-8111-111111111111',1,$2 FROM c RETURNING copy_course_id),
		u AS (INSERT INTO course_units(learner_id,course_id,authored_id,position,title) SELECT $1::uuid,id,'u1',0,'U' FROM c RETURNING id),
		s AS (INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status) SELECT $1::uuid,id,'l1',0,'L','G','current' FROM u RETURNING id),
		g AS (INSERT INTO lesson_generations(learner_id,lesson_id,course_id,quote_id,state) SELECT $1::uuid,s.id,c.id,$3::uuid,'completed' FROM s,c RETURNING id)
		INSERT INTO copy_generation_consents(generation_id,learner_id,copy_course_id,lesson_id,quote_id,pricing_version) SELECT g.id,$1::uuid,c.id,s.id,$3::uuid,'erased-version' FROM g,c,s`, erasedID, strings.Repeat("cd", 32), restoredQuote); err != nil {
		t.Fatal(err)
	}
	if _, err := restored.Pool.Exec(t.Context(), "DELETE FROM learners WHERE id=$1::uuid", erasedID); err != nil {
		t.Fatal(err)
	}
	var survived bool
	if err := restored.Pool.QueryRow(t.Context(), "SELECT EXISTS(SELECT 1 FROM course_copies WHERE learner_id=$1::uuid) OR EXISTS(SELECT 1 FROM publication_links pl JOIN courses c ON c.id=pl.course_id WHERE c.learner_id=$1::uuid) OR EXISTS(SELECT 1 FROM learner_profiles WHERE learner_id=$1::uuid) OR EXISTS(SELECT 1 FROM course_brief_revisions WHERE learner_id=$1::uuid) OR EXISTS(SELECT 1 FROM outline_drafts WHERE learner_id=$1::uuid) OR EXISTS(SELECT 1 FROM course_creations WHERE learner_id=$1::uuid) OR EXISTS(SELECT 1 FROM courses WHERE learner_id=$1::uuid) OR EXISTS(SELECT 1 FROM characters WHERE learner_id=$1::uuid) OR EXISTS(SELECT 1 FROM character_screenings WHERE learner_id=$1::uuid) OR EXISTS(SELECT 1 FROM publication_drafts WHERE owner_id=$1::uuid) OR EXISTS(SELECT 1 FROM publication_screenings WHERE owner_id=$1::uuid) OR EXISTS(SELECT 1 FROM course_casts WHERE learner_id=$1::uuid) OR EXISTS(SELECT 1 FROM lesson_cast_snapshots WHERE learner_id=$1::uuid) OR EXISTS(SELECT 1 FROM quick_notes WHERE learner_id=$1::uuid) OR EXISTS(SELECT 1 FROM quick_note_enrichments WHERE learner_id=$1::uuid) OR EXISTS(SELECT 1 FROM bookmarks WHERE learner_id=$1::uuid)", erasedID).Scan(&survived); err != nil || survived {
		t.Fatal("restored context survived account erasure", err)
	}
	var valid bool
	if err := restored.Pool.QueryRow(t.Context(), `SELECT length(c.code_hash)=64 AND c.expires_at<now() AND c.attempts=5 AND length(t.token_hash)=64 AND t.expires_at>now() AND length(r.key_hash)=64 AND r.expires_at>now() FROM auth_codes c CROSS JOIN auth_refresh_tokens t CROSS JOIN auth_rate_limits r`).Scan(&valid); err != nil || !valid {
		t.Fatal("restored credential hashes/expiry invalid", err)
	}
	auth, err := accounts.New(restored.Pool, "synthetic-backup-auth-signing-key-32-bytes", new(accounts.MailSink))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	auth.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), "POST", "/v1/auth/verify", strings.NewReader(`{"email":"synthetic@example.test","code":"123456","age_confirmed":true}`)))
	if response.Code != http.StatusUnauthorized {
		t.Fatal("restored expired challenge became usable")
	}
	// A restored revoked session rejects even a correctly signed, unexpired JWT.
	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer: "duolify", Audience: jwt.ClaimStrings{"duolify-client"}, Subject: "a1111111-1111-4111-8111-111111111111", ID: "d4444444-4444-4444-8444-444444444444", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
	}).SignedString(backupSigningKey())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequestWithContext(t.Context(), "GET", "/v1/me", nil)
	request.Header.Set("Authorization", "Bearer "+access)
	response = httptest.NewRecorder()
	auth.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatal("restored revoked access became usable")
	}
	// Replay must retain its consumed state and revoke its restored family too.
	for _, secret := range []string{"synthetic-consumed-refresh", "synthetic-active-refresh", "synthetic-revoked-refresh"} {
		response = httptest.NewRecorder()
		auth.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), "POST", "/v1/auth/refresh", strings.NewReader(`{"refresh_token":"`+secret+`"}`)))
		if response.Code != http.StatusUnauthorized {
			t.Fatal("restored consumed/revoked refresh became usable")
		}
	}

	for _, query := range []string{"DELETE FROM credit_allocations", "DELETE FROM credit_usage", "DELETE FROM credit_artifacts", "UPDATE pricing_quotes SET snapshot='{}'", "DELETE FROM pricing_quotes", "TRUNCATE pricing_quotes", "DELETE FROM pricing_quote_consents", "DELETE FROM pricing_quote_policies", "UPDATE credit_entries SET amount=1", "DELETE FROM credit_entries", "TRUNCATE credit_entries", "UPDATE credit_timezones SET time_zone='UTC'", "DELETE FROM credit_timezones", "TRUNCATE credit_timezones", "UPDATE pricing_versions SET config='{}'", "DELETE FROM pricing_versions", "TRUNCATE pricing_versions", "UPDATE pricing_audit SET reason='rewritten'", "DELETE FROM pricing_audit", "TRUNCATE pricing_audit", "UPDATE pricing_activation_events SET activated_at=now()", "DELETE FROM pricing_activation_events", "TRUNCATE pricing_activation_events", "UPDATE pricing_version_cancellations SET reason='rewritten'", "DELETE FROM pricing_version_cancellations", "TRUNCATE pricing_version_cancellations", "UPDATE pricing_verification_evidence SET evidence='{}'", "DELETE FROM pricing_verification_evidence", "TRUNCATE pricing_verification_evidence"} {
		if _, err := restored.Pool.Exec(t.Context(), query); err == nil {
			t.Fatalf("restored immutable data accepted mutation: %s", query)
		}
	}
	// A thread deleted after this backup comes back with it; its tombstone must remove it again and
	// the note saved from it must read "source unavailable" with no reply text left behind.
	for _, statement := range []string{
		`INSERT INTO tutor_threads (id, learner_id, course_id) VALUES ('fa333333-3333-4333-8333-333333333333', 'a1111111-1111-4111-8111-111111111111', 'f1111111-1111-4111-8111-111111111111')`,
		`INSERT INTO tutor_messages (id, thread_id, learner_id, role, content) VALUES ('fa999999-9999-4999-8999-999999999999', 'fa333333-3333-4333-8333-333333333333', 'a1111111-1111-4111-8111-111111111111', 'assistant', 'Resurrected reply text')`,
		`UPDATE quick_note_chat_sources SET unavailable=false, thread_id='fa333333-3333-4333-8333-333333333333', message_id='fa999999-9999-4999-8999-999999999999' WHERE note_id='fa222222-2222-4222-8222-222222222222'`,
	} {
		if _, err := restored.Pool.Exec(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	// E17-07: the restored turn keeps its pinned Character reference; a turn of the resurrected thread
	// loses its reference when the tombstone is applied again.
	var pinned int
	if err := restored.Pool.QueryRow(t.Context(), `SELECT count(*) FROM tutor_turns WHERE idempotency_key='backup-fixture-key' AND speaker_key='supporter' AND character_id='default-supporter' AND character_version=1 AND cast_source='course' AND cast_version=2`).Scan(&pinned); err != nil || pinned != 1 {
		t.Fatalf("restore lost the pinned tutor Character reference: %d %v", pinned, err)
	}
	ghostQuote, err := pricing.NewQuotes(restored.Pool).Create(t.Context(), "a1111111-1111-4111-8111-111111111111", pricing.QuoteInput{Action: "tutor_short", InputTokens: 10, ContextTokens: 100})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restored.Pool.Exec(t.Context(), `INSERT INTO tutor_turns (learner_id, course_id, idempotency_key, quote_id, state, thread_id, assistant_message_id, speaker_key, character_id, character_version, cast_source, cast_version, persona_fallback) VALUES ('a1111111-1111-4111-8111-111111111111', 'f1111111-1111-4111-8111-111111111111', 'resurrected-pin', $1, 'completed', 'fa333333-3333-4333-8333-333333333333', 'fa999999-9999-4999-8999-999999999999', 'guide', 'default-guide', 1, 'default', 0, 'invalid_persona')`, ghostQuote.ID); err != nil {
		t.Fatal(err)
	}
	if err := accounts.ApplyDeletionTombstones(t.Context(), restored.Pool); err != nil {
		t.Fatalf("apply deletion tombstones after restore: %v", err)
	}
	var kept int
	if err := restored.Pool.QueryRow(t.Context(), `SELECT count(*) FROM tutor_turns WHERE idempotency_key='resurrected-pin' AND speaker_key IS NULL AND character_id IS NULL AND character_version IS NULL AND cast_source IS NULL AND cast_version IS NULL AND persona_fallback IS NULL AND assistant_message_id IS NULL`).Scan(&kept); err != nil || kept != 1 {
		t.Fatalf("restore revived a deleted thread's Character reference: %d %v", kept, err)
	}
	var resurrected, unavailable int
	if err := restored.Pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM tutor_threads WHERE id='fa333333-3333-4333-8333-333333333333') + (SELECT count(*) FROM tutor_messages WHERE content='Resurrected reply text')`).Scan(&resurrected); err != nil || resurrected != 0 {
		t.Fatalf("restore resurrected a deleted thread: %d %v", resurrected, err)
	}
	if err := restored.Pool.QueryRow(t.Context(), `SELECT count(*) FROM quick_note_chat_sources s JOIN quick_notes n ON n.id=s.note_id WHERE s.note_id='fa222222-2222-4222-8222-222222222222' AND s.unavailable AND s.thread_id IS NULL AND s.message_id IS NULL AND n.raw_text='Saved before its thread was deleted'`).Scan(&unavailable); err != nil || unavailable != 1 {
		t.Fatalf("surviving provenance not unavailable-but-readable: %d %v", unavailable, err)
	}
	// Replay a restored deletion for the actual payment owner. Identity erasure
	// must leave the paid ledger and business dedupe references usable for audit.
	const paymentOwner = "a1111111-1111-4111-8111-111111111111"
	if _, err := restored.Pool.Exec(t.Context(), "INSERT INTO account_deletion_tombstones(learner_reference) VALUES($1) ON CONFLICT DO NOTHING", accounts.LearnerReference(paymentOwner)); err != nil {
		t.Fatal(err)
	}
	if err := accounts.ApplyDeletionTombstones(t.Context(), restored.Pool); err != nil {
		t.Fatal(err)
	}
	// The backup contains an outstanding preview reservation. Tombstone replay
	// must finalize it before anonymizing, including both operational counters.
	var activeJobs int
	var heldAfterDeletion int64
	if err := restored.Pool.QueryRow(t.Context(), `SELECT count(*) FROM credit_jobs WHERE owner_reference=$1 AND state IN ('reserved','running')`, accounts.LearnerReference(paymentOwner)).Scan(&activeJobs); err != nil || activeJobs != 0 {
		t.Fatalf("restored deletion stranded jobs: %d %v", activeJobs, err)
	}
	if err := restored.Pool.QueryRow(t.Context(), "SELECT COALESCE(sum(held_nanos),0) FROM credit_cost_counters").Scan(&heldAfterDeletion); err != nil || heldAfterDeletion != 0 {
		t.Fatalf("restored deletion stranded capacity: %d %v", heldAfterDeletion, err)
	}
	var outlineSurvived bool
	var positions int
	if err := restored.Pool.QueryRow(t.Context(), "SELECT count(*) FROM lesson_playback_positions WHERE learner_id::text=$1", paymentOwner).Scan(&positions); err != nil || positions != 0 {
		t.Fatal("playback position survived restore tombstone", positions, err)
	}
	var syncEvents int
	if err := restored.Pool.QueryRow(t.Context(), "SELECT count(*) FROM lesson_playback_sync_events WHERE learner_id::text=$1", paymentOwner).Scan(&syncEvents); err != nil || syncEvents != 0 {
		t.Fatal("playback sync event survived restore tombstone", syncEvents, err)
	}
	if err := restored.Pool.QueryRow(t.Context(), "SELECT EXISTS(SELECT 1 FROM outline_drafts WHERE learner_id::text=$1) OR EXISTS(SELECT 1 FROM outline_proposals WHERE learner_id::text=$1) OR EXISTS(SELECT 1 FROM lesson_manifests WHERE learner_id::text=$1)", paymentOwner).Scan(&outlineSurvived); err != nil || outlineSurvived {
		t.Fatal("outline survived restore tombstone", err)
	}
	var anonymous bool
	if err := restored.Pool.QueryRow(t.Context(), "SELECT learner_id IS NULL AND owner_reference=$2 FROM credit_checkout_orders WHERE id=$1", f.paymentOrderID, accounts.LearnerReference(paymentOwner)).Scan(&anonymous); err != nil || !anonymous {
		t.Fatalf("restored payment retained identity: %v %v", anonymous, err)
	}
	f.replayPayment(t, paidService, "checkout.session.expired")
	f.replaySubscription(t, paidService, "customer.subscription.updated")
	f.replaySubscription(t, paidService, "invoice.payment_succeeded")
	if count, err := paidService.IssueAnnualAllowances(t.Context(), asOf.AddDate(2, 0, 0)); err != nil || count != 0 {
		t.Fatalf("deleted annual schedule granted %d %v", count, err)
	}
	if err := restored.Pool.QueryRow(t.Context(), "SELECT count(*) FROM credit_entries WHERE kind='subscription_grant'").Scan(&grants); err != nil || grants != 5 {
		t.Fatalf("deleted restored grant replay %d %v", grants, err)
	}
	if err := restored.Pool.QueryRow(t.Context(), "SELECT learner_id IS NULL FROM credit_checkout_orders WHERE id=$1", f.subscriptionOrderID).Scan(&anonymous); err != nil || !anonymous {
		t.Fatalf("restored subscription retained identity %v %v", anonymous, err)
	}
	if err := restored.Pool.QueryRow(t.Context(), "SELECT count(*) FROM credit_payment_results WHERE state='paid'").Scan(&purchases); err != nil || purchases != 1 {
		t.Fatalf("erased restore duplicated payment: %d %v", purchases, err)
	}
	if _, err := restored.Pool.Exec(t.Context(), "UPDATE credit_payment_payloads SET expires_at=now()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	if err := paidService.PrunePaymentPayloads(t.Context()); err != nil {
		t.Fatal(err)
	}
	var payloads int
	if err := restored.Pool.QueryRow(t.Context(), "SELECT count(*) FROM credit_payment_payloads").Scan(&payloads); err != nil || payloads != 0 {
		t.Fatalf("restored payload expiry: %d %v", payloads, err)
	}
}

// AssertWriterCannotDelete checks real IAM enforcement, including admin boundaries.
func (f *Fixture) AssertWriterCannotDelete(t *testing.T) {
	t.Helper()
	key := f.ArchiveKey(f.lastDate)
	_, err := f.objects.Client.DeleteObject(t.Context(), &s3.DeleteObjectInput{Bucket: aws.String(f.objects.Bucket), Key: aws.String(key)})
	if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("writer deletion not denied: %v", err)
	}
	_, err = f.objects.Client.PutBucketPolicy(t.Context(), &s3.PutBucketPolicyInput{Bucket: aws.String(f.objects.Bucket), Policy: aws.String(`{"Version":"2012-10-17","Statement":[]}`)})
	if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("writer administration not denied: %v", err)
	}
	appOptions := f.writerOptions
	appOptions.AccessKey = "application-writer"
	appOptions.SecretKey = "application-synthetic-secret"
	app, err := backup.NewS3(appOptions)
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.Client.DeleteObject(t.Context(), &s3.DeleteObjectInput{Bucket: aws.String(app.Bucket), Key: aws.String(key)})
	if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("API backup deletion not denied: %v", err)
	}
}

// AssertObjectCount ensures publication failures and retries leave no extra keys.
func (f *Fixture) AssertObjectCount(t *testing.T, count int) {
	t.Helper()
	response, err := f.root.Client.ListObjectsV2(t.Context(), &s3.ListObjectsV2Input{Bucket: aws.String(f.root.Bucket)})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Contents) != count {
		t.Fatalf("objects=%d want=%d", len(response.Contents), count)
	}
}

// AssertEvents flushes the real PostHog wrapper into the local HTTP test double.
func (f *Fixture) AssertEvents(t *testing.T, names ...string) {
	t.Helper()
	if f.packaged != nil {
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		if err := f.packaged.Stop(ctx, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.events.Close(); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	got := make([]string, 0, len(f.captured))
	for _, event := range f.captured {
		got = append(got, event.Event)
		if event.DistinctID != "00000000-0000-4000-8000-000000000011" || event.Properties["source"] != "api" || event.Properties["platform"] != "web" || event.Properties["app_version"] == "" {
			t.Fatalf("invalid common properties: %+v", event)
		}
		for key := range event.Properties {
			switch key {
			case "source", "platform", "app_version", "size_bytes", "duration_ms", "ok", "$lib", "$lib_version", "$os", "$os_version", "$os_distro", "$go_version", "$geoip_disable", "$is_server":
			default:
				t.Fatalf("unexpected backup property %q", key)
			}
		}
	}
	if !reflect.DeepEqual(got, names) {
		t.Fatalf("events=%v want=%v", got, names)
	}
}

// ApplicationObjects accesses only this fixture's disposable versioned asset bucket.
func (f *Fixture) ApplicationObjects() *player.S3 {
	return &player.S3{Client: f.root.Client, Bucket: "application-assets"}
}

func backupSigningKey() []byte {
	key, err := hkdf.Key(sha256.New, []byte("synthetic-backup-auth-signing-key-32-bytes"), nil, "duolify/auth/v1/jwt", 32)
	if err != nil {
		panic(err)
	}
	return key
}
