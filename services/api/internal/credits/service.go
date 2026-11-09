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
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	dbsql "github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

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

// LedgerStore abstracts access to the append-only credit ledger.
type LedgerStore interface {
	GetLedgerEntries(ctx context.Context, learnerID string) ([]Entry, error)
	ListLedgerActivity(ctx context.Context, learnerID string, limit int, beforeID int64) ([]Entry, error)
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

// Service coordinates auditable wallet derivation, caching and cursor-paginated activity.
type Service struct {
	store LedgerStore
	redis *redis.Client
	mux   *http.ServeMux
}

// NewService builds a Service from a Postgres pool and Redis client.
func NewService(pool *pgxpool.Pool, redisClient *redis.Client) *Service {
	return NewServiceWithStore(&pgStore{pool: pool}, redisClient)
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

	// Derive directly from append-only ledger entries
	entries, err := s.store.GetLedgerEntries(ctx, learnerID)
	if err != nil {
		return nil, fmt.Errorf("get ledger entries: %w", err)
	}

	wallet := CalculateWallet(entries, "Asia/Ho_Chi_Minh")

	// Cache result for 5 minutes
	if s.redis != nil {
		if payload, err := json.Marshal(wallet); err == nil {
			_ = s.redis.Set(ctx, cacheKey, payload, 5*time.Minute).Err()
		}
	}

	return wallet, nil
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
	var (
		dailyFreeGranted   int64 = 46
		dailyFreeAvailable int64
		purchasedAvailable int64
		subscriptionTotal  int64
		reservedTotal      int64
	)

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
		if e.Provenance != nil {
			if b, ok := e.Provenance["bucket"].(string); ok {
				bucket = b
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
			// Refunds add back to the attributed bucket
			if bucket == "subscription" {
				subscriptionTotal += e.Amount
			} else {
				purchasedAvailable += e.Amount
			}

		case "reserve":
			// Reserve holds funds (e.Amount is negative)
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
			// Release finishes the reservation (returns unused or full estimate)
			if held, ok := reservations[e.Reference]; ok && held > 0 {
				reservedTotal -= held
				delete(reservations, e.Reference)
			}
			switch bucket {
			case "daily_free":
				dailyFreeAvailable += e.Amount
			case "subscription":
				subscriptionTotal += e.Amount
			default:
				purchasedAvailable += e.Amount
			}

		case "spend":
			// Actual settled spend (e.Amount is negative)
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
			// Do NOT add to available, purchased or any other bucket.
		}
	}

	if dailyFreeAvailable < 0 {
		dailyFreeAvailable = 0
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

	// Default next local day reset (midnight Asia/Ho_Chi_Minh or UTC)
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
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
