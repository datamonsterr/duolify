package backup_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/datamonsterr/duolify/services/api/internal/backup"
)

type expiryStore struct {
	objects   map[string]int64
	deleted   []string
	listErr   error
	deleteErr error
}

func (s *expiryStore) List(_ context.Context, prefix string) ([]backup.Object, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	var out []backup.Object
	for key, size := range s.objects {
		if strings.HasPrefix(key, prefix) {
			out = append(out, backup.Object{Key: key, Size: size})
		}
	}
	return out, nil
}

func (s *expiryStore) Delete(_ context.Context, key string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.deleted = append(s.deleted, key)
	delete(s.objects, key)
	return nil
}

func TestClassifyKey(t *testing.T) {
	for _, tc := range []struct {
		key   string
		class backup.Class
		ok    bool
	}{
		{"2026-11-09/schema-7.dump.age", backup.General, true},
		{"financial/2026-11-09/schema-7.dump.age", backup.Financial, true},
		{"account-deletions/v1/" + strings.Repeat("a", 64) + ".json", 0, false},
		{"2026-13-40/schema-7.dump.age", 0, false},
		{"2026-11-09/schema-7.dump", 0, false},
		{"financial/../2026-11-09/schema-7.dump.age", 0, false},
		{"notes.txt", 0, false},
	} {
		class, date, ok := backup.ClassifyKey(tc.key)
		if ok != tc.ok || (ok && (class != tc.class || date.Format(time.DateOnly) != "2026-11-09")) {
			t.Errorf("%q => %v %v %v", tc.key, class, date, ok)
		}
	}
}

func TestRetentionDefaultsAndFloors(t *testing.T) {
	got, err := backup.LoadRetention(func(string) string { return "" })
	if err != nil || got.GeneralDays != 14 || got.FinancialDays != 35 {
		t.Fatalf("defaults %+v %v", got, err)
	}
	for _, env := range []map[string]string{
		{"BACKUP_GENERAL_RETENTION_DAYS": "13"},
		{"BACKUP_FINANCIAL_RETENTION_DAYS": "34"},
		{"BACKUP_GENERAL_RETENTION_DAYS": "x"},
		{"BACKUP_GENERAL_RETENTION_DAYS": "40", "BACKUP_FINANCIAL_RETENTION_DAYS": "35"},
	} {
		if _, err := backup.LoadRetention(func(k string) string { return env[k] }); err == nil {
			t.Errorf("accepted %v", env)
		}
	}
	longer, err := backup.LoadRetention(func(k string) string {
		return map[string]string{"BACKUP_GENERAL_RETENTION_DAYS": "20", "BACKUP_FINANCIAL_RETENTION_DAYS": "60"}[k]
	})
	if err != nil || longer.GeneralDays != 20 || longer.FinancialDays != 60 {
		t.Fatalf("%+v %v", longer, err)
	}
}

func TestExpirerBoundaries(t *testing.T) {
	now := time.Date(2026, 11, 13, 0, 0, 1, 0, time.UTC)
	store := &expiryStore{objects: map[string]int64{
		"2026-10-30/schema-1.dump.age":                              10, // exactly 14 days: kept
		"2026-10-29/schema-1.dump.age":                              20, // 15 days: expired
		"financial/2026-10-09/schema-1.dump.age":                    30, // exactly 35 days: kept
		"financial/2026-10-08/schema-1.dump.age":                    40, // 36 days: expired
		"2026-11-14/schema-1.dump.age":                              50, // future: kept
		"account-deletions/v1/" + strings.Repeat("b", 64) + ".json": 60,
	}}
	result, err := backup.Expirer{Store: store, Retention: backup.DefaultRetention, Now: func() time.Time { return now }}.Run(t.Context())
	if err != nil || result.Deleted != 2 || result.Bytes != 60 {
		t.Fatalf("%+v %v", result, err)
	}
	slices.Sort(store.deleted)
	if !slices.Equal(store.deleted, []string{"2026-10-29/schema-1.dump.age", "financial/2026-10-08/schema-1.dump.age"}) {
		t.Fatalf("deleted %v", store.deleted)
	}
}

func TestExpirerFailureEmitsPrivateBackupFailedAndHonorsOptOut(t *testing.T) {
	for _, optOut := range []bool{false, true} {
		ev := &events{}
		store := &expiryStore{objects: map[string]int64{"2026-01-01/schema-1.dump.age": 5}, deleteErr: errors.New("secret-provider endpoint 10.0.0.1")}
		_, err := backup.Expirer{Store: store, Retention: backup.DefaultRetention, Now: time.Now, Events: ev, OptOut: optOut}.Run(t.Context())
		if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "2026-01-01") {
			t.Fatalf("unsafe error %v", err)
		}
		if optOut {
			if len(ev.names) != 0 {
				t.Fatal("opt-out still emitted")
			}
			continue
		}
		assertPrivateEvents(t, ev, false)
	}
	ev := &events{}
	_, err := backup.Expirer{Store: &expiryStore{listErr: errors.New("boom")}, Retention: backup.DefaultRetention, Now: time.Now, Events: ev}.Run(t.Context())
	if err == nil || len(ev.names) != 1 {
		t.Fatalf("list failure not reported: %v %v", err, ev.names)
	}
}

func TestFinancialCopyPublishedBeforeGeneralAndRetryIsIdempotent(t *testing.T) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	dst := &multiObjects{data: map[string][]byte{}}
	service, err := backup.New(backup.Options{Source: &source{}, Objects: dst, Recipient: identity.Recipient().String(), FinancialCopy: true, TempDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := service.Run(t.Context(), "2026-11-09"); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(dst.order, []string{"financial/2026-11-09/schema-1.dump.age", "2026-11-09/schema-1.dump.age"}) {
		t.Fatalf("order %v", dst.order)
	}
	if !bytes.Equal(dst.data["financial/2026-11-09/schema-1.dump.age"], dst.data["2026-11-09/schema-1.dump.age"]) || bytes.Contains(dst.data["financial/2026-11-09/schema-1.dump.age"], []byte("synthetic-private-learner")) {
		t.Fatal("financial copy differs or is plaintext")
	}
}

type multiObjects struct {
	data  map[string][]byte
	order []string
}

func (m *multiObjects) Exists(_ context.Context, key string) (bool, error) {
	_, ok := m.data[key]
	return ok, nil
}

func (m *multiObjects) Put(_ context.Context, key string, r io.ReadSeeker, _ int64) error {
	body, err := io.ReadAll(r)
	if _, ok := m.data[key]; !ok {
		m.order = append(m.order, key)
		m.data[key] = body
	}
	return err
}

type versioned struct {
	versions []backup.Version
	bodies   map[string][]byte
	put      []map[string]string
	putErr   error
}

func (v *versioned) ListVersions(context.Context, string) ([]backup.Version, error) {
	return v.versions, nil
}

func (v *versioned) GetVersion(_ context.Context, _, id string) ([]byte, error) {
	return v.bodies[id], nil
}

func (v *versioned) PutVersioned(_ context.Context, _ string, _ []byte, meta map[string]string) (string, error) {
	v.put = append(v.put, meta)
	return "v-new", v.putErr
}

func TestRecoveryRequiresTrackedJobAndExistingLiveVersion(t *testing.T) {
	store := &versioned{
		versions: []backup.Version{{ID: "v2", Latest: true}, {ID: "dm", DeleteMarker: true}, {ID: "v1"}},
		bodies:   map[string][]byte{"v1": []byte("old")},
	}
	var logs bytes.Buffer
	recovery := backup.Recovery{Objects: store, Logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	for _, tc := range []struct{ version, job string }{{"v1", ""}, {"v1", "bad job!"}, {"missing", "job-12345"}, {"dm", "job-12345"}, {"v2", "job-12345"}} {
		if _, err := recovery.RestoreVersion(t.Context(), "slides/a.html", tc.version, tc.job); err == nil {
			t.Errorf("accepted %+v", tc)
		}
	}
	if len(store.put) != 0 {
		t.Fatal("rejected recoveries wrote objects")
	}
	id, err := recovery.RestoreVersion(t.Context(), "slides/a.html", "v1", "job-12345")
	if err != nil || id != "v-new" {
		t.Fatalf("%q %v", id, err)
	}
	want := map[string]string{"duolify-recovery-job": "job-12345", "duolify-recovery-kind": "restore", "duolify-recovery-source-version": "v1"}
	if len(store.put) != 1 || !mapsEqual(store.put[0], want) {
		t.Fatalf("metadata %v", store.put)
	}
	if !strings.Contains(logs.String(), "job-12345") || strings.Contains(logs.String(), "old") {
		t.Fatalf("operational log: %s", logs.String())
	}
}

func TestRegenerateIsTrackedAndNeverSilent(t *testing.T) {
	store := &versioned{}
	recovery := backup.Recovery{Objects: store}
	produce := func(context.Context) ([]byte, error) { return []byte("new"), nil }
	if _, err := recovery.Regenerate(t.Context(), "slides/a.html", "", produce); err == nil {
		t.Fatal("silent regeneration accepted")
	}
	if _, err := recovery.Regenerate(t.Context(), "slides/a.html", "job-12345", func(context.Context) ([]byte, error) { return nil, errors.New("x") }); err == nil || len(store.put) != 0 {
		t.Fatal("failed generator wrote an object")
	}
	if _, err := recovery.Regenerate(t.Context(), "slides/a.html", "job-12345", produce); err != nil || store.put[0]["duolify-recovery-kind"] != "regenerate" {
		t.Fatalf("%v %v", err, store.put)
	}
}

func TestRecoveryFailureEmitsBackupFailedWithoutKey(t *testing.T) {
	ev := &events{}
	store := &versioned{versions: []backup.Version{{ID: "v1"}, {ID: "v2", Latest: true}}, bodies: map[string][]byte{"v1": []byte("x")}, putErr: errors.New("private-key-name slides/secret.html")}
	_, err := backup.Recovery{Objects: store, Events: ev}.RestoreVersion(t.Context(), "slides/secret.html", "v1", "job-12345")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe error %v", err)
	}
	assertPrivateEvents(t, ev, false)
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
