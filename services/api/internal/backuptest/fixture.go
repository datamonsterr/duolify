// Package backuptest provides disposable synthetic Postgres/RustFS fixtures for
// the backup e2e and integration suites. It never accesses configured services.
package backuptest

import (
	"bytes"
	"context"
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
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/backup"
	"github.com/datamonsterr/duolify/services/api/internal/backupadmin"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
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
	DB            *store.Store
	Logger        *slog.Logger
	container     *postgres.PostgresContainer
	databaseURL   string
	identity      *age.X25519Identity
	objects       *backup.S3
	root          *backup.S3
	faults        *faultStore
	queue         *river.Client[pgx.Tx]
	events        analytics.Events
	mu            sync.Mutex
	captured      []capture
	posthogURL    string
	writerOptions backup.S3Options
	packaged      testcontainers.Container
	schemaVersion int64
	lastDate      string
}

type capture struct {
	Event      string         `json:"event"`
	DistinctID string         `json:"distinct_id"`
	Properties map[string]any `json:"properties"`
}

// New starts isolated containers and all production migrations, never live providers.
func New(t *testing.T) *Fixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	t.Cleanup(cancel)
	f := &Fixture{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))}
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
	f.events, err = analytics.New("synthetic-project-key", posthog.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.events.Close() })
	f.faults = &faultStore{Objects: f.objects}
	source := containerSource{Postgres: backup.Postgres{Pool: f.DB.Pool}, container: f.container}
	service, err := backup.New(backup.Options{Source: source, Objects: f.faults, Recipient: f.identity.Recipient().String(), Events: f.events, TempDir: t.TempDir()})
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
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		if err := f.queue.Stop(stopCtx); err != nil {
			t.Error(err)
		}
	})
	return f
}

type containerSource struct {
	backup.Postgres
	container *postgres.PostgresContainer
}

func (s containerSource) Dump(ctx context.Context, w io.Writer) error {
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
	for _, query := range []string{
		"INSERT INTO learners (id,display_name) VALUES ('a1111111-1111-4111-8111-111111111111','Synthetic Learner')",
		"INSERT INTO credit_entries (learner_id,amount,kind,reference) VALUES ('a1111111-1111-4111-8111-111111111111',120,'purchase','synthetic-purchase'),('a1111111-1111-4111-8111-111111111111',-35,'spend','synthetic-spend')",
		"INSERT INTO learner_profiles (learner_id,locale,time_zone,analytics_opt_out) VALUES ('a1111111-1111-4111-8111-111111111111','vi-VN','Asia/Ho_Chi_Minh',true)",
		"INSERT INTO auth_identities (email,learner_id) VALUES ('synthetic@example.test','a1111111-1111-4111-8111-111111111111')",
		"INSERT INTO auth_codes (email,code_hash,expires_at,attempts) VALUES ('synthetic@example.test',repeat('a',64),now()-interval '1 minute',5)",
		"INSERT INTO auth_sessions (id,learner_id) VALUES ('b2222222-2222-4222-8222-222222222222','a1111111-1111-4111-8111-111111111111')",
		fmt.Sprintf("INSERT INTO auth_refresh_tokens (token_hash,session_id,expires_at,consumed_at) VALUES ('%x','b2222222-2222-4222-8222-222222222222',now()+interval '30 days',now())", sha256.Sum256([]byte("synthetic-consumed-refresh"))),
		fmt.Sprintf("INSERT INTO auth_refresh_tokens (token_hash,session_id,expires_at) VALUES ('%x','b2222222-2222-4222-8222-222222222222',now()+interval '30 days')", sha256.Sum256([]byte("synthetic-active-refresh"))),
		"INSERT INTO auth_sessions (id,learner_id,revoked_at) VALUES ('d4444444-4444-4444-8444-444444444444','a1111111-1111-4111-8111-111111111111',now())",
		fmt.Sprintf("INSERT INTO auth_refresh_tokens (token_hash,session_id,expires_at) VALUES ('%x','d4444444-4444-4444-8444-444444444444',now()+interval '30 days')", sha256.Sum256([]byte("synthetic-revoked-refresh"))),
		"INSERT INTO auth_rate_limits (key_hash,count,expires_at) VALUES (repeat('c',64),3,now()+interval '1 hour')",
	} {
		if _, err := f.DB.Pool.Exec(t.Context(), query); err != nil {
			t.Fatal(err)
		}
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
	if version != f.schemaVersion || riverVersions < 1 || learners != 1 || entries != 2 || balance != 85 || name != "Synthetic Learner" {
		t.Fatalf("invalid restored schema/learner/ledger: %d %d %d %d %d %s", version, riverVersions, learners, entries, balance, name)
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
	// Compare complete auth rows, including hashes, expiry/consumption, age gate and
	// analytics preference. Table names are a fixed fixture allowlist.
	for _, table := range []string{"learner_profiles", "auth_identities", "auth_codes", "auth_sessions", "auth_refresh_tokens", "auth_rate_limits"} {
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
	}).SignedString([]byte("synthetic-backup-auth-signing-key-32-bytes"))
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

	for _, query := range []string{"UPDATE credit_entries SET amount=1", "DELETE FROM credit_entries", "TRUNCATE credit_entries", "UPDATE pricing_versions SET config='{}'", "DELETE FROM pricing_versions", "TRUNCATE pricing_versions"} {
		if _, err := restored.Pool.Exec(t.Context(), query); err == nil {
			t.Fatalf("restored immutable data accepted mutation: %s", query)
		}
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
