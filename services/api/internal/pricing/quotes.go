package pricing

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ModelRate stores integer USD nanodollars per token, including cache classes.
type ModelRate struct {
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	ContextMax int64  `json:"context_max"`
	Input      int64  `json:"input"`
	Output     int64  `json:"output"`
	CacheWrite int64  `json:"cache_write"`
	CacheRead  int64  `json:"cache_read"`
}

// QuotePolicy is immutable Pricing config data owned by its version.
type QuotePolicy struct {
	FreeConcurrency           int64                `json:"free_concurrency"`
	MaxAttempts               int64                `json:"max_attempts"`
	FallbackModels            map[string]string    `json:"fallback_models"`
	TTLSeconds                int64                `json:"ttl_seconds"`
	USDNanosPerCredit         int64                `json:"usd_nanos_per_credit"`
	AudioUSDNanosPerCharacter int64                `json:"audio_usd_nanos_per_character"`
	AudioModel                string               `json:"audio_model"`
	Voices                    []string             `json:"voices"`
	ActionModels              map[string]string    `json:"action_models"`
	Models                    map[string]ModelRate `json:"models"`
}

// QuoteInput contains counts and allowlisted identifiers, never learner text.
type QuoteInput struct {
	OutlineDraftID  string `json:"outline_draft_id,omitempty"`
	OutlineRevision int64  `json:"outline_revision,omitempty"`
	RequestHash     string `json:"request_hash,omitempty"`
	BriefRevision   string `json:"brief_revision,omitempty"`
	Action          string `json:"action"`
	Model           string `json:"model,omitempty"`
	Voice           string `json:"voice,omitempty"`
	InputTokens     int64  `json:"input_tokens,omitempty"`
	OutputTokens    int64  `json:"output_tokens,omitempty"`
	ContextTokens   int64  `json:"context_tokens,omitempty"`
	// LargeContext records the Learner's explicit approval of a tutor context above the standard
	// cached-context bound; the input bound then follows the quoted context, never more than prompt_tokens.
	LargeContext        bool  `json:"large_context,omitempty"`
	NarrationCharacters int64 `json:"narration_characters,omitempty"`
}

// Quote is an authoritative component ceiling; accepting it does not reserve funds.
type Quote struct {
	ID                   string           `json:"id"`
	PricingVersion       string           `json:"pricing_version"`
	Action               string           `json:"action"`
	Currency             string           `json:"currency"`
	ContentMinimum       int64            `json:"content_minimum"`
	ContentCeiling       int64            `json:"content_ceiling"`
	AudioCeiling         int64            `json:"audio_ceiling"`
	DailyFreeEligible    bool             `json:"daily_free_eligible"`
	Input                QuoteInput       `json:"input"`
	Bounds               map[string]int64 `json:"bounds"`
	Rate                 ModelRate        `json:"rate"`
	Policy               QuotePolicy      `json:"policy"`
	NarrationNumerator   int64            `json:"narration_numerator"`
	NarrationDenominator int64            `json:"narration_denominator"`
	CreatedAt            time.Time        `json:"created_at"`
	ExpiresAt            time.Time        `json:"expires_at"`
	Accepted             bool             `json:"accepted"`
	Expired              bool             `json:"expired"`
}

var errQuote = errors.New("quote unavailable: invalid action, bounds or missing rate")

// CalculateQuote rounds the aggregate narration amount once, using integers only.
func CalculateQuote(c Config, p QuotePolicy, in QuoteInput, now time.Time) (Quote, error) {
	q := Quote{PricingVersion: c.Version, Action: in.Action, Currency: c.Currency, Input: in, Policy: p, CreatedAt: now, Bounds: map[string]int64{}}
	action, ok := c.Actions[in.Action]
	if !ok || in.InputTokens < 0 || in.OutputTokens < 0 || in.ContextTokens < 0 || in.NarrationCharacters < 0 || p.TTLSeconds <= 0 || p.TTLSeconds > 86400 || p.USDNanosPerCredit <= 0 {
		return q, errQuote
	}
	var limits map[string]json.RawMessage
	if json.Unmarshal(c.Limits, &limits) != nil {
		return q, errQuote
	}
	key := in.Action
	if key == "narration" {
		key = "lesson_content"
	}
	if key == "tutor_short" || key == "tutor_long" {
		key = "tutor"
	}
	if key != "cached_replay" && json.Unmarshal(limits[key], &q.Bounds) != nil {
		if key == "setup_chat" {
			q.Bounds = map[string]int64{"input_tokens": 2000, "output_tokens": 1000}
		} else {
			return q, errQuote
		}
	}
	maxInput, maxOutput := q.Bounds["input_tokens"], q.Bounds["output_tokens"]
	if key == "tutor" {
		class := "short"
		if in.Action == "tutor_long" {
			class = "long"
		}
		maxInput, maxOutput = q.Bounds[class+"_input_tokens"], q.Bounds[class+"_output_tokens"]
		if in.ContextTokens > q.Bounds["prompt_tokens"]-in.InputTokens || (in.ContextTokens > q.Bounds["cached_context_tokens"] && !in.LargeContext) {
			return q, errQuote
		}
	} else if in.ContextTokens > 0 || in.LargeContext {
		return q, errQuote
	}
	q.Bounds["input_tokens"], q.Bounds["output_tokens"] = maxInput, maxOutput
	if key == "tutor" {
		q.Bounds["input_tokens"] += q.Bounds["cached_context_tokens"]
		if in.LargeContext {
			q.Bounds["input_tokens"] = maxInput + max(q.Bounds["cached_context_tokens"], in.ContextTokens)
		}
	}
	if in.InputTokens > maxInput || in.OutputTokens > maxOutput {
		return q, errQuote
	}
	if in.Action == "cached_replay" && in.Model != "" {
		return q, errQuote
	}
	if in.Action == "narration" {
		if in.InputTokens != 0 || in.OutputTokens != 0 || (in.Model != "" && in.Model != p.AudioModel) {
			return q, errQuote
		}
		in.Model = p.AudioModel
	}
	if in.Model == "" {
		in.Model = p.ActionModels[in.Action]
		if in.Model == "" && (in.Action == "setup_chat" || in.Action == "course_brief_chat" || in.Action == "outline_chat_change") {
			in.Model = "haiku"
		}
	}
	if in.Action != "cached_replay" && in.Action != "narration" {
		rate, found := p.Models[in.Model]
		if !found || rate.Provider == "" || rate.Model == "" || rate.Input <= 0 || rate.Output <= 0 || rate.CacheWrite <= 0 || rate.CacheRead <= 0 || rate.ContextMax <= 0 || in.InputTokens > rate.ContextMax-in.ContextTokens {
			return q, errQuote
		}
		// An action may only use its evaluated, configured model bracket.
		expectedModel := p.ActionModels[in.Action]
		if expectedModel == "" && (in.Action == "setup_chat" || in.Action == "course_brief_chat" || in.Action == "outline_chat_change") {
			expectedModel = "haiku"
		}
		if in.Model != expectedModel {
			return q, errQuote
		}
		q.Rate = rate
	}
	if in.NarrationCharacters > 0 {
		if (in.Action != "lesson_content" && in.Action != "narration") || in.NarrationCharacters > q.Bounds["narration_characters"] || c.Narration.Characters <= 0 || c.Narration.Credits < 0 || c.Narration.Credits > 1000000000 || in.NarrationCharacters > 1000000000 || p.AudioModel == "" || p.AudioUSDNanosPerCharacter <= 0 {
			return q, errQuote
		}
		if in.Voice == "" && len(p.Voices) > 0 {
			in.Voice = p.Voices[0]
		}
		if !slices.Contains(p.Voices, in.Voice) {
			return q, errQuote
		}
		amount := in.NarrationCharacters * int64(c.Narration.Credits)
		q.AudioCeiling = amount / int64(c.Narration.Characters)
		if amount%int64(c.Narration.Characters) != 0 {
			q.AudioCeiling++
		}
	} else if in.Voice != "" || in.Action == "narration" {
		return q, errQuote
	}
	q.ContentCeiling = int64(action.Credits)
	q.ContentMinimum = int64(action.Credits)
	if in.Action == "narration" {
		q.ContentCeiling = 0
	}
	var operational struct {
		Job           float64 `json:"generation_usd_per_request"`
		Free          float64 `json:"free_learner_usd_per_local_day"`
		Paid          float64 `json:"paid_learner_usd_per_local_day"`
		Platform      float64 `json:"platform_usd_per_day"`
		Deadline      int64   `json:"generation_deadline_seconds"`
		ChatDeadline  int64   `json:"tutor_note_deadline_seconds"`
		AudioDeadline int64   `json:"tts_deadline_seconds"`
		Concurrency   int64   `json:"free_concurrent_generation"`
	}
	if json.Unmarshal(c.Limits, &operational) != nil {
		return q, errQuote
	}
	q.Bounds["max_cost_nanos"] = int64(operational.Job * 1e9)
	q.Bounds["free_day_nanos"] = int64(operational.Free * 1e9)
	q.Bounds["paid_day_nanos"] = int64(operational.Paid * 1e9)
	q.Bounds["platform_day_nanos"] = int64(operational.Platform * 1e9)
	q.Bounds["concurrency"] = operational.Concurrency
	if q.Bounds["concurrency"] == 0 {
		q.Bounds["concurrency"] = p.FreeConcurrency
	}
	q.Bounds["max_attempts"] = p.MaxAttempts
	deadline := operational.Deadline
	if key == "tutor" || key == "quick_note" {
		deadline = operational.ChatDeadline
	}
	if in.Action == "narration" {
		deadline = operational.AudioDeadline
	}
	q.Bounds["timeout_ms"] = deadline * 1000
	if in.Action == "outline_chat_change" || in.Action == "character_create" {
		var feature map[string]int64
		if json.Unmarshal(limits[key], &feature) != nil || feature["timeout_ms"] <= 0 || feature["timeout_ms"] >= q.Bounds["timeout_ms"] || feature["max_attempts"] <= 0 || feature["max_attempts"] > p.MaxAttempts || feature["max_cost_nanos"] <= 0 {
			return q, errQuote
		}
		q.Bounds["timeout_ms"] = feature["timeout_ms"]
		if in.Action == "character_create" {
			if feature["output_tokens"] > 2000 || feature["max_attempts"] > 2 || feature["timeout_ms"] > 90000 || feature["screen_input_tokens"] > 18000 || feature["screen_output_tokens"] > 200 || feature["screen_timeout_ms"] > 10000 || feature["screen_max_attempts"] > 2 {
				return q, errQuote
			}
			for _, key := range []string{"count", "day_attempts", "minute_attempts", "prompt_chars", "screen_input_tokens", "screen_output_tokens", "screen_timeout_ms", "screen_max_attempts"} {
				if feature[key] <= 0 {
					return q, errQuote
				}
				q.Bounds[key] = feature[key]
			}
		}
		q.Bounds["max_attempts"] = feature["max_attempts"]
		q.Bounds["max_cost_nanos"] = min(q.Bounds["max_cost_nanos"], feature["max_cost_nanos"])
	}
	q.Input = in
	q.Bounds["narration_characters"] = in.NarrationCharacters
	q.NarrationNumerator = int64(c.Narration.Credits)
	q.NarrationDenominator = int64(c.Narration.Characters)
	q.DailyFreeEligible = slices.Contains(c.DailyFree.EligibleActions, in.Action)
	q.ExpiresAt = now.Add(time.Duration(p.TTLSeconds) * time.Second)
	return q, nil
}

// Quotes persists original terms and consent independently of wallet operations.
type Quotes struct {
	pool    *pgxpool.Pool
	env     string
	ownerID string
}

// NewQuotes constructs the database-backed quote service.
func NewQuotes(pool *pgxpool.Pool) *Quotes {
	return &Quotes{
		pool:    pool,
		env:     os.Getenv("DUOLIFY_ENV"),
		ownerID: os.Getenv("PRICING_OWNER_ID"),
	}
}

// SetEnvironment configures the deployment environment for decision gate checks.
func (s *Quotes) SetEnvironment(env string) {
	s.env = env
}

// SetOwnerID configures the pricing owner ID for decision gate checks.
func (s *Quotes) SetOwnerID(ownerID string) {
	s.ownerID = ownerID
}

// Create resolves database time and snapshots the active version.
func (s *Quotes) Create(ctx context.Context, owner string, in QuoteInput) (Quote, error) {
	return s.createVersion(ctx, owner, in, "")
}

// CreateDevelopmentOutline quotes only the explicitly unapproved development action.
// The caller must gate this server-only path by the deployment environment.
func (s *Quotes) CreateDevelopmentOutline(ctx context.Context, owner string, in QuoteInput) (Quote, error) {
	if in.Action != "outline_chat_change" {
		return Quote{}, errQuote
	}
	// Once an activated version carries the keys, resolve through it:
	if q, err := s.Create(ctx, owner, in); err == nil {
		return q, nil
	}
	// Fall back to dev version only in dev/test for environments without an activated version:
	if !IsDevOrTest(s.env) {
		return Quote{}, errQuote
	}
	var activatedEver bool
	if s.pool != nil {
		_ = s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pricing_activation_events e JOIN pricing_versions v ON v.version=e.version WHERE v.config->'actions' ? 'outline_chat_change')").Scan(&activatedEver)
	}
	if activatedEver {
		return Quote{}, errQuote
	}
	return s.createVersion(ctx, owner, in, "usd-dev-outline-chat-unapproved")
}

// CreateDevelopmentCharacter uses only the explicitly UNAPPROVED proposal.
// Callers must gate this by their server-owned environment.
func (s *Quotes) CreateDevelopmentCharacter(ctx context.Context, owner string, in QuoteInput) (Quote, error) {
	if in.Action != "character_create" {
		return Quote{}, errQuote
	}
	return s.createVersion(ctx, owner, in, "usd-dev-character-create-unapproved")
}

func (s *Quotes) createVersion(ctx context.Context, owner string, in QuoteInput, version string) (Quote, error) {
	queries := db.New(s.pool)
	if in.Action == "outline_chat_change" || in.Action == "outline" {
		var exists bool
		if len(in.RequestHash) != 64 || in.OutlineRevision < 1 || s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM outline_drafts d JOIN course_brief_revisions b ON b.id=d.brief_revision WHERE d.id::text=$1 AND d.learner_id::text=$2 AND d.revision=$3 AND b.expires_at>now())`, in.OutlineDraftID, owner, in.OutlineRevision).Scan(&exists) != nil || !exists {
			return Quote{}, errQuote
		}
	} else if in.OutlineDraftID != "" || in.OutlineRevision != 0 || in.RequestHash != "" {
		return Quote{}, errQuote
	}
	if in.BriefRevision != "" {
		if in.Action != "preview" {
			return Quote{}, errQuote
		}
		if _, err := queries.GetCourseBrief(ctx, db.GetCourseBriefParams{ID: in.BriefRevision, LearnerID: owner}); err != nil {
			return Quote{}, errQuote
		}
	}
	row, err := queries.ActiveQuoteConfig(ctx)
	if version != "" {
		err = s.pool.QueryRow(ctx, `SELECT v.config,p.policy,now() FROM pricing_versions v JOIN pricing_quote_policies p ON p.version=v.version WHERE v.version=$1 AND NOT v.approved`, version).Scan(&row.Config, &row.Policy, &row.QuotedAt)
	}
	if err != nil {
		return Quote{}, err
	}
	c, err := Parse(row.Config)
	if err != nil {
		return Quote{}, err
	}
	if IsDesignChatAction(in.Action) {
		if _, ok := c.Actions[in.Action]; !ok {
			return Quote{}, errQuote
		}
		if version == "" {
			var reviewer string
			if !IsDevOrTest(s.env) {
				_ = s.pool.QueryRow(ctx, "SELECT coalesce((SELECT reviewed_by FROM pricing_drafts WHERE version=$1),'')", c.Version).Scan(&reviewer)
			}
			if err := CheckDesignChatDecision(c, s.env, reviewer, s.ownerID); err != nil {
				return Quote{}, errQuote
			}
		}
	}
	var policy QuotePolicy
	if err = json.Unmarshal(row.Policy, &policy); err != nil {
		return Quote{}, err
	}
	quote, err := CalculateQuote(c, policy, in, row.QuotedAt.Time)
	if err != nil {
		return Quote{}, err
	}
	raw, err := json.Marshal(quote)
	if err != nil {
		return Quote{}, err
	}
	var learner pgtype.UUID
	if err = learner.Scan(owner); err != nil {
		return Quote{}, err
	}
	quote.ID, err = queries.InsertQuote(ctx, db.InsertQuoteParams{LearnerID: learner, OwnerReference: fmt.Sprintf("%x", sha256.Sum256([]byte(owner))), PricingVersion: c.Version, Snapshot: raw, CreatedAt: pgtype.Timestamptz{Time: quote.CreatedAt, Valid: true}, ExpiresAt: pgtype.Timestamptz{Time: quote.ExpiresAt, Valid: true}})
	return quote, err
}

// Bounds returns the active configuration's bounds for an action without persisting a quote.
func (s *Quotes) Bounds(ctx context.Context, in QuoteInput) (map[string]int64, error) {
	row, err := db.New(s.pool).ActiveQuoteConfig(ctx)
	if err != nil {
		return nil, err
	}
	c, err := Parse(row.Config)
	if err != nil {
		return nil, err
	}
	var policy QuotePolicy
	if err = json.Unmarshal(row.Policy, &policy); err != nil {
		return nil, err
	}
	q, err := CalculateQuote(c, policy, in, row.QuotedAt.Time)
	return q.Bounds, err
}

// Get reads original terms for the authenticated owner.
func (s *Quotes) Get(ctx context.Context, owner, id string) (Quote, error) {
	return ReadQuote(ctx, s.pool, owner, id)
}

// ReadQuote reads pinned terms through the caller's pool or transaction.
func ReadQuote(ctx context.Context, conn db.DBTX, owner, id string) (Quote, error) {
	row, err := db.New(conn).GetQuote(ctx, db.GetQuoteParams{ID: quoteUUID(id), LearnerID: quoteUUID(owner)})
	if err != nil {
		return Quote{}, err
	}
	var q Quote
	if err = json.Unmarshal(row.Snapshot, &q); err != nil {
		return q, err
	}
	q.ID = id
	q.Accepted = row.Accepted
	q.Expired = row.Expired
	return q, nil
}

// Accept records consent only while the quote remains valid.
func (s *Quotes) Accept(ctx context.Context, owner, id string) error {
	count, err := db.New(s.pool).AcceptQuote(ctx, db.AcceptQuoteParams{ID: quoteUUID(id), LearnerID: quoteUUID(owner)})
	if err != nil {
		return err
	}
	if count == 0 {
		q, err := s.Get(ctx, owner, id)
		if err != nil || !q.Accepted || q.Expired {
			return errQuote
		}
	}
	return nil
}

// ServeHTTP handles authenticated quote creation, readback and consent.
func (s *Quotes) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	owner := accounts.LearnerID(r.Context())
	if owner == "" {
		http.Error(w, `{"error":"sign in required"}`, http.StatusUnauthorized)
		return
	}
	var result any
	var err error
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/quotes":
		var in QuoteInput
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&in) != nil {
			http.Error(w, `{"error":"invalid quote input"}`, 400)
			return
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			http.Error(w, `{"error":"invalid quote input"}`, 400)
			return
		}
		result, err = s.Create(r.Context(), owner, in)
	case r.Method == http.MethodGet:
		result, err = s.Get(r.Context(), owner, r.PathValue("id"))
	case r.Method == http.MethodPost:
		err = s.Accept(r.Context(), owner, r.PathValue("id"))
		result = map[string]bool{"accepted": true}
	default:
		w.WriteHeader(405)
		return
	}
	if err != nil {
		code := 503
		if errors.Is(err, errQuote) {
			code = 409
		} else if r.Method == http.MethodGet {
			code = 404
		}
		http.Error(w, `{"error":"Quote unavailable or expired; renew quote and review terms."}`, code)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

func quoteUUID(id string) pgtype.UUID { var value pgtype.UUID; _ = value.Scan(id); return value }
