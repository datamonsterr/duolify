//go:build integration

package backup_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/datamonsterr/duolify/services/api/internal/backup"
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
)

func denied(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("%s not denied: %v", what, err)
	}
}

func TestRustFSCredentialBoundariesAndVersioning(t *testing.T) {
	fixture := backuptest.New(t)
	ctx := t.Context()
	const dump = "2026-01-01/schema-1.dump.age"
	const financial = "financial/2026-01-01/schema-1.dump.age"
	const ledger = "account-deletions/v1/0000000000000000000000000000000000000000000000000000000000000002.json"
	for _, key := range []string{dump, financial, ledger} {
		fixture.SeedBackupObject(t, key, []byte("age-encryption.org/v1\nx"))
	}
	writer, expirer, restorer := fixture.Store("writer"), fixture.Store("expirer"), fixture.Store("restorer")
	del := func(s *backup.S3, key string) error {
		_, err := s.Client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(key)})
		return err
	}
	put := func(s *backup.S3, key string) error {
		_, err := s.Client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(key), Body: bytes.NewReader([]byte("x"))})
		return err
	}
	get := func(s *backup.S3, key string) error {
		out, err := s.Client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(key)})
		if err == nil {
			_ = out.Body.Close()
		}
		return err
	}

	// Append-only writer: no delete of any prefix, no administration.
	for _, key := range []string{dump, financial, ledger} {
		denied(t, "writer delete "+key, del(writer, key))
	}
	// Expirer: only list and delete dumps; never read, write or touch the deletion ledger.
	denied(t, "expirer put", put(expirer, "2026-01-02/schema-1.dump.age"))
	denied(t, "expirer get", get(expirer, dump))
	denied(t, "expirer ledger delete", del(expirer, ledger))
	denied(t, "expirer app bucket", get(&backup.S3{Client: expirer.Client, Bucket: fixture.ApplicationStorage().Bucket}, "slides/a.html"))
	// Restorer: read-only.
	if err := get(restorer, dump); err != nil {
		t.Fatalf("restorer cannot read: %v", err)
	}
	denied(t, "restorer put", put(restorer, "2026-01-03/schema-1.dump.age"))
	denied(t, "restorer delete", del(restorer, dump))

	if keys, err := expirer.List(ctx, "20"); err != nil || len(keys) != 1 {
		t.Fatalf("expirer list %v %v", keys, err)
	}
	if err := del(expirer, dump); err != nil {
		t.Fatalf("expirer cannot delete a dump: %v", err)
	}
	if err := del(expirer, financial); err != nil {
		t.Fatalf("expirer cannot delete a financial dump: %v", err)
	}

	// Separate buckets: backup bucket is not the versioned application bucket.
	app := fixture.ApplicationStorage()
	if app.Bucket == writer.Bucket {
		t.Fatal("application and backup buckets must differ")
	}
	status, err := fixture.RootClient().GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: aws.String(app.Bucket)})
	if err != nil || status.Status != types.BucketVersioningStatusEnabled {
		t.Fatalf("application bucket versioning %v %v", status, err)
	}
	// The application identity cannot erase history or switch versioning off.
	first, err := app.PutVersioned(ctx, "slides/a.html", []byte("one"), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.Client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(app.Bucket), Key: aws.String("slides/a.html"), VersionId: aws.String(first)})
	denied(t, "application version delete", err)
	_, err = app.Client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{Bucket: aws.String(app.Bucket), VersioningConfiguration: &types.VersioningConfiguration{Status: types.BucketVersioningStatusSuspended}})
	denied(t, "application versioning change", err)
	_, err = app.Client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(writer.Bucket), Key: aws.String(ledger)})
	denied(t, "application read of backup bucket", err)
}

func TestOverwrittenAndDeletedObjectsRecoverWithSameReference(t *testing.T) {
	fixture := backuptest.New(t)
	ctx := t.Context()
	app := fixture.ApplicationStorage()
	const key = "audio/lesson-1.mp3"
	first, err := app.PutVersioned(ctx, key, []byte("paid-v1"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.PutVersioned(ctx, key, []byte("oops"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(app.Bucket), Key: aws.String(key)}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.GetCurrent(ctx, key); err == nil {
		t.Fatal("delete marker should hide the object")
	}
	recovery := fixture.Recovery()
	if _, err := recovery.RestoreVersion(ctx, key, first, "recover-rustfs-1"); err != nil {
		t.Fatal(err)
	}
	got, err := app.GetCurrent(ctx, key)
	if err != nil || string(got) != "paid-v1" {
		t.Fatalf("current %q %v", got, err)
	}
	// Tracked operator regeneration appends under the same key; history stays intact.
	if _, err := recovery.Regenerate(ctx, key, "regen-rustfs-2", func(context.Context) ([]byte, error) { return []byte("regenerated"), nil }); err != nil {
		t.Fatal(err)
	}
	versions, err := fixture.AssetOperator().ListVersions(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, v := range versions {
		kinds = append(kinds, v.Metadata["duolify-recovery-kind"])
	}
	if len(versions) != 5 || kinds[0] != "regenerate" || kinds[1] != "restore" {
		t.Fatalf("versions %v", kinds)
	}
	if _, err := recovery.Regenerate(ctx, key, "", func(context.Context) ([]byte, error) { return nil, errors.New("must not run") }); err == nil {
		t.Fatal("silent regeneration accepted")
	}
}

func TestFinancialCopyAndExpiryFailureAnalyticsOnRustFS(t *testing.T) {
	fixture := backuptest.New(t)
	fixture.Seed(t)
	fixture.UseFinancialCopy(t)
	fixture.RunJob(t, "2026-11-12")
	fixture.AssertObjectCount(t, 2)
	if got := fixture.Ciphertext(t, "financial/2026-11-12/schema-"+fixture.SchemaVersionString()+".dump.age"); !bytes.HasPrefix(got, []byte("age-encryption.org/v1")) {
		t.Fatal("financial copy not encrypted")
	}
	// Expired by 40 days: only the general copy rolls off before the financial one.
	result, err := fixture.Expirer(time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC).AddDate(0, 0, 15)).Run(ctx(t))
	if err != nil || result.Deleted != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	fixture.AssertObjectCount(t, 1)
	// A broken expiry job is visible as backup_failed through the PostHog double.
	failing := backup.Expirer{Store: failingStore{}, Retention: backup.DefaultRetention, Now: time.Now, Events: fixture.Events()}
	if _, err := failing.Run(ctx(t)); err == nil {
		t.Fatal("expected failure")
	}
	fixture.AssertEvents(t, "backup_completed", "backup_failed")
}

func ctx(t *testing.T) context.Context { return t.Context() }

type failingStore struct{}

func (failingStore) List(context.Context, string) ([]backup.Object, error) {
	return nil, errors.New("provider down")
}
func (failingStore) Delete(context.Context, string) error { return nil }
