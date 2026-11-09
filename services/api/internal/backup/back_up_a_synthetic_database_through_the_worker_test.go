package backup_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/datamonsterr/duolify/services/api/internal/backup"
)

type source struct {
	fail  bool
	dumps int
}

type changingSchema struct {
	source
	checks int
}

func (s *changingSchema) SchemaVersion(context.Context) (int64, error) {
	s.checks++
	return int64(s.checks), nil
}

func (*source) SchemaVersion(context.Context) (int64, error) { return 1, nil }
func (s *source) Dump(_ context.Context, w io.Writer) error {
	s.dumps++
	_, err := io.WriteString(w, "PGDMP synthetic-private-learner")
	if s.fail {
		return errors.New("synthetic-private-learner postgres://secret")
	}
	return err
}

type objects struct {
	data []byte
	key  string
	fail bool
}

func (s *objects) Exists(context.Context, string) (bool, error) { return len(s.data) > 0, nil }
func (s *objects) Put(_ context.Context, key string, r io.ReadSeeker, _ int64) error {
	if s.fail {
		return errors.New("private provider error")
	}
	var err error
	s.key = key
	s.data, err = io.ReadAll(r)
	return err
}

type events struct {
	names      []string
	properties []map[string]any
	ids        []string
}

func (e *events) Capture(id, name string, p map[string]any) error {
	e.ids = append(e.ids, id)
	e.names = append(e.names, name)
	e.properties = append(e.properties, p)
	return errors.New("analytics unavailable")
}
func (*events) Close() error { return nil }

func TestEncryptBeforePublishAndIdempotentDate(t *testing.T) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	src, dst, ev := &source{}, &objects{}, &events{}
	dir := t.TempDir()
	service, err := backup.New(backup.Options{Source: src, Objects: dst, Recipient: identity.Recipient().String(), Events: ev, TempDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := service.Run(t.Context(), "2026-11-09"); err != nil {
			t.Fatal(err)
		}
	}
	if src.dumps != 1 || dst.key != "2026-11-09/schema-1.dump.age" {
		t.Fatalf("dumps=%d key=%q", src.dumps, dst.key)
	}
	if bytes.Contains(dst.data, []byte("synthetic-private-learner")) {
		t.Fatal("plaintext uploaded")
	}
	decrypted, err := age.Decrypt(bytes.NewReader(dst.data), identity)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := io.ReadAll(decrypted)
	if err != nil || string(plain) != "PGDMP synthetic-private-learner" {
		t.Fatalf("decrypt=%q err=%v", plain, err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatalf("temporary files left: %v %v", files, err)
	}
	assertPrivateEvents(t, ev, true)
}

func TestFailuresPublishNothingAndCleanTemporaryFiles(t *testing.T) {
	for _, stage := range []string{"dump", "upload"} {
		t.Run(stage, func(t *testing.T) {
			identity, err := age.GenerateX25519Identity()
			if err != nil {
				t.Fatal(err)
			}
			src, dst, ev := &source{fail: stage == "dump"}, &objects{fail: stage == "upload"}, &events{}
			dir := t.TempDir()
			service, err := backup.New(backup.Options{Source: src, Objects: dst, Recipient: identity.Recipient().String(), Events: ev, TempDir: dir})
			if err != nil {
				t.Fatal(err)
			}
			if err := service.Run(t.Context(), "2026-11-09"); err == nil || strings.Contains(err.Error(), "private") {
				t.Fatalf("unsafe or absent error: %v", err)
			}
			if len(dst.data) != 0 {
				t.Fatal("partial object published")
			}
			files, err := os.ReadDir(dir)
			if err != nil || len(files) != 0 {
				t.Fatalf("files=%v error=%v", files, err)
			}
			assertPrivateEvents(t, ev, false)
		})
	}
}

func TestInvalidConfigurationDateAndOptOut(t *testing.T) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backup.New(backup.Options{Source: &source{}, Objects: &objects{}, Recipient: "not-a-key"}); err == nil {
		t.Fatal("invalid recipient accepted")
	}
	src, ev := &source{}, &events{}
	service, err := backup.New(backup.Options{Source: src, Objects: &objects{}, Recipient: identity.Recipient().String(), Events: ev, OptOut: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Run(t.Context(), "../2026-11-09"); err == nil {
		t.Fatal("unsafe date accepted")
	}
	if src.dumps != 0 {
		t.Fatal("invalid job dumped data")
	}
	if err := service.Run(t.Context(), "2026-11-09"); err != nil {
		t.Fatal(err)
	}
	if len(ev.names) != 0 {
		t.Fatal("opt-out ignored")
	}
}

func TestNightlyUTCSchedule(t *testing.T) {
	before := time.Date(2026, 11, 9, 1, 59, 59, 0, time.UTC)
	after := backup.Nightly{}.Next(before)
	if !after.Equal(time.Date(2026, 11, 9, 2, 0, 0, 0, time.UTC)) {
		t.Fatal(after)
	}
	if got := (backup.Nightly{}).Next(after); !got.Equal(after.Add(24 * time.Hour)) {
		t.Fatal(got)
	}
	local := time.Date(2026, 11, 9, 9, 0, 0, 0, time.FixedZone("local", 7*3600))
	if got := (backup.Nightly{}).Next(local); !got.Equal(after.Add(24 * time.Hour)) {
		t.Fatal(got)
	}
}

func TestMigrationDuringDumpDoesNotPublishMislabeledArchive(t *testing.T) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	destination := &objects{}
	service, err := backup.New(backup.Options{Source: &changingSchema{}, Objects: destination, Recipient: identity.Recipient().String()})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Run(t.Context(), "2026-11-09"); err == nil {
		t.Fatal("published an archive whose schema version changed during dump")
	}
	if len(destination.data) != 0 {
		t.Fatal("mislabeled archive uploaded")
	}
}

func assertPrivateEvents(t *testing.T, ev *events, ok bool) {
	t.Helper()
	if len(ev.names) == 0 {
		t.Fatal("no event")
	}
	want := "backup_failed"
	if ok {
		want = "backup_completed"
	}
	for i, name := range ev.names {
		p := ev.properties[i]
		if name != want || ev.ids[i] != "00000000-0000-4000-8000-000000000011" || p["source"] != "api" || p["platform"] != "web" || p["app_version"] == "" || p["ok"] != ok {
			t.Fatalf("invalid event: %s %s %v", ev.ids[i], name, p)
		}
		for key := range p {
			switch key {
			case "source", "platform", "app_version", "size_bytes", "duration_ms", "ok":
			default:
				t.Fatalf("private property %q", key)
			}
		}
	}
}
