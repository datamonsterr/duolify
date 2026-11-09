package credits_test

import (
	"context"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/credits"
)

type mockLedgerStore struct {
	entries []credits.Entry
}

func (m *mockLedgerStore) GetLedgerEntries(_ context.Context, _ string) ([]credits.Entry, error) {
	return m.entries, nil
}

func (m *mockLedgerStore) ListLedgerActivity(_ context.Context, _ string, limit int, beforeID int64) ([]credits.Entry, error) {
	var result []credits.Entry
	for _, e := range m.entries {
		if beforeID > 0 && e.ID >= beforeID {
			continue
		}
		result = append(result, e)
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func (m *mockLedgerStore) InsertCreditEntry(_ context.Context, e credits.Entry) (credits.Entry, error) {
	e.ID = int64(len(m.entries) + 1)
	m.entries = append(m.entries, e)
	return e, nil
}

func (m *mockLedgerStore) GetEffectiveTimezone(_ context.Context, _ string, _ time.Time) (string, error) {
	return "Asia/Ho_Chi_Minh", nil
}

func (m *mockLedgerStore) ScheduleTimezoneChange(_ context.Context, _, _ string, _ time.Time) error {
	return nil
}

func (m *mockLedgerStore) GetLastDailyGrant(_ context.Context, _ string) (*credits.Entry, error) {
	return nil, nil
}

func (m *mockLedgerStore) ListUnexpiredDailyGrants(_ context.Context, _ time.Time) ([]credits.DailyGrantSummary, error) {
	return nil, nil
}

func TestWallet_BoundaryCasesAndCalculations(t *testing.T) {
	ctx := context.Background()

	t.Run("empty ledger yields zero balances", func(t *testing.T) {
		mock := &mockLedgerStore{entries: nil}
		svc := credits.NewServiceWithStore(mock, nil)

		wallet, err := svc.GetWallet(ctx, "00000000-0000-0000-0000-000000000001")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if wallet.Available != 0 || wallet.Purchased != 0 || wallet.Subscription != 0 || wallet.Reserved != 0 {
			t.Fatalf("expected all zeros, got %+v", wallet)
		}
		if wallet.DailyFree.Available != 0 {
			t.Fatalf("expected 0 daily free available, got %d", wallet.DailyFree.Available)
		}
		if wallet.DailyFree.Granted != 0 {
			t.Fatalf("expected 0 daily free granted for empty ledger, got %d", wallet.DailyFree.Granted)
		}
	})

	t.Run("historical reward is unspendable", func(t *testing.T) {
		mock := &mockLedgerStore{
			entries: []credits.Entry{
				{ID: 1, Amount: 100, Kind: "reward", Reference: "old-streak"},
				{ID: 2, Amount: 50, Kind: "purchase", Reference: "pack-1"},
			},
		}
		svc := credits.NewServiceWithStore(mock, nil)

		wallet, err := svc.GetWallet(ctx, "00000000-0000-0000-0000-000000000002")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Reward must NOT be added to Available or Purchased
		if wallet.Available != 50 {
			t.Fatalf("expected available 50, got %d", wallet.Available)
		}
		if wallet.Purchased != 50 {
			t.Fatalf("expected purchased 50, got %d", wallet.Purchased)
		}
	})

	t.Run("daily free expired credits reduce pool to zero", func(t *testing.T) {
		mock := &mockLedgerStore{
			entries: []credits.Entry{
				{ID: 1, Amount: 46, Kind: "daily_grant", Reference: "dg-1", Provenance: map[string]any{"bucket": "daily_free"}},
				{ID: 2, Amount: -8, Kind: "spend", Reference: "tutor-1", Provenance: map[string]any{"bucket": "daily_free"}},
				{ID: 3, Amount: -38, Kind: "expire", Reference: "exp-1", Provenance: map[string]any{"bucket": "daily_free"}},
			},
		}
		svc := credits.NewServiceWithStore(mock, nil)

		wallet, err := svc.GetWallet(ctx, "00000000-0000-0000-0000-000000000003")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if wallet.DailyFree.Available != 0 {
			t.Fatalf("expected 0 daily free after expire, got %d", wallet.DailyFree.Available)
		}
		if wallet.Available != 0 {
			t.Fatalf("expected 0 available after expire, got %d", wallet.Available)
		}
	})

	t.Run("in-flight reservation is held and releases unused estimate", func(t *testing.T) {
		// Outline: 55 reserved, settled at 52 (+3 released)
		mock := &mockLedgerStore{
			entries: []credits.Entry{
				{ID: 1, Amount: 400, Kind: "purchase", Reference: "pack-400", Provenance: map[string]any{"bucket": "purchase"}},
				{ID: 2, Amount: -55, Kind: "reserve", Reference: "outline-1", Provenance: map[string]any{"bucket": "purchase"}},
				{ID: 3, Amount: 3, Kind: "release", Reference: "outline-1", Provenance: map[string]any{"bucket": "purchase"}},
				{ID: 4, Amount: -190, Kind: "reserve", Reference: "lesson-3", Provenance: map[string]any{"bucket": "purchase"}},
			},
		}
		svc := credits.NewServiceWithStore(mock, nil)

		wallet, err := svc.GetWallet(ctx, "00000000-0000-0000-0000-000000000004")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Purchased net: 400 - 55 + 3 - 190 = 158
		// Reserved: 190 (since lesson-3 is still in-flight)
		if wallet.Reserved != 190 {
			t.Fatalf("expected reserved 190, got %d", wallet.Reserved)
		}
		if wallet.Purchased != 158 {
			t.Fatalf("expected purchased 158, got %d", wallet.Purchased)
		}
		if wallet.Available != 158 {
			t.Fatalf("expected available 158, got %d", wallet.Available)
		}
	})

	t.Run("cursor pagination validation", func(t *testing.T) {
		mock := &mockLedgerStore{
			entries: []credits.Entry{
				{ID: 3, Amount: 10, Kind: "purchase", CreatedAt: time.Now()},
				{ID: 2, Amount: 20, Kind: "purchase", CreatedAt: time.Now()},
				{ID: 1, Amount: 30, Kind: "purchase", CreatedAt: time.Now()},
			},
		}
		svc := credits.NewServiceWithStore(mock, nil)

		// Invalid cursor string should error
		_, err := svc.ListActivity(ctx, "00000000-0000-0000-0000-000000000005", -5, "invalid-cursor-non-base64")
		if err == nil {
			t.Fatal("expected error for invalid cursor, got nil")
		}

		// Valid pagination
		page, err := svc.ListActivity(ctx, "00000000-0000-0000-0000-000000000005", 2, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(page.Entries) != 2 {
			t.Fatalf("expected 2 entries, got %d", len(page.Entries))
		}
		if page.NextCursor == "" {
			t.Fatal("expected non-empty cursor")
		}
	})
}
