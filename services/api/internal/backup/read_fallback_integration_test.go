//go:build integration

package backup_test

import (
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/backup"
	"github.com/datamonsterr/duolify/services/api/internal/objecttest"
	"github.com/datamonsterr/duolify/services/api/internal/s3fallback"
)

func TestBackupReadsTryNewThenOldAndWritesStayNew(t *testing.T) {
	srv := objecttest.Start(t)
	srv.Bucket(t, "backups-new", true)
	srv.Bucket(t, "backups-old", true)
	options := backup.S3Options{Endpoint: srv.Endpoint, Region: objecttest.Region, Bucket: "backups-new", ApplicationBucket: "assets", AccessKey: objecttest.AccessKey, SecretKey: objecttest.SecretKey}
	oldOnly, err := backup.NewS3(backup.S3Options{Endpoint: srv.Endpoint, Region: objecttest.Region, Bucket: "backups-old", ApplicationBucket: "assets", AccessKey: objecttest.AccessKey, SecretKey: objecttest.SecretKey})
	if err != nil {
		t.Fatal(err)
	}
	oldVersion, err := oldOnly.PutVersioned(t.Context(), "dumps/legacy.age", []byte("old bytes"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = oldOnly.PutVersioned(t.Context(), "dumps/both.age", []byte("old both"), nil); err != nil {
		t.Fatal(err)
	}

	off, err := backup.NewS3(options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = off.GetCurrent(t.Context(), "dumps/legacy.age"); err == nil {
		t.Fatal("fallback must be off by default")
	}

	options.Fallback = &s3fallback.Store{Endpoint: srv.Endpoint, Region: objecttest.Region, Bucket: "backups-old", AccessKey: objecttest.AccessKey, SecretKey: objecttest.SecretKey}
	on, err := backup.NewS3(options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = on.PutVersioned(t.Context(), "dumps/both.age", []byte("new both"), nil); err != nil {
		t.Fatal(err)
	}
	if got, err := on.GetCurrent(t.Context(), "dumps/legacy.age"); err != nil || string(got) != "old bytes" {
		t.Fatalf("old-only read: %q %v", got, err)
	}
	if got, err := on.GetVersion(t.Context(), "dumps/legacy.age", oldVersion); err != nil || string(got) != "old bytes" {
		t.Fatalf("old-only version read: %q %v", got, err)
	}
	if got, err := on.GetCurrent(t.Context(), "dumps/both.age"); err != nil || string(got) != "new both" {
		t.Fatalf("new store must win: %q %v", got, err)
	}
	if ok, err := on.Exists(t.Context(), "dumps/legacy.age"); err != nil || !ok {
		t.Fatalf("exists through fallback: %v %v", ok, err)
	}
	if ok, err := on.Exists(t.Context(), "dumps/none.age"); err != nil || ok {
		t.Fatalf("absent in both: %v %v", ok, err)
	}
	if _, err = on.PutVersioned(t.Context(), "dumps/fresh.age", []byte("fresh"), nil); err != nil {
		t.Fatal(err)
	}
	if ok, _ := oldOnly.Exists(t.Context(), "dumps/fresh.age"); ok {
		t.Fatal("write reached the old store")
	}
}
