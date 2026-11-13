package backup_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"sync"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/datamonsterr/duolify/services/api/internal/backup"
	"github.com/jackc/pgx/v5/pgxpool"
)

type drillStore struct {
	objects map[string][]byte
}

func (s drillStore) List(_ context.Context, prefix string) ([]backup.Object, error) {
	var out []backup.Object
	for key, body := range s.objects {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			out = append(out, backup.Object{Key: key, Size: int64(len(body))})
		}
	}
	return out, nil
}

func (s drillStore) GetCurrent(_ context.Context, key string) ([]byte, error) {
	body, ok := s.objects[key]
	if !ok {
		return nil, errors.New("missing")
	}
	return body, nil
}

type captured struct {
	id, event string
	props     map[string]any
}
type recorder struct {
	mu     sync.Mutex
	events []captured
}

func (r *recorder) Capture(id, event string, props map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, captured{id, event, props})
	return nil
}
func (r *recorder) names() []string {
	var out []string
	for _, e := range r.events {
		out = append(out, e.event)
	}
	return out
}

func neverRestore(context.Context, []byte) (*pgxpool.Pool, func(), error) {
	return nil, nil, errors.New("restore must not be reached")
}

func TestNewestDumpUsesKeyDateAndIgnoresOtherPrefixes(t *testing.T) {
	objects := []backup.Object{
		{Key: "2026-11-10/schema-4.dump.age"}, {Key: "2026-11-12/schema-7.dump.age"},
		{Key: "financial/2026-11-13/schema-7.dump.age"}, {Key: "account-deletions/v1/" + string(bytes.Repeat([]byte("a"), 64)) + ".json"},
		{Key: "operator-notes.txt"}, {Key: "2026-11-11/schema-5.dump.age"},
	}
	key, date, schema, ok := backup.NewestDump(objects)
	if !ok || key != "2026-11-12/schema-7.dump.age" || schema != 7 || !date.Equal(time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("newest=%q %v %d %v", key, date, schema, ok)
	}
	if _, _, _, ok := backup.NewestDump(objects[2:5]); ok {
		t.Fatal("financial copies and ledger objects must not count as the general dump")
	}
}

func TestRPOAndRTOBudgetsAreBoundaryExact(t *testing.T) {
	day := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		age time.Duration
		ok  bool
	}{{0, true}, {24 * time.Hour, true}, {24*time.Hour + time.Nanosecond, false}} {
		if got := backup.RPOWithinBudget(day.Add(c.age).Sub(day)); got != c.ok {
			t.Fatalf("rpo %v ok=%v", c.age, got)
		}
	}
	if !backup.RTOWithinBudget(4*time.Hour) || backup.RTOWithinBudget(4*time.Hour+time.Nanosecond) {
		t.Fatal("RTO budget is 4h inclusive")
	}
	// A dump dated in the future (clock skew) is not an RPO violation.
	if backup.RPO(day, day.Add(time.Hour)) != 0 {
		t.Fatal("negative RPO must clamp to zero")
	}
}

func TestDrillFailureEmitsBackupFailedAndDrillCompletedWithoutPrivateText(t *testing.T) {
	identity, _ := age.GenerateX25519Identity()
	events := &recorder{}
	store := drillStore{objects: map[string][]byte{"2026-11-12/schema-7.dump.age": []byte("age-encryption.org/v1\nnot a real ciphertext PRIVATE-LEARNER-TEXT")}}
	report, err := backup.RunDrill(t.Context(), backup.DrillOptions{
		Store: store, Identity: identity, Restore: neverRestore, Source: "ci", Events: events,
		Now: func() time.Time { return time.Date(2026, 11, 12, 6, 0, 0, 0, time.UTC) },
	})
	if err == nil || report.OK {
		t.Fatalf("corrupt ciphertext must fail the drill: %v %+v", err, report)
	}
	if !slices.Equal(events.names(), []string{"backup_failed", "restore_drill_completed"}) {
		t.Fatalf("events=%v", events.names())
	}
	for _, e := range events.events {
		if e.id != "00000000-0000-4000-8000-000000000011" || e.props["source"] != "ci" || e.props["platform"] != "web" || e.props["app_version"] == "" || e.props["ok"] != false {
			t.Fatalf("common properties: %+v", e)
		}
		if _, ok := e.props["duration_ms"].(int64); !ok {
			t.Fatalf("duration_ms: %+v", e.props)
		}
		for key, value := range e.props {
			switch key {
			case "source", "platform", "app_version", "ok", "duration_ms", "size_bytes":
			default:
				t.Fatalf("unexpected property %q", key)
			}
			if s, ok := value.(string); ok && (len(s) > 40 || bytes.Contains([]byte(s), []byte("PRIVATE"))) {
				t.Fatalf("private text leaked in %q", key)
			}
		}
	}
	if bytes.Contains([]byte(err.Error()), []byte("PRIVATE-LEARNER-TEXT")) {
		t.Fatal("error leaked ciphertext content")
	}
}

func TestDrillFailsWhenNoDumpOrStaleAndHonoursOptOut(t *testing.T) {
	identity, _ := age.GenerateX25519Identity()
	now := func() time.Time { return time.Date(2026, 11, 14, 0, 0, 1, 0, time.UTC) }
	events := &recorder{}
	// No dump at all.
	if _, err := backup.RunDrill(t.Context(), backup.DrillOptions{Store: drillStore{}, Identity: identity, Restore: neverRestore, Source: "ci", Events: events, Now: now}); err == nil {
		t.Fatal("an empty backup bucket must fail the drill")
	}
	// Stale: newest key date is more than 24h before now; fails before any restore.
	stale := drillStore{objects: map[string][]byte{"2026-11-12/schema-7.dump.age": []byte("x")}}
	report, err := backup.RunDrill(t.Context(), backup.DrillOptions{Store: stale, Identity: identity, Restore: neverRestore, Source: "ci", Events: events, Now: now})
	if err == nil || report.RPO <= 24*time.Hour {
		t.Fatalf("stale dump must exceed RPO: %v %+v", err, report)
	}
	if len(events.events) == 0 {
		t.Fatal("failures must be emitted")
	}
	opted := &recorder{}
	_, _ = backup.RunDrill(t.Context(), backup.DrillOptions{Store: drillStore{}, Identity: identity, Restore: neverRestore, Source: "ci", Events: opted, OptOut: true, Now: now})
	if len(opted.events) != 0 {
		t.Fatalf("opt-out must suppress drill events: %v", opted.names())
	}
}

func TestScheduledDrillIsDisabledWithoutOwnerCredentials(t *testing.T) {
	_, err := backup.LoadDrill(func(string) string { return "" })
	if !errors.Is(err, backup.ErrDrillDisabled) {
		t.Fatalf("missing credentials must disable, not fail: %v", err)
	}
	partial := map[string]string{"BACKUP_S3_ENDPOINT": "http://127.0.0.1:1", "BACKUP_RESTORE_S3_ACCESS_KEY_ID": "restorer"}
	if _, err := backup.LoadDrill(func(k string) string { return partial[k] }); !errors.Is(err, backup.ErrDrillDisabled) {
		t.Fatalf("partial credentials stay disabled: %v", err)
	}
}

func TestAnonymizerHelpersAreDeterministicAndDetectPII(t *testing.T) {
	a, b := backup.AnonymizedEmail("a1111111-1111-4111-8111-111111111111"), backup.AnonymizedEmail("b1111111-1111-4111-8111-111111111111")
	if a == b || a != backup.AnonymizedEmail("a1111111-1111-4111-8111-111111111111") || backup.ContainsPII(a) {
		t.Fatalf("anonymized email %q %q", a, b)
	}
	for _, leaked := range []string{"learner@example.com", `{"to":"x.y+z@mail.example.org"}`} {
		if !backup.ContainsPII(leaked) {
			t.Fatalf("email not detected in %q", leaked)
		}
	}
	for _, clean := range []string{"", "Learner 1a2b3c4d", "[redacted]", "2026-11-12/schema-7.dump.age"} {
		if backup.ContainsPII(clean) {
			t.Fatalf("false positive %q", clean)
		}
	}
}

func TestFinancialReplayHookIsExtensionPointOnly(t *testing.T) {
	var hook backup.FinancialReplay
	if hook != nil {
		t.Fatal("no business checks are bundled; E7-11 supplies the hook")
	}
	_ = io.Discard
}
