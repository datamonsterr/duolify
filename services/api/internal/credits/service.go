// Package credits provides wallet derivation and auditable credit ledger activity.
package credits

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	dbsql "github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// ErrDuplicateGrant indicates that a daily grant was already issued for the specified date.
var ErrDuplicateGrant = errors.New("daily grant already issued for this date")

// Entry represents an immutable credit ledger row.
type Entry struct {
	ID             int64          `json:"id"`
	LearnerID      string         `json:"learner_id"`
	Amount         int64          `json:"amount"`
	Kind           string         `json:"kind"`
	Reference      string         `json:"reference"`
	CreatedAt      time.Time      `json:"created_at"`
	PricingVersion string         `json:"pricing_version,omitempty"`
	Provenance     map[string]any `json:"provenance,omitempty"`
}

// TimezoneRecord represents a tracked timezone change with 24-hour delayed effective instant.
type TimezoneRecord struct {
	ID          int64     `json:"id"`
	LearnerID   string    `json:"learner_id"`
	Timezone    string    `json:"time_zone"`
	EffectiveAt time.Time `json:"effective_at"`
	CreatedAt   time.Time `json:"created_at"`
}

// DailyGrantSummary summarizes an issued daily grant for worker expiry processing.
type DailyGrantSummary struct {
	LearnerID string    `json:"learner_id"`
	Date      string    `json:"date"`
	Amount    int64     `json:"amount"`
	GrantedAt time.Time `json:"granted_at"`
}

// DailyFreeSummary describes the available daily free credits pool and its reset instant.
type DailyFreeSummary struct {
	Available int64     `json:"available"`
	Granted   int64     `json:"granted"`
	ResetsAt  time.Time `json:"resets_at"`
	Timezone  string    `json:"timezone"`
}

// WalletResponse carries the complete bucketed wallet balances and recent activity overview.
type WalletResponse struct {
	Available      int64             `json:"available"`
	DailyFree      DailyFreeSummary  `json:"daily_free"`
	Purchased      int64             `json:"purchased"`
	Subscription   int64             `json:"subscription"`
	Reserved       int64             `json:"reserved"`
	ReservedNote   string            `json:"reserved_note,omitempty"`
	Cached         bool              `json:"cached,omitempty"`
	Unverified     bool              `json:"unverified,omitempty"`
	LastVerifiedAt string            `json:"last_verified_at,omitempty"`
	RecentActivity []ActivityItemDTO `json:"recent_activity"`
}

// ActivityItemDTO is a human-readable ledger activity entry.
type ActivityItemDTO struct {
	ID            int64  `json:"id"`
	Date          string `json:"date"`
	Activity      string `json:"activity"`
	Detail        string `json:"detail,omitempty"`
	Source        string `json:"source"`
	AmountDisplay string `json:"amount_display"`
	Amount        int64  `json:"amount"`
	Kind          string `json:"kind"`
}

// ActivityPageDTO holds a cursor-paginated page of ledger activity entries.
type ActivityPageDTO struct {
	Entries    []ActivityItemDTO `json:"entries"`
	NextCursor string            `json:"next_cursor"`
}

// LedgerStore abstracts access to the append-only credit ledger and timezone history.
type LedgerStore interface {
	GetLedgerEntries(ctx context.Context, learnerID string) ([]Entry, error)
	ListLedgerActivity(ctx context.Context, learnerID string, limit int, beforeID int64) ([]Entry, error)
	InsertCreditEntry(ctx context.Context, e Entry) (Entry, error)
	GetEffectiveTimezone(ctx context.Context, learnerID string, at time.Time) (string, error)
	ScheduleTimezoneChange(ctx context.Context, learnerID, tz string, effectiveAt time.Time) error
	GetLastDailyGrant(ctx context.Context, learnerID string) (*Entry, error)
	ListUnexpiredDailyGrants(ctx context.Context, asOf time.Time) ([]DailyGrantSummary, error)
}

type pgStore struct {
	pool *pgxpool.Pool
}

func (s *pgStore) GetLedgerEntries(ctx context.Context, learnerID string) ([]Entry, error) {
	var uid pgtype.UUID
	if err := uid.Scan(learnerID); err != nil {
		return nil, fmt.Errorf("invalid learner id: %w", err)
	}

	queries := dbsql.New(s.pool)
	rows, err := queries.ListCreditEntries(ctx, uid)
	if err != nil {
		return nil, err
	}

	entries := make([]Entry, len(rows))
	for i, r := range rows {
		entries[i] = entryFromDB(r)
	}
	return entries, nil
}

func (s *pgStore) ListLedgerActivity(ctx context.Context, learnerID string, limit int, beforeID int64) ([]Entry, error) {
	var uid pgtype.UUID
	if err := uid.Scan(learnerID); err != nil {
		return nil, fmt.Errorf("invalid learner id: %w", err)
	}

	safeLimit := int32(20)
	if limit > 0 && limit <= 100 {
		safeLimit = int32(limit)
	}

	queries := dbsql.New(s.pool)
	rows, err := queries.ListCreditEntriesCursor(ctx, dbsql.ListCreditEntriesCursorParams{
		LearnerID: uid,
		Column2:   beforeID,
		Limit:     safeLimit,
	})
	if err != nil {
		return nil, err
	}

	entries := make([]Entry, len(rows))
	for i, r := range rows {
		entries[i] = entryFromDB(r)
	}
	return entries, nil
}

func (s *pgStore) InsertCreditEntry(ctx context.Context, e Entry) (Entry, error) {
	var uid pgtype.UUID
	if err := uid.Scan(e.LearnerID); err != nil {
		return Entry{}, fmt.Errorf("invalid learner id: %w", err)
	}
	provBytes, err := json.Marshal(e.Provenance)
	if err != nil {
		provBytes = []byte("{}")
	}

	queries := dbsql.New(s.pool)
	row, err := queries.CreateCreditEntry(ctx, dbsql.CreateCreditEntryParams{
		LearnerID:      uid,
		Amount:         e.Amount,
		Kind:           e.Kind,
		Reference:      e.Reference,
		PricingVersion: pgtype.Text{String: e.PricingVersion, Valid: e.PricingVersion != ""},
		Column6:        provBytes,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Entry{}, ErrDuplicateGrant
		}
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "credit_entries_daily_grant_uniq") {
			return Entry{}, ErrDuplicateGrant
		}
		return Entry{}, err
	}
	return entryFromDB(row), nil
}

func (s *pgStore) GetEffectiveTimezone(ctx context.Context, learnerID string, at time.Time) (string, error) {
	var uid pgtype.UUID
	if err := uid.Scan(learnerID); err != nil {
		return "", fmt.Errorf("invalid learner id: %w", err)
	}
	queries := dbsql.New(s.pool)
	row, err := queries.GetEffectiveCreditTimezone(ctx, dbsql.GetEffectiveCreditTimezoneParams{
		LearnerID:   uid,
		EffectiveAt: pgtype.Timestamptz{Time: at, Valid: true},
	})
	if err == nil && row.TimeZone != "" {
		return row.TimeZone, nil
	}

	// Fallback to learner_profiles.time_zone
	var tz string
	err = s.pool.QueryRow(ctx, "SELECT time_zone FROM learner_profiles WHERE learner_id = $1", uid).Scan(&tz)
	if err == nil && tz != "" {
		return tz, nil
	}
	return "Asia/Ho_Chi_Minh", nil
}

func (s *pgStore) ScheduleTimezoneChange(ctx context.Context, learnerID, tz string, effectiveAt time.Time) error {
	var uid pgtype.UUID
	if err := uid.Scan(learnerID); err != nil {
		return fmt.Errorf("invalid learner id: %w", err)
	}
	queries := dbsql.New(s.pool)
	_, err := queries.InsertCreditTimezone(ctx, dbsql.InsertCreditTimezoneParams{
		LearnerID:   uid,
		TimeZone:    tz,
		EffectiveAt: pgtype.Timestamptz{Time: effectiveAt, Valid: true},
	})
	return err
}

func (s *pgStore) GetLastDailyGrant(ctx context.Context, learnerID string) (*Entry, error) {
	var uid pgtype.UUID
	if err := uid.Scan(learnerID); err != nil {
		return nil, fmt.Errorf("invalid learner id: %w", err)
	}
	queries := dbsql.New(s.pool)
	row, err := queries.GetLastDailyGrant(ctx, uid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	entry := entryFromDB(row)
	return &entry, nil
}

func (s *pgStore) ListUnexpiredDailyGrants(ctx context.Context, _ time.Time) ([]DailyGrantSummary, error) {
	queries := dbsql.New(s.pool)
	rows, err := queries.ListUnexpiredDailyGrants(ctx)
	if err != nil {
		return nil, err
	}
	res := make([]DailyGrantSummary, len(rows))
	for i, r := range rows {
		res[i] = DailyGrantSummary{
			LearnerID: uuid.UUID(r.LearnerID.Bytes).String(),
			Date:      r.GrantDate,
			Amount:    r.Amount,
			GrantedAt: r.CreatedAt.Time,
		}
	}
	return res, nil
}

func entryFromDB(r dbsql.CreditEntry) Entry {
	e := Entry{
		ID:        r.ID,
		LearnerID: uuid.UUID(r.LearnerID.Bytes).String(),
		Amount:    r.Amount,
		Kind:      r.Kind,
		Reference: r.Reference,
		CreatedAt: r.CreatedAt.Time,
	}
	if r.PricingVersion.Valid {
		e.PricingVersion = r.PricingVersion.String
	}
	if len(r.Provenance) > 0 {
		var prov map[string]any
		if err := json.Unmarshal(r.Provenance, &prov); err == nil {
			e.Provenance = prov
		}
	}
	return e
}

// Service coordinates auditable wallet derivation, daily grants, expiry, caching and activity.
type Service struct {
	store     LedgerStore
	redis     *redis.Client
	catalogue *pricing.Catalogue
	events    analytics.Events
	mux       *http.ServeMux
	mu        sync.Mutex
}

// NewService builds a Service from a Postgres pool and Redis client.
func NewService(pool *pgxpool.Pool, redisClient *redis.Client) *Service {
	return NewServiceWithCatalogue(pool, redisClient, nil, nil)
}

// NewServiceWithCatalogue builds a Service with pricing catalogue and analytics.
func NewServiceWithCatalogue(pool *pgxpool.Pool, redisClient *redis.Client, catalogue *pricing.Catalogue, events analytics.Events) *Service {
	var store LedgerStore
	if pool != nil {
		store = &pgStore{pool: pool}
	}
	s := &Service{
		store:     store,
		redis:     redisClient,
		catalogue: catalogue,
		events:    events,
		mux:       http.NewServeMux(),
	}
	s.registerRoutes()
	return s
}

// NewServiceWithStore builds a Service from an abstract LedgerStore and optional Redis client.
func NewServiceWithStore(store LedgerStore, redisClient *redis.Client) *Service {
	s := &Service{
		store: store,
		redis: redisClient,
		mux:   http.NewServeMux(),
	}
	s.registerRoutes()
	return s
}

// SetStore replaces the store (used in tests).
func (s *Service) SetStore(store LedgerStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store = store
}

func (s *Service) registerRoutes() {
	s.mux.HandleFunc("GET /v1/me/credits", s.handleGetWallet)
	s.mux.HandleFunc("GET /v1/me/credits/activity", s.handleGetActivity)
	s.mux.HandleFunc("GET /v1/credits/wallet", s.handleGetWallet)
	s.mux.HandleFunc("GET /v1/credits/activity", s.handleGetActivity)
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// GetWallet derives or loads cached wallet balances and activity.
func (s *Service) GetWallet(ctx context.Context, learnerID string) (*WalletResponse, error) {
	return s.GetWalletAt(ctx, learnerID, time.Now())
}

// GetWalletAt derives wallet balances as of a specific point in time.
func (s *Service) GetWalletAt(ctx context.Context, learnerID string, at time.Time) (*WalletResponse, error) {
	tz := "Asia/Ho_Chi_Minh"
	if s.store != nil {
		if eff, err := s.store.GetEffectiveTimezone(ctx, learnerID, at); err == nil && eff != "" {
			tz = eff
		}
	}

	cacheKey := fmt.Sprintf("credits:wallet:%s", learnerID)

	// Try reading from cache
	if s.redis != nil {
		if raw, err := s.redis.Get(ctx, cacheKey).Bytes(); err == nil {
			var resp WalletResponse
			if err := json.Unmarshal(raw, &resp); err == nil {
				resp.Cached = true
				return &resp, nil
			}
		}
	}

	if s.store == nil {
		return CalculateWalletAt(nil, tz, at), nil
	}

	// Derive directly from append-only ledger entries
	entries, err := s.store.GetLedgerEntries(ctx, learnerID)
	if err != nil {
		return nil, fmt.Errorf("get ledger entries: %w", err)
	}

	wallet := CalculateWalletAt(entries, tz, at)

	// Cache result for 5 minutes
	if s.redis != nil {
		if payload, err := json.Marshal(wallet); err == nil {
			_ = s.redis.Set(ctx, cacheKey, payload, 5*time.Minute).Err()
		}
	}

	return wallet, nil
}

// EnsureDailyGrant writes the daily free grant idempotently once per learner/local date.
func (s *Service) EnsureDailyGrant(ctx context.Context, learnerID string, now time.Time) (*Entry, error) {
	if s.store == nil {
		return nil, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tz, err := s.store.GetEffectiveTimezone(ctx, learnerID, now)
	if err != nil || tz == "" {
		tz = "Asia/Ho_Chi_Minh"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	localDate := now.In(loc).Format(time.DateOnly)

	// Check if already granted for this local date
	entries, err := s.store.GetLedgerEntries(ctx, learnerID)
	if err == nil {
		for _, e := range entries {
			if e.Kind == "daily_grant" {
				if d, ok := e.Provenance["date"].(string); ok && d == localDate {
					return &e, nil
				}
			}
		}
	}

	// Check anti-farming guard: if previous grant exists, check elapsed time across timezone change
	if !s.canGrantDailyCreditsLocked(ctx, learnerID, now, tz) {
		return nil, nil
	}

	amount := int64(46)
	pricingVersion := "usd-launch-2026-11-08"
	if s.catalogue != nil {
		cfg := s.catalogue.Config()
		if cfg.DailyFree.Credits > 0 {
			amount = int64(cfg.DailyFree.Credits)
		}
		if cfg.Version != "" {
			pricingVersion = cfg.Version
		}
	}

	newEntry := Entry{
		LearnerID:      learnerID,
		Amount:         amount,
		Kind:           "daily_grant",
		Reference:      fmt.Sprintf("daily_grant:%s:%s", learnerID, localDate),
		PricingVersion: pricingVersion,
		Provenance: map[string]any{
			"bucket":   "daily_free",
			"date":     localDate,
			"timezone": tz,
			"activity": "Daily free credits granted",
			"source":   "Daily free",
		},
		CreatedAt: now,
	}

	created, err := s.store.InsertCreditEntry(ctx, newEntry)
	if err != nil {
		if errors.Is(err, ErrDuplicateGrant) {
			// Idempotent: return existing entry
			entries, _ := s.store.GetLedgerEntries(ctx, learnerID)
			for _, e := range entries {
				if e.Kind == "daily_grant" {
					if d, ok := e.Provenance["date"].(string); ok && d == localDate {
						return &e, nil
					}
				}
			}
			return nil, nil
		}
		return nil, err
	}

	_ = s.InvalidateCache(ctx, learnerID)

	if s.events != nil {
		_ = s.events.Capture(learnerID, "daily_credits_granted", map[string]any{
			"amount":      int(amount),
			"source":      "api",
			"platform":    "web",
			"app_version": "0.1.0",
		})
	}

	return &created, nil
}

// ExpireDailyCredits finds past-day unexpired daily grants and expires unreserved remainders.
func (s *Service) ExpireDailyCredits(ctx context.Context, asOf time.Time) (int, error) {
	if s.store == nil {
		return 0, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	summaries, err := s.store.ListUnexpiredDailyGrants(ctx, asOf)
	if err != nil {
		return 0, err
	}

	expiredCount := 0
	pricingVersion := "usd-launch-2026-11-08"
	if s.catalogue != nil && s.catalogue.Config().Version != "" {
		pricingVersion = s.catalogue.Config().Version
	}

	for _, sum := range summaries {
		tz, err := s.store.GetEffectiveTimezone(ctx, sum.LearnerID, asOf)
		if err != nil || tz == "" {
			tz = "Asia/Ho_Chi_Minh"
		}
		loc, err := time.LoadLocation(tz)
		if err != nil {
			loc = time.UTC
		}
		currentLocalDate := asOf.In(loc).Format(time.DateOnly)

		// Only expire if the local date of the grant has already ended
		if sum.Date >= currentLocalDate {
			continue
		}

		// Calculate unreserved remainder for that day's pool
		entries, err := s.store.GetLedgerEntries(ctx, sum.LearnerID)
		if err != nil {
			continue
		}

		var (
			totalGranted int64
			totalSpent   int64
			activeHeld   int64
		)
		reservations := make(map[string]int64)

		for _, e := range entries {
			bucket, _ := e.Provenance["bucket"].(string)
			date, _ := e.Provenance["date"].(string)

			if e.Kind == "daily_grant" && date == sum.Date {
				totalGranted += e.Amount
			} else if bucket == "daily_free" && date == sum.Date {
				switch e.Kind {
				case "spend":
					totalSpent += (-e.Amount)
				case "reserve":
					held := -e.Amount
					reservations[e.Reference] += held
					activeHeld += held
				case "release":
					if held, ok := reservations[e.Reference]; ok && held > 0 {
						activeHeld -= held
						delete(reservations, e.Reference)
					}
				}
			}
		}

		unreservedRemainder := totalGranted - totalSpent - activeHeld
		if unreservedRemainder > 0 {
			expireEntry := Entry{
				LearnerID:      sum.LearnerID,
				Amount:         -unreservedRemainder,
				Kind:           "expire",
				Reference:      fmt.Sprintf("daily_expire:%s:%s", sum.LearnerID, sum.Date),
				PricingVersion: pricingVersion,
				Provenance: map[string]any{
					"bucket":   "daily_free",
					"date":     sum.Date,
					"timezone": tz,
					"activity": "Expired · daily free",
					"source":   "Daily free",
				},
				CreatedAt: asOf,
			}

			_, err := s.store.InsertCreditEntry(ctx, expireEntry)
			if err == nil {
				expiredCount++
				_ = s.InvalidateCache(ctx, sum.LearnerID)
				if s.events != nil {
					_ = s.events.Capture(sum.LearnerID, "daily_credits_expired", map[string]any{
						"amount":      int(unreservedRemainder),
						"source":      "api",
						"platform":    "web",
						"app_version": "0.1.0",
					})
				}
			}
		}
	}

	return expiredCount, nil
}

// UpdateTimezone schedules a timezone change to take effect after 24 hours.
func (s *Service) UpdateTimezone(ctx context.Context, learnerID, tz string, requestedAt time.Time) error {
	if _, err := time.LoadLocation(tz); err != nil {
		return fmt.Errorf("invalid IANA timezone %q: %w", tz, err)
	}

	effectiveAt := requestedAt.Add(24 * time.Hour)
	if err := s.store.ScheduleTimezoneChange(ctx, learnerID, tz, effectiveAt); err != nil {
		return fmt.Errorf("schedule timezone change: %w", err)
	}

	_ = s.InvalidateCache(ctx, learnerID)
	return nil
}

// GetEffectiveTimezone returns the active timezone at the specified time.
func (s *Service) GetEffectiveTimezone(ctx context.Context, learnerID string, at time.Time) (string, error) {
	if s.store == nil {
		return "Asia/Ho_Chi_Minh", nil
	}
	return s.store.GetEffectiveTimezone(ctx, learnerID, at)
}

// CanGrantDailyCredits verifies anti-farming rules: across timezone changes, at least 24h must elapse.
func (s *Service) CanGrantDailyCredits(ctx context.Context, learnerID string, at time.Time, currentTz string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.canGrantDailyCreditsLocked(ctx, learnerID, at, currentTz)
}

func (s *Service) canGrantDailyCreditsLocked(ctx context.Context, learnerID string, at time.Time, currentTz string) bool {
	lastGrant, err := s.store.GetLastDailyGrant(ctx, learnerID)
	if err != nil || lastGrant == nil {
		return true
	}

	lastTz, _ := lastGrant.Provenance["timezone"].(string)
	if lastTz != "" && lastTz != currentTz {
		if at.Sub(lastGrant.CreatedAt) < 24*time.Hour {
			return false
		}
	}
	return true
}

// IsActionEligibleForDailyFree checks if an action key may draw from daily free credits.
func (s *Service) IsActionEligibleForDailyFree(action string) bool {
	if s.catalogue == nil {
		return action == "quick_note" || action == "tutor_short" || action == "tutor_long"
	}
	for _, el := range s.catalogue.Config().DailyFree.EligibleActions {
		if el == action {
			return true
		}
	}
	return false
}

// InvalidateCache invalidates the learner's cached wallet.
func (s *Service) InvalidateCache(ctx context.Context, learnerID string) error {
	if s.redis == nil {
		return nil
	}
	return s.redis.Del(ctx, fmt.Sprintf("credits:wallet:%s", learnerID)).Err()
}

// ListActivity returns cursor-paginated activity for the learner.
func (s *Service) ListActivity(ctx context.Context, learnerID string, limit int, cursor string) (*ActivityPageDTO, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	var beforeID int64
	if cursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return nil, errors.New("invalid cursor")
		}
		id, err := strconv.ParseInt(string(decoded), 10, 64)
		if err != nil || id <= 0 {
			return nil, errors.New("invalid cursor")
		}
		beforeID = id
	}

	// Fetch limit + 1 to check if there is a next page
	entries, err := s.store.ListLedgerActivity(ctx, learnerID, limit+1, beforeID)
	if err != nil {
		return nil, fmt.Errorf("list ledger activity: %w", err)
	}

	hasNext := len(entries) > limit
	if hasNext {
		entries = entries[:limit]
	}

	items := make([]ActivityItemDTO, len(entries))
	for i, e := range entries {
		items[i] = formatActivityItem(e)
	}

	nextCursor := ""
	if hasNext && len(items) > 0 {
		lastID := items[len(items)-1].ID
		nextCursor = base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(lastID, 10)))
	}

	return &ActivityPageDTO{
		Entries:    items,
		NextCursor: nextCursor,
	}, nil
}

// CalculateWallet derives bucket totals and recent activity from raw ledger entries.
func CalculateWallet(entries []Entry, tz string) *WalletResponse {
	return CalculateWalletAt(entries, tz, time.Now())
}

// CalculateWalletAt derives bucket totals and recent activity as of a specific point in time.
func CalculateWalletAt(entries []Entry, tz string, at time.Time) *WalletResponse {
	var (
		dailyFreeGranted   int64 = 46
		dailyFreeAvailable int64
		purchasedAvailable int64
		subscriptionTotal  int64
		reservedTotal      int64
	)

	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	now := at.In(loc)
	currentLocalDate := now.Format(time.DateOnly)

	// Reservations map by reference: tracks net reserved amounts
	reservations := make(map[string]int64)

	// In reverse order (newest first for recent activity)
	activityItems := make([]ActivityItemDTO, 0, len(entries))

	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		activityItems = append(activityItems, formatActivityItem(e))
	}

	for _, e := range entries {
		bucket := ""
		date := ""
		if e.Provenance != nil {
			if b, ok := e.Provenance["bucket"].(string); ok {
				bucket = b
			}
			if d, ok := e.Provenance["date"].(string); ok {
				date = d
			}
		}

		switch e.Kind {
		case "daily_grant":
			dailyFreeGranted = e.Amount
			dailyFreeAvailable += e.Amount

		case "expire":
			// Expire entries remove unspent daily free
			dailyFreeAvailable += e.Amount
			if dailyFreeAvailable < 0 {
				dailyFreeAvailable = 0
			}

		case "purchase":
			purchasedAvailable += e.Amount

		case "subscription_grant":
			subscriptionTotal += e.Amount

		case "refund":
			if bucket == "subscription" {
				subscriptionTotal += e.Amount
			} else {
				purchasedAvailable += e.Amount
			}

		case "reserve":
			held := -e.Amount
			reservations[e.Reference] += held
			reservedTotal += held

			switch bucket {
			case "daily_free":
				dailyFreeAvailable += e.Amount
			case "subscription":
				subscriptionTotal += e.Amount
			default:
				purchasedAvailable += e.Amount
			}

		case "release":
			if held, ok := reservations[e.Reference]; ok && held > 0 {
				reservedTotal -= held
				delete(reservations, e.Reference)
			}
			switch bucket {
			case "daily_free":
				// Released expired allocation never becomes spendable tomorrow
				if date == "" || date == currentLocalDate {
					dailyFreeAvailable += e.Amount
				}
			case "subscription":
				subscriptionTotal += e.Amount
			default:
				purchasedAvailable += e.Amount
			}

		case "spend":
			switch bucket {
			case "daily_free":
				dailyFreeAvailable += e.Amount
			case "subscription":
				subscriptionTotal += e.Amount
			default:
				purchasedAvailable += e.Amount
			}

		case "reward":
			// Historical reward entries stay readable in activity but are unspendable.
		}
	}

	if dailyFreeAvailable < 0 {
		dailyFreeAvailable = 0
	}
	if dailyFreeAvailable > dailyFreeGranted {
		dailyFreeAvailable = dailyFreeGranted
	}
	if purchasedAvailable < 0 {
		purchasedAvailable = 0
	}
	if subscriptionTotal < 0 {
		subscriptionTotal = 0
	}
	if reservedTotal < 0 {
		reservedTotal = 0
	}

	available := dailyFreeAvailable + purchasedAvailable + subscriptionTotal

	// Next local day reset (midnight in tz)
	nextReset := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, loc)

	var reservedNote string
	if reservedTotal > 0 {
		reservedNote = fmt.Sprintf("%d reserved for a lesson being prepared", reservedTotal)
	}

	// Limit recent activity to 10 for the wallet overview
	recent := activityItems
	if len(recent) > 10 {
		recent = recent[:10]
	}

	return &WalletResponse{
		Available: available,
		DailyFree: DailyFreeSummary{
			Available: dailyFreeAvailable,
			Granted:   dailyFreeGranted,
			ResetsAt:  nextReset,
			Timezone:  tz,
		},
		Purchased:      purchasedAvailable,
		Subscription:   subscriptionTotal,
		Reserved:       reservedTotal,
		ReservedNote:   reservedNote,
		RecentActivity: recent,
	}
}

func formatActivityItem(e Entry) ActivityItemDTO {
	source := "Purchased"
	if e.Provenance != nil {
		if s, ok := e.Provenance["source"].(string); ok && s != "" {
			source = s
		} else if b, ok := e.Provenance["bucket"].(string); ok && b != "" {
			switch b {
			case "daily_free":
				source = "Daily free"
			case "subscription":
				source = "Plus plan"
			case "purchase":
				source = "Purchased"
			}
		}
	}

	var activity string
	var detail string
	if e.Provenance != nil {
		if act, ok := e.Provenance["activity"].(string); ok && act != "" {
			activity = act
		}
		if d, ok := e.Provenance["detail"].(string); ok && d != "" {
			detail = d
		}
	}

	if activity == "" {
		switch e.Kind {
		case "reserve":
			activity = "Reserved · " + cleanRef(e.Reference)
		case "release":
			activity = "Released · unused reserve"
		case "spend":
			activity = "Settled · " + cleanRef(e.Reference)
		case "purchase":
			activity = "Purchase · " + cleanRef(e.Reference)
		case "daily_grant":
			activity = "Daily free credits granted"
			source = "Daily free"
		case "expire":
			activity = "Expired · daily free"
			source = "Daily free"
		case "reward":
			activity = "Streak reward (historical)"
			source = "Reward"
		default:
			activity = e.Kind + " · " + cleanRef(e.Reference)
		}
	}

	amountDisplay := ""
	if e.Kind == "reserve" {
		amountDisplay = fmt.Sprintf("−%d held", abs(e.Amount))
	} else if e.Amount > 0 {
		amountDisplay = fmt.Sprintf("+%d", e.Amount)
	} else {
		amountDisplay = fmt.Sprintf("−%d", abs(e.Amount))
	}

	dateDisplay := formatDate(e.CreatedAt)
	if e.Provenance != nil {
		if d, ok := e.Provenance["date_display"].(string); ok && d != "" {
			dateDisplay = d
		}
	}

	return ActivityItemDTO{
		ID:            e.ID,
		Date:          dateDisplay,
		Activity:      activity,
		Detail:        detail,
		Source:        source,
		AmountDisplay: amountDisplay,
		Amount:        e.Amount,
		Kind:          e.Kind,
	}
}

func cleanRef(ref string) string {
	if strings.HasPrefix(ref, "ref-") {
		return strings.TrimPrefix(ref, "ref-")
	}
	return ref
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

func formatDate(t time.Time) string {
	if t.IsZero() {
		return "Today"
	}
	return t.Format("Jan 2 15:04")
}

func (s *Service) handleGetWallet(w http.ResponseWriter, r *http.Request) {
	learnerID := extractLearnerID(r)
	if learnerID == "" {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign in required"})
		return
	}

	if s.store != nil {
		_, _ = s.EnsureDailyGrant(r.Context(), learnerID, time.Now())
		_, _ = s.ExpireDailyCredits(r.Context(), time.Now())
	}

	wallet, err := s.GetWallet(r.Context(), learnerID)
	if err != nil {
		slog.ErrorContext(r.Context(), "get wallet failed", "error", err, "learner_id", learnerID)
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to read wallet"})
		return
	}

	respondJSON(w, http.StatusOK, wallet)
}

func (s *Service) handleGetActivity(w http.ResponseWriter, r *http.Request) {
	learnerID := extractLearnerID(r)
	if learnerID == "" {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign in required"})
		return
	}

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	cursor := r.URL.Query().Get("cursor")
	page, err := s.ListActivity(r.Context(), learnerID, limit, cursor)
	if err != nil {
		slog.ErrorContext(r.Context(), "get activity failed", "error", err, "learner_id", learnerID)
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, page)
}

func extractLearnerID(r *http.Request) string {
	if id, ok := accounts.LearnerIDFromContext(r.Context()); ok {
		return id
	}
	for _, key := range []any{"learner_id", "learnerID", "learner"} {
		if val, ok := r.Context().Value(key).(string); ok && val != "" {
			return val
		}
	}
	if val := r.Header.Get("X-Learner-ID"); val != "" {
		return val
	}
	return ""
}

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
