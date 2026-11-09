package credits_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
)

type mockMemoryStore struct {
	mu           sync.Mutex
	entries      []credits.Entry
	timezones    map[string][]credits.TimezoneRecord
	nextEntryID  int64
	nextTZID     int64
	uniqueGrants map[string]bool // key: learnerID + ":" + date
}

func newMockMemoryStore() *mockMemoryStore {
	return &mockMemoryStore{
		timezones:    make(map[string][]credits.TimezoneRecord),
		uniqueGrants: make(map[string]bool),
		nextEntryID:  1,
		nextTZID:     1,
	}
}

func (m *mockMemoryStore) GetLedgerEntries(_ context.Context, learnerID string) ([]credits.Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res []credits.Entry
	for _, e := range m.entries {
		if e.LearnerID == learnerID {
			res = append(res, e)
		}
	}
	return res, nil
}

func (m *mockMemoryStore) ListLedgerActivity(_ context.Context, learnerID string, limit int, beforeID int64) ([]credits.Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res []credits.Entry
	for i := len(m.entries) - 1; i >= 0; i-- {
		e := m.entries[i]
		if e.LearnerID != learnerID {
			continue
		}
		if beforeID > 0 && e.ID >= beforeID {
			continue
		}
		res = append(res, e)
		if len(res) == limit {
			break
		}
	}
	return res, nil
}

func (m *mockMemoryStore) InsertCreditEntry(_ context.Context, e credits.Entry) (credits.Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if e.Kind == "daily_grant" {
		dateStr, _ := e.Provenance["date"].(string)
		key := e.LearnerID + ":" + dateStr
		if m.uniqueGrants[key] {
			return credits.Entry{}, credits.ErrDuplicateGrant
		}
		m.uniqueGrants[key] = true
	}

	e.ID = m.nextEntryID
	m.nextEntryID++
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	m.entries = append(m.entries, e)
	return e, nil
}

func (m *mockMemoryStore) GetEffectiveTimezone(_ context.Context, learnerID string, at time.Time) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	records := m.timezones[learnerID]
	var effective string
	for _, r := range records {
		if !r.EffectiveAt.After(at) {
			effective = r.Timezone
		}
	}
	if effective == "" {
		return "Asia/Ho_Chi_Minh", nil
	}
	return effective, nil
}

func (m *mockMemoryStore) ScheduleTimezoneChange(_ context.Context, learnerID, tz string, effectiveAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.timezones[learnerID] = append(m.timezones[learnerID], credits.TimezoneRecord{
		ID:          m.nextTZID,
		LearnerID:   learnerID,
		Timezone:    tz,
		EffectiveAt: effectiveAt,
		CreatedAt:   time.Now().UTC(),
	})
	m.nextTZID++
	return nil
}

func (m *mockMemoryStore) GetLastDailyGrant(_ context.Context, learnerID string) (*credits.Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.entries) - 1; i >= 0; i-- {
		if m.entries[i].LearnerID == learnerID && m.entries[i].Kind == "daily_grant" {
			e := m.entries[i]
			return &e, nil
		}
	}
	return nil, nil
}

func (m *mockMemoryStore) ListUnexpiredDailyGrants(_ context.Context, _ time.Time) ([]credits.DailyGrantSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var summaries []credits.DailyGrantSummary
	seen := make(map[string]bool)

	for _, e := range m.entries {
		if e.Kind != "daily_grant" {
			continue
		}
		dateStr, _ := e.Provenance["date"].(string)
		key := e.LearnerID + ":" + dateStr
		if seen[key] {
			continue
		}
		seen[key] = true

		// Check if expired already
		hasExpire := false
		for _, other := range m.entries {
			if other.LearnerID == e.LearnerID && other.Kind == "expire" {
				if d, _ := other.Provenance["date"].(string); d == dateStr {
					hasExpire = true
					break
				}
			}
		}
		if !hasExpire {
			summaries = append(summaries, credits.DailyGrantSummary{
				LearnerID: e.LearnerID,
				Date:      dateStr,
				Amount:    e.Amount,
				GrantedAt: e.CreatedAt,
			})
		}
	}
	return summaries, nil
}

type mockEventsRecorder struct {
	mu     sync.Mutex
	events []struct {
		LearnerID string
		Event     string
		Props     map[string]any
	}
}

func (m *mockEventsRecorder) Capture(learnerID, event string, props map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, struct {
		LearnerID string
		Event     string
		Props     map[string]any
	}{learnerID, event, props})
	return nil
}

func (m *mockEventsRecorder) Close() error { return nil }

func (m *mockEventsRecorder) Count(event string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := 0
	for _, e := range m.events {
		if e.Event == event {
			c++
		}
	}
	return c
}

func TestDailyGrant_GrantConcurrently(t *testing.T) {
	ctx := context.Background()
	store := newMockMemoryStore()
	events := &mockEventsRecorder{}
	catalogue := pricing.FromConfig(pricing.Config{
		Version: "usd-launch-2026-11-08",
		DailyFree: struct {
			Credits         int      `json:"credits"`
			EligibleActions []string `json:"eligible_actions"`
			Expires         string   `json:"expires"`
			SpendFirst      bool     `json:"spend_first"`
		}{
			Credits:         46,
			EligibleActions: []string{"quick_note", "tutor_short", "tutor_long"},
			Expires:         "learner_local_day_end",
			SpendFirst:      true,
		},
	})

	svc := credits.NewServiceWithCatalogue(nil, nil, catalogue, events)
	svc.SetStore(store)

	const concurrency = 20
	var wg sync.WaitGroup
	errs := make(chan error, concurrency)
	now := time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)
	learnerID := "00000000-0000-0000-0000-000000000001"

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.EnsureDailyGrant(ctx, learnerID, now)
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("unexpected error: %v", err)
	}

	entries, err := store.GetLedgerEntries(ctx, learnerID)
	if err != nil {
		t.Fatal(err)
	}

	expectedGrantAmount := int64(catalogue.Config().DailyFree.Credits)
	expectedVersion := catalogue.Config().Version
	dailyGrants := 0
	for _, e := range entries {
		if e.Kind == "daily_grant" {
			dailyGrants++
			if e.Amount != expectedGrantAmount {
				t.Fatalf("expected %d credits granted, got %d", expectedGrantAmount, e.Amount)
			}
			if e.PricingVersion != expectedVersion {
				t.Fatalf("expected pricing version %s, got %s", expectedVersion, e.PricingVersion)
			}
		}
	}
	if dailyGrants != 1 {
		t.Fatalf("expected exactly 1 daily_grant entry under concurrency, got %d", dailyGrants)
	}

	if events.Count("daily_credits_granted") != 1 {
		t.Fatalf("expected exactly 1 daily_credits_granted event, got %d", events.Count("daily_credits_granted"))
	}
}

func TestDailyCredits_CrossMidnight(t *testing.T) {
	ctx := context.Background()
	store := newMockMemoryStore()
	events := &mockEventsRecorder{}
	catalogue := pricing.FromConfig(pricing.Config{
		Version: "usd-launch-2026-11-08",
		DailyFree: struct {
			Credits         int      `json:"credits"`
			EligibleActions []string `json:"eligible_actions"`
			Expires         string   `json:"expires"`
			SpendFirst      bool     `json:"spend_first"`
		}{
			Credits:         46,
			EligibleActions: []string{"quick_note", "tutor_short", "tutor_long"},
			Expires:         "learner_local_day_end",
			SpendFirst:      true,
		},
	})
	svc := credits.NewServiceWithCatalogue(nil, nil, catalogue, events)
	svc.SetStore(store)

	learnerID := "00000000-0000-0000-0000-000000000002"

	// Day 1: 46 granted, 8 spent on tutor chat, 15 reserved
	day1 := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	_, err := svc.EnsureDailyGrant(ctx, learnerID, day1)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = store.InsertCreditEntry(ctx, credits.Entry{
		LearnerID: learnerID, Amount: -8, Kind: "spend", Reference: "chat-1",
		PricingVersion: "usd-launch-2026-11-08",
		Provenance:     map[string]any{"bucket": "daily_free", "date": "2026-11-09"},
	})
	_, _ = store.InsertCreditEntry(ctx, credits.Entry{
		LearnerID: learnerID, Amount: -15, Kind: "reserve", Reference: "res-1",
		PricingVersion: "usd-launch-2026-11-08",
		Provenance:     map[string]any{"bucket": "daily_free", "date": "2026-11-09"},
	})

	// Midnight crossed: expire Day 1 as of Day 2
	day2 := time.Date(2026, 11, 10, 1, 0, 0, 0, time.UTC)
	expiredCount, err := svc.ExpireDailyCredits(ctx, day2)
	if err != nil {
		t.Fatal(err)
	}
	if expiredCount != 1 {
		t.Fatalf("expected 1 expired entry, got %d", expiredCount)
	}

	// Unreserved remainder = 46 - 8 - 15 = 23 credits
	entries, _ := store.GetLedgerEntries(ctx, learnerID)
	var expireEntry *credits.Entry
	for i := range entries {
		if entries[i].Kind == "expire" {
			expireEntry = &entries[i]
			break
		}
	}
	if expireEntry == nil {
		t.Fatal("expected expire entry to be created")
	}
	expectedDay1Grant := int64(catalogue.Config().DailyFree.Credits)
	expectedExpire := -(expectedDay1Grant - 8 - 15)
	if expireEntry.Amount != expectedExpire {
		t.Fatalf("expected expire amount %d (unreserved remainder), got %d", expectedExpire, expireEntry.Amount)
	}

	if events.Count("daily_credits_expired") != 1 {
		t.Fatalf("expected 1 daily_credits_expired event, got %d", events.Count("daily_credits_expired"))
	}

	// Day 2 grant gives fresh credits
	_, err = svc.EnsureDailyGrant(ctx, learnerID, day2)
	if err != nil {
		t.Fatal(err)
	}

	wallet, err := svc.GetWalletAt(ctx, learnerID, day2)
	if err != nil {
		t.Fatal(err)
	}
	if wallet.DailyFree.Available != expectedDay1Grant {
		t.Fatalf("expected fresh %d on Day 2, got %d", expectedDay1Grant, wallet.DailyFree.Available)
	}

	// When Day 1's 15 reservation releases 5 on Day 2:
	// "released expired allocations never become spendable tomorrow"
	_, _ = store.InsertCreditEntry(ctx, credits.Entry{
		LearnerID: learnerID, Amount: 5, Kind: "release", Reference: "res-1",
		PricingVersion: catalogue.Config().Version,
		Provenance:     map[string]any{"bucket": "daily_free", "date": "2026-11-09"},
	})

	walletAfterRelease, err := svc.GetWalletAt(ctx, learnerID, day2)
	if err != nil {
		t.Fatal(err)
	}
	if walletAfterRelease.DailyFree.Available != expectedDay1Grant {
		t.Fatalf("released expired credits must not become spendable today; expected %d, got %d", expectedDay1Grant, walletAfterRelease.DailyFree.Available)
	}
}

func TestDailyCredits_ChangeTimezone(t *testing.T) {
	ctx := context.Background()
	store := newMockMemoryStore()
	events := &mockEventsRecorder{}
	catalogue := pricing.FromConfig(pricing.Config{
		Version: "usd-launch-2026-11-08",
		DailyFree: struct {
			Credits         int      `json:"credits"`
			EligibleActions []string `json:"eligible_actions"`
			Expires         string   `json:"expires"`
			SpendFirst      bool     `json:"spend_first"`
		}{
			Credits:         46,
			EligibleActions: []string{"quick_note", "tutor_short", "tutor_long"},
			Expires:         "learner_local_day_end",
			SpendFirst:      true,
		},
	})
	svc := credits.NewServiceWithCatalogue(nil, nil, catalogue, events)
	svc.SetStore(store)

	learnerID := "00000000-0000-0000-0000-000000000003"
	t0 := time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)

	// Initial grant in default timezone Asia/Ho_Chi_Minh
	_, err := svc.EnsureDailyGrant(ctx, learnerID, t0)
	if err != nil {
		t.Fatal(err)
	}

	// Request timezone change to America/New_York
	err = svc.UpdateTimezone(ctx, learnerID, "America/New_York", t0)
	if err != nil {
		t.Fatal(err)
	}

	// Before 24h: effective timezone is still Asia/Ho_Chi_Minh
	t12 := t0.Add(12 * time.Hour)
	tz12, err := svc.GetEffectiveTimezone(ctx, learnerID, t12)
	if err != nil {
		t.Fatal(err)
	}
	if tz12 != "Asia/Ho_Chi_Minh" {
		t.Fatalf("expected Asia/Ho_Chi_Minh before 24h, got %s", tz12)
	}

	// After 24h: effective timezone transitions to America/New_York
	t25 := t0.Add(25 * time.Hour)
	tz25, err := svc.GetEffectiveTimezone(ctx, learnerID, t25)
	if err != nil {
		t.Fatal(err)
	}
	if tz25 != "America/New_York" {
		t.Fatalf("expected America/New_York after 24h, got %s", tz25)
	}

	// Elapsed-time anti-farming guard: if less than 24h elapsed since last grant,
	// cannot grant in new timezone
	t20 := t0.Add(20 * time.Hour)
	canGrant := svc.CanGrantDailyCredits(ctx, learnerID, t20, tz25)
	if canGrant {
		t.Fatal("expected anti-farming guard to reject grant when less than 24h elapsed")
	}

	// When 25h elapsed, new grant is permitted for the new local day
	canGrant25 := svc.CanGrantDailyCredits(ctx, learnerID, t25, tz25)
	if !canGrant25 {
		t.Fatal("expected grant to be permitted after 24h elapsed on new local day")
	}
}

func TestDailyCredits_SpendOrderAndSizing(t *testing.T) {
	catalogue := pricing.FromConfig(pricing.Config{
		Version: "usd-launch-2026-11-08",
		Actions: map[string]pricing.Action{
			"quick_note":     {Credits: 2},
			"tutor_short":    {Credits: 5},
			"tutor_long":     {Credits: 8},
			"preview":        {Credits: 25},
			"outline":        {Credits: 55},
			"lesson_content": {Credits: 140},
			"narration":      {Credits: 50},
		},
		DailyFree: struct {
			Credits         int      `json:"credits"`
			EligibleActions []string `json:"eligible_actions"`
			Expires         string   `json:"expires"`
			SpendFirst      bool     `json:"spend_first"`
		}{
			Credits:         46,
			EligibleActions: []string{"quick_note", "tutor_short", "tutor_long"},
			Expires:         "learner_local_day_end",
			SpendFirst:      true,
		},
	})
	svc := credits.NewServiceWithCatalogue(nil, nil, catalogue, nil)

	// Sizing validation:
	expectedSizingCredits := catalogue.Config().DailyFree.Credits
	// 3 quick notes (3*2 = 6) + 8 short tutor messages (8*5 = 40) = 46
	costNotesShort := 3*catalogue.Config().Actions["quick_note"].Credits + 8*catalogue.Config().Actions["tutor_short"].Credits
	if costNotesShort != expectedSizingCredits {
		t.Fatalf("expected 3 notes + 8 short messages to equal %d, got %d", expectedSizingCredits, costNotesShort)
	}

	// 3 quick notes (3*2 = 6) + 5 long tutor messages (5*8 = 40) = 46
	costNotesLong := 3*catalogue.Config().Actions["quick_note"].Credits + 5*catalogue.Config().Actions["tutor_long"].Credits
	if costNotesLong != expectedSizingCredits {
		t.Fatalf("expected 3 notes + 5 long messages to equal %d, got %d", expectedSizingCredits, costNotesLong)
	}

	// Verify eligibility
	for _, act := range []string{"quick_note", "tutor_short", "tutor_long"} {
		if !svc.IsActionEligibleForDailyFree(act) {
			t.Fatalf("expected %s to be eligible for daily free", act)
		}
	}
	for _, act := range []string{"preview", "outline", "lesson_content", "narration"} {
		if svc.IsActionEligibleForDailyFree(act) {
			t.Fatalf("expected %s NOT to be eligible for daily free", act)
		}
	}
}

func TestDailyGrant_CatalogueRequirements(t *testing.T) {
	ctx := context.Background()

	t.Run("returns clear error when catalogue is nil", func(t *testing.T) {
		svc := credits.NewServiceWithCatalogue(nil, nil, nil, nil)
		svc.SetStore(newMockMemoryStore())
		entry, err := svc.EnsureDailyGrant(ctx, "00000000-0000-0000-0000-000000000001", time.Now())
		if err == nil {
			t.Fatal("expected error when catalogue is nil, got nil")
		}
		if entry != nil {
			t.Fatalf("expected nil entry when catalogue is nil, got %+v", entry)
		}
	})

	t.Run("returns clear error when DailyFree.Credits <= 0", func(t *testing.T) {
		cat := pricing.FromConfig(pricing.Config{
			Version: "usd-custom-2026",
			DailyFree: struct {
				Credits         int      `json:"credits"`
				EligibleActions []string `json:"eligible_actions"`
				Expires         string   `json:"expires"`
				SpendFirst      bool     `json:"spend_first"`
			}{
				Credits: 0,
			},
		})
		svc := credits.NewServiceWithCatalogue(nil, nil, cat, nil)
		svc.SetStore(newMockMemoryStore())
		entry, err := svc.EnsureDailyGrant(ctx, "00000000-0000-0000-0000-000000000001", time.Now())
		if err == nil {
			t.Fatal("expected error when DailyFree.Credits <= 0, got nil")
		}
		if entry != nil {
			t.Fatalf("expected nil entry when DailyFree.Credits <= 0, got %+v", entry)
		}
	})

	t.Run("uses custom amount and active version from catalogue fixture", func(t *testing.T) {
		cat := pricing.FromConfig(pricing.Config{
			Version: "usd-pricing-custom-version",
			DailyFree: struct {
				Credits         int      `json:"credits"`
				EligibleActions []string `json:"eligible_actions"`
				Expires         string   `json:"expires"`
				SpendFirst      bool     `json:"spend_first"`
			}{
				Credits:         75,
				EligibleActions: []string{"quick_note"},
			},
		})
		store := newMockMemoryStore()
		svc := credits.NewServiceWithCatalogue(nil, nil, cat, nil)
		svc.SetStore(store)
		learnerID := "00000000-0000-0000-0000-000000000099"
		entry, err := svc.EnsureDailyGrant(ctx, learnerID, time.Now())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if entry.Amount != 75 {
			t.Fatalf("expected custom amount 75 from catalogue, got %d", entry.Amount)
		}
		if entry.PricingVersion != "usd-pricing-custom-version" {
			t.Fatalf("expected custom pricing version from catalogue, got %s", entry.PricingVersion)
		}
	})
}

func TestCalculateWalletAt_DailyFreeDerivedFromCurrentLocalDateEntries(t *testing.T) {
	ctx := context.Background()
	loc, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
	today := time.Date(2026, 11, 10, 10, 0, 0, 0, loc)
	yesterday := time.Date(2026, 11, 9, 10, 0, 0, 0, loc)

	t.Run("past day grant without today grant results in zero dailyFreeGranted", func(t *testing.T) {
		store := newMockMemoryStore()
		_, _ = store.InsertCreditEntry(ctx, credits.Entry{
			LearnerID:  "00000000-0000-0000-0000-000000000001",
			Amount:     50,
			Kind:       "daily_grant",
			Reference:  "dg-yesterday",
			CreatedAt:  yesterday,
			Provenance: map[string]any{"bucket": "daily_free", "date": "2026-11-09"},
		})
		_, _ = store.InsertCreditEntry(ctx, credits.Entry{
			LearnerID:  "00000000-0000-0000-0000-000000000001",
			Amount:     -50,
			Kind:       "expire",
			Reference:  "exp-yesterday",
			CreatedAt:  time.Date(2026, 11, 9, 23, 59, 0, 0, loc),
			Provenance: map[string]any{"bucket": "daily_free", "date": "2026-11-09"},
		})

		svc := credits.NewServiceWithStore(store, nil)
		wallet, err := svc.GetWalletAt(ctx, "00000000-0000-0000-0000-000000000001", today)
		if err != nil {
			t.Fatal(err)
		}
		if wallet.DailyFree.Granted != 0 {
			t.Fatalf("expected 0 daily free granted today when no grant for today, got %d", wallet.DailyFree.Granted)
		}
	})

	t.Run("today grant amount is reflected in dailyFreeGranted", func(t *testing.T) {
		store := newMockMemoryStore()
		_, _ = store.InsertCreditEntry(ctx, credits.Entry{
			LearnerID:  "00000000-0000-0000-0000-000000000002",
			Amount:     65,
			Kind:       "daily_grant",
			Reference:  "dg-today",
			CreatedAt:  today,
			Provenance: map[string]any{"bucket": "daily_free", "date": "2026-11-10"},
		})

		svc := credits.NewServiceWithStore(store, nil)
		wallet, err := svc.GetWalletAt(ctx, "00000000-0000-0000-0000-000000000002", today)
		if err != nil {
			t.Fatal(err)
		}
		if wallet.DailyFree.Granted != 65 {
			t.Fatalf("expected 65 daily free granted from today's entry, got %d", wallet.DailyFree.Granted)
		}
		if wallet.DailyFree.Available != 65 {
			t.Fatalf("expected 65 daily free available, got %d", wallet.DailyFree.Available)
		}
	})
}
