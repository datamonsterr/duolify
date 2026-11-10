package characters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Mapper is the bounded, stateless prompt-to-closed-spec RPC.
type Mapper interface {
	MapCharacter(context.Context, *pb.MapCharacterRequest) (*pb.MapCharacterResponse, error)
}
type creation struct {
	wallet      *credits.Service
	mapper      Mapper
	environment string
}

// ConfigureCreation injects a server-owned environment, ledger and mapper.
func (s *Service) ConfigureCreation(wallet *credits.Service, mapper Mapper, environment string) {
	s.creation = &creation{wallet, mapper, environment}
}

// CreateRequest identifies one learner intent and an optional pinned regeneration.
type CreateRequest struct {
	RequestKey      string `json:"request_key"`
	Prompt          string `json:"prompt"`
	CharacterID     string `json:"character_id,omitempty"`
	ExpectedVersion int    `json:"expected_version,omitempty"`
}

// CreateJob is the content-free job state plus an approved result and quote.
type CreateJob struct {
	ID            string         `json:"id"`
	State         string         `json:"state"`
	Reason        string         `json:"reason_code"`
	Operation     string         `json:"operation"`
	ReservationID string         `json:"reservation_id,omitempty"`
	Quote         *pricing.Quote `json:"quote,omitempty"`
	Character     *Record        `json:"character,omitempty"`
	Proposal      bool           `json:"proposal"`
}

// CreateAvailability exposes the fail-closed pricing gate.
type CreateAvailability struct {
	Enabled  bool   `json:"enabled"`
	Reason   string `json:"reason_code"`
	Proposal bool   `json:"proposal"`
}

var errCreationDisabled = errors.New("creation_disabled")

// ValidCreatePrompt counts Unicode characters and rejects executable/control input.
func ValidCreatePrompt(prompt string) bool {
	n := utf8.RuneCountInString(prompt)
	return utf8.ValidString(prompt) && n >= 1 && n <= 1500 && strings.TrimSpace(prompt) != "" && !strings.ContainsAny(prompt, "<>") && !strings.ContainsFunc(prompt, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r' })
}

// CreateTerminal identifies durable outcomes that cannot invoke another mapper.
func CreateTerminal(state string) bool {
	return state == "succeeded" || state == "failed" || state == "rejected" || state == "cancelled"
}
func (s *Service) creationConfig(ctx context.Context) (pricing.Config, pricing.QuotePolicy, error) {
	var c pricing.Config
	var p pricing.QuotePolicy
	if s.creation == nil || s.creation.wallet == nil || s.creation.mapper == nil || s.screener == nil {
		return c, p, errCreationDisabled
	}
	row, err := db.New(s.pool).ActiveQuoteConfig(ctx)
	if s.creation.environment == "development" || s.creation.environment == "test" {
		err = s.pool.QueryRow(ctx, `SELECT v.config,p.policy,now() FROM pricing_versions v JOIN pricing_quote_policies p ON p.version=v.version WHERE v.version='usd-dev-character-create-unapproved' AND NOT v.approved`).Scan(&row.Config, &row.Policy, &row.QuotedAt)
	}
	if err != nil {
		return c, p, errCreationDisabled
	}
	c, err = pricing.Parse(row.Config)
	if err != nil || json.Unmarshal(row.Policy, &p) != nil {
		return c, p, errCreationDisabled
	}
	if _, err = pricing.CalculateQuote(c, p, pricing.QuoteInput{Action: "character_create"}, time.Now()); err != nil {
		return c, p, errCreationDisabled
	}
	return c, p, nil
}

// CreateAvailability resolves the active supported key without calling providers.
func (s *Service) CreateAvailability(ctx context.Context) CreateAvailability {
	c, _, err := s.creationConfig(ctx)
	a := CreateAvailability{Enabled: err == nil, Proposal: strings.HasPrefix(c.Version, "usd-dev-")}
	if err != nil {
		a.Reason = "creation_disabled"
	}
	return a
}

// GetCreate returns only the requesting learner's saved operation.
func (s *Service) GetCreate(ctx context.Context, owner, id string) (CreateJob, error) {
	var j CreateJob
	var quoteID, reservation *string
	var raw []byte
	if _, err := uuid.Parse(id); err != nil {
		return j, ErrNotFound
	}
	err := s.pool.QueryRow(ctx, `SELECT id::text,state,reason_code,operation,quote_id::text,reservation_id::text,result FROM character_create_jobs WHERE id=$1 AND learner_id=$2`, id, owner).Scan(&j.ID, &j.State, &j.Reason, &j.Operation, &quoteID, &reservation, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return j, ErrNotFound
	}
	if err != nil {
		return j, err
	}
	if raw != nil {
		if err = json.Unmarshal(raw, &j.Character); err != nil {
			return j, err
		}
	}
	if reservation != nil {
		j.ReservationID = *reservation
	}
	if quoteID != nil {
		q, e := pricing.NewQuotes(s.pool).Get(ctx, owner, *quoteID)
		if e != nil {
			return j, e
		}
		j.Quote = &q
		j.Proposal = strings.HasPrefix(q.PricingVersion, "usd-dev-")
	}
	return j, nil
}

// QuoteCreate consumes attempt quota, screens input and snapshots the server quote.
func (s *Service) QuoteCreate(ctx context.Context, owner string, in CreateRequest) (CreateJob, error) {
	if len(in.RequestKey) < 1 || len(in.RequestKey) > 100 {
		return CreateJob{}, errors.New("invalid_request_key")
	}
	// Return the saved result even after pricing changes or prompt expiry.
	var previous string
	err := s.pool.QueryRow(ctx, `SELECT id::text FROM character_create_jobs WHERE learner_id=$1 AND request_key=$2`, owner, in.RequestKey).Scan(&previous)
	if err == nil {
		return s.GetCreate(ctx, owner, previous)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return CreateJob{}, err
	}
	c, p, err := s.creationConfig(ctx)
	if err != nil {
		return CreateJob{}, err
	}
	envelope, err := pricing.CalculateQuote(c, p, pricing.QuoteInput{Action: "character_create"}, time.Now())
	if err != nil {
		return CreateJob{}, errCreationDisabled
	}
	operation := "create"
	if in.CharacterID != "" {
		old, e := s.Get(ctx, owner, in.CharacterID)
		if e != nil {
			return CreateJob{}, e
		}
		if old.Version != in.ExpectedVersion {
			return CreateJob{}, errors.New("version_conflict")
		}
		operation = "regenerate"
	} else if in.ExpectedVersion != 0 {
		return CreateJob{}, errors.New("invalid_spec")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return CreateJob{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var lock string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM learners WHERE id=$1 FOR UPDATE`, owner).Scan(&lock); err != nil {
		return CreateJob{}, ErrNotFound
	}
	// Recheck the key after serializing concurrent submissions.
	if err = tx.QueryRow(ctx, `SELECT id::text FROM character_create_jobs WHERE learner_id=$1 AND request_key=$2`, owner, in.RequestKey).Scan(&previous); err == nil {
		_ = tx.Rollback(ctx)
		return s.GetCreate(ctx, owner, previous)
	}
	var day, minute, count int64
	if err = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE created_at>now()-interval '24 hours'),count(*) FILTER(WHERE created_at>now()-interval '1 minute') FROM character_create_jobs WHERE learner_id=$1`, owner).Scan(&day, &minute); err != nil {
		return CreateJob{}, err
	}
	if day >= envelope.Bounds["day_attempts"] || minute >= envelope.Bounds["minute_attempts"] {
		return CreateJob{}, errors.New("limit_exceeded")
	}
	// Pending creates hold a count slot; regeneration never consumes another slot.
	if err = tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM characters WHERE learner_id=$1 AND deleted_at IS NULL)+(SELECT count(*) FROM character_create_jobs WHERE learner_id=$1 AND operation='create' AND state IN ('screening','quoted','queued','running'))`, owner).Scan(&count); err != nil {
		return CreateJob{}, err
	}
	reason := ""
	if !ValidCreatePrompt(in.Prompt) || int64(utf8.RuneCountInString(in.Prompt)) > envelope.Bounds["prompt_chars"] {
		reason = "invalid_spec"
	}
	if operation == "create" && count >= envelope.Bounds["count"] {
		reason = "limit_exceeded"
	}
	state := "screening"
	var prompt any = in.Prompt
	if reason != "" {
		state = "rejected"
		prompt = nil
	}
	var id string
	var character any
	if in.CharacterID != "" {
		character = in.CharacterID
	}
	if err = tx.QueryRow(ctx, `INSERT INTO character_create_jobs(learner_id,request_key,character_id,expected_version,operation,state,reason_code,prompt,deadline_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,now()+interval '90 seconds') RETURNING id::text`, owner, in.RequestKey, character, in.ExpectedVersion, operation, state, reason, prompt).Scan(&id); err != nil {
		return CreateJob{}, err
	}
	if reason != "" {
		if err = creationEvent(ctx, tx, owner, id+":rejected", "character_rejected", map[string]any{"operation": operation, "reason_code": reason}); err != nil {
			return CreateJob{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return CreateJob{}, err
	}
	if reason != "" {
		return s.GetCreate(ctx, owner, id)
	}
	// Input is screened before quote confirmation or any paid mapping. Rejections
	// retain only a bounded verdict; never the raw prompt.
	screenCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	response, screenErr := s.screener.ScreenCharacter(screenCtx, &pb.ScreenCharacterRequest{RawPrompt: in.Prompt, Limits: creationScreenLimits(envelope, envelope.Bounds["max_cost_nanos"])})
	cancel()
	if reason = s.recordScreen(ctx, owner, id, response, screenErr, envelope); reason != "" {
		return s.failCreate(ctx, owner, id, "rejected", reason, nil)
	}
	quotes := pricing.NewQuotes(s.pool)
	var q pricing.Quote
	if strings.HasPrefix(c.Version, "usd-dev-") {
		q, err = quotes.CreateDevelopmentCharacter(ctx, owner, pricing.QuoteInput{Action: "character_create"})
	} else {
		q, err = quotes.Create(ctx, owner, pricing.QuoteInput{Action: "character_create"})
	}
	if err != nil {
		return s.failCreate(ctx, owner, id, "failed", "creation_disabled", nil)
	}
	tx, err = s.pool.Begin(ctx)
	if err != nil {
		return CreateJob{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `UPDATE character_create_jobs SET state='quoted',quote_id=$3,deadline_at=NULL WHERE id=$1 AND learner_id=$2 AND state='screening'`, id, owner, q.ID)
	if err != nil {
		return CreateJob{}, err
	}
	if tag.RowsAffected() == 1 {
		err = creationEvent(ctx, tx, owner, id+":quoted", "character_create_quoted", map[string]any{"operation": operation, "credits": q.ContentCeiling, "pricing_version": q.PricingVersion})
	}
	if err != nil {
		return CreateJob{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return CreateJob{}, err
	}
	return s.GetCreate(ctx, owner, id)
}

// ConfirmCreate records consent and reserves once before background mapping.
func (s *Service) ConfirmCreate(ctx context.Context, owner, id, quoteID string) (CreateJob, error) {
	j, err := s.GetCreate(ctx, owner, id)
	if err != nil {
		return j, err
	}
	if j.Quote == nil || quoteID != j.Quote.ID {
		return j, errors.New("quote_mismatch")
	}
	if j.State != "quoted" {
		return j, nil
	}
	if _, _, err = s.creationConfig(ctx); err != nil {
		return j, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return j, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var state string
	if err = tx.QueryRow(ctx, `SELECT state FROM character_create_jobs WHERE id=$1 AND learner_id=$2 FOR UPDATE`, id, owner).Scan(&state); err != nil {
		return j, ErrNotFound
	}
	if state != "quoted" {
		_ = tx.Rollback(ctx)
		return s.GetCreate(ctx, owner, id)
	}
	quotes := pricing.NewQuotes(s.pool)
	if err = quotes.Accept(ctx, owner, quoteID); err != nil {
		return j, errors.New("quote_expired")
	}
	reservation, err := s.creation.wallet.ReserveAction(ctx, owner, quoteID, "character_create")
	if err != nil {
		if errors.Is(err, credits.ErrInsufficient) {
			return j, errors.New("insufficient_credits")
		}
		return j, err
	}
	if reservation.State != "reserved" {
		return j, errors.New("reservation_unavailable")
	}
	_, err = tx.Exec(ctx, `UPDATE character_create_jobs SET state='queued',reservation_id=$3,deadline_at=$4 WHERE id=$1 AND learner_id=$2`, id, owner, reservation.ID, reservation.DeadlineAt)
	if err != nil {
		return j, err
	}
	if err = tx.Commit(ctx); err != nil {
		return j, err
	}
	return s.GetCreate(ctx, owner, id)
}

// ExecuteCreate claims one confirmed job and atomically saves and settles success.
func (s *Service) ExecuteCreate(ctx context.Context, owner, id string) error {
	j, err := s.GetCreate(ctx, owner, id)
	if err != nil {
		return err
	}
	if j.State != "queued" {
		return nil
	}
	tag, err := s.pool.Exec(ctx, `UPDATE character_create_jobs SET state='running' WHERE id=$1 AND learner_id=$2 AND state='queued' AND deadline_at>clock_timestamp()`, id, owner)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		current, e := s.GetCreate(ctx, owner, id)
		if e != nil {
			return e
		}
		if current.State == "queued" {
			_, e = s.failCreate(ctx, owner, id, "failed", "timeout", nil)
		}
		return e
	}
	var prompt *string
	var character *string
	var expected int
	var deadline time.Time
	var screenCost int64
	if err = s.pool.QueryRow(ctx, `SELECT prompt,character_id::text,expected_version,deadline_at,screen_cost_nanos FROM character_create_jobs WHERE id=$1 AND learner_id=$2`, id, owner).Scan(&prompt, &character, &expected, &deadline, &screenCost); err != nil {
		return err
	}
	jobCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	fail := func(reason string, usage []*pb.Usage) error {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer stop()
		_, e := s.failCreate(cleanup, owner, id, "failed", reason, usage)
		return e
	}
	if prompt == nil {
		return fail("cancelled", nil)
	}
	if err = s.creation.wallet.Claim(jobCtx, owner, j.ReservationID); err != nil {
		return fail("provider_unavailable", nil)
	}
	limits, err := s.creation.wallet.Limits(jobCtx, owner, j.ReservationID, "character_create")
	if err != nil {
		return fail("provider_unavailable", nil)
	}
	if limits.OutputTokens > 2000 || limits.MaxAttempts > 2 {
		return fail("invalid_spec", nil)
	}
	screenLimits := creationScreenLimits(*j.Quote, j.Quote.Bounds["max_cost_nanos"]-screenCost)
	worstBounds := &pb.UsageLimits{InputNanos: max(screenLimits.InputNanos, screenLimits.CacheWriteNanos, screenLimits.CacheReadNanos), OutputNanos: screenLimits.OutputNanos}
	screenWorst := usageCost(&pb.Usage{InputTokens: screenLimits.InputTokens, OutputTokens: screenLimits.OutputTokens}, worstBounds)
	if screenLimits.MaxAttempts == 0 || screenWorst > j.Quote.Bounds["max_cost_nanos"]/int64(screenLimits.MaxAttempts) {
		return fail("provider_unavailable", nil)
	}
	screenWorst *= int64(screenLimits.MaxAttempts)
	remaining := j.Quote.Bounds["max_cost_nanos"] - screenCost - screenWorst
	if remaining <= 0 {
		return fail("provider_unavailable", nil)
	}
	limits.MaxCostNanos = min(limits.MaxCostNanos, uint64(remaining))
	response, err := s.creation.mapper.MapCharacter(jobCtx, &pb.MapCharacterRequest{Prompt: *prompt, Limits: limits})
	var attempts []*pb.Usage
	if response != nil {
		attempts = response.Attempts
	}
	mappingErr := err
	if mappingErr == nil && response != nil {
		_, mappingErr = ValidateSpecJSON([]byte(response.SpecJson))
	}
	if e := s.recordMapping(context.WithoutCancel(ctx), owner, id, attempts, mappingErr, limits); e != nil {
		return fail("provider_unavailable", attempts)
	}
	if err != nil || response == nil || len(attempts) == 0 || len(attempts) > int(limits.MaxAttempts) {
		return fail("provider_unavailable", attempts)
	}
	spec, err := ValidateSpecJSON([]byte(response.SpecJson))
	if err != nil {
		return fail("invalid_spec", attempts)
	}
	compiled, err := CompileSVG(spec, "idle")
	if err != nil {
		return fail("invalid_spec", attempts)
	}
	mappingCost := int64(0)
	for _, a := range attempts {
		if a == nil || a.UsageUnavailable {
			return fail("provider_unavailable", attempts)
		}
		cost := usageCost(a, limits)
		if cost > j.Quote.Bounds["max_cost_nanos"]-mappingCost {
			return fail("provider_unavailable", attempts)
		}
		mappingCost += cost
	}
	remaining = j.Quote.Bounds["max_cost_nanos"] - screenCost - mappingCost
	if remaining <= 0 {
		return fail("provider_unavailable", attempts)
	}
	screenLimits = creationScreenLimits(*j.Quote, remaining)
	screenLimits.DeadlineUnixMs = uint64(deadline.UnixMilli())
	screenCtx, screenCancel := context.WithTimeout(jobCtx, 10*time.Second)
	screened, screenErr := s.screener.ScreenCharacter(screenCtx, &pb.ScreenCharacterRequest{SpecJson: response.SpecJson, CompiledAppearance: compiled, Limits: screenLimits})
	screenCancel()
	if reason := s.recordScreen(context.WithoutCancel(ctx), owner, id, screened, screenErr, *j.Quote); reason != "" {
		return fail(reason, attempts)
	}
	if err = s.pool.QueryRow(jobCtx, `SELECT screen_cost_nanos FROM character_create_jobs WHERE id=$1 AND learner_id=$2`, id, owner).Scan(&screenCost); err != nil {
		return fail("provider_unavailable", attempts)
	}
	if screenCost > j.Quote.Bounds["max_cost_nanos"]-mappingCost {
		return fail("provider_unavailable", attempts)
	}
	if jobCtx.Err() != nil {
		return fail("timeout", attempts)
	}
	accepted := false
	for _, a := range attempts {
		if a != nil && a.Delivered && !a.UsageUnavailable {
			accepted = true
			a.ArtifactId = id
		}
	}
	if !accepted {
		return fail("provider_unavailable", attempts)
	}
	_, err = s.creation.wallet.SettleWith(jobCtx, owner, j.ReservationID, attempts, func(tx pgx.Tx, r credits.Reservation) error {
		if r.State != "settled" {
			return errors.New("invalid_spec")
		}
		var state string
		if e := tx.QueryRow(jobCtx, `SELECT state FROM character_create_jobs WHERE id=$1 AND learner_id=$2 AND deadline_at>clock_timestamp() FOR UPDATE`, id, owner).Scan(&state); e != nil {
			return e
		}
		if state != "running" {
			return errors.New("cancelled")
		}
		// Serialize account deletion, count checks and version compare-and-swap.
		var lock string
		if e := tx.QueryRow(jobCtx, `SELECT id::text FROM learners WHERE id=$1 FOR UPDATE`, owner).Scan(&lock); e != nil {
			return e
		}
		next := 1
		characterID := ""
		if character == nil {
			var count int64
			if e := tx.QueryRow(jobCtx, `SELECT count(*) FROM characters WHERE learner_id=$1 AND deleted_at IS NULL`, owner).Scan(&count); e != nil {
				return e
			}
			if count >= j.Quote.Bounds["count"] {
				return errors.New("limit_exceeded")
			}
			if e := tx.QueryRow(jobCtx, `INSERT INTO characters(learner_id) VALUES($1) RETURNING id::text`, owner).Scan(&characterID); e != nil {
				return e
			}
		} else {
			characterID = *character
			next = expected + 1
			tag, e := tx.Exec(jobCtx, `UPDATE characters SET current_version=$4 WHERE id=$1 AND learner_id=$2 AND deleted_at IS NULL AND current_version=$3`, characterID, owner, expected, next)
			if e != nil {
				return e
			}
			if tag.RowsAffected() != 1 {
				return errors.New("version_conflict")
			}
		}
		if _, e := tx.Exec(jobCtx, `INSERT INTO character_versions(character_id,version,spec) VALUES($1,$2,$3)`, characterID, next, response.SpecJson); e != nil {
			return e
		}
		record := Record{ID: characterID, Version: next, Spec: *spec}
		raw, _ := json.Marshal(record)
		if _, e := tx.Exec(jobCtx, `UPDATE character_create_jobs SET state='succeeded',character_id=$2,result=$3,prompt=NULL,finished_at=now() WHERE id=$1`, id, characterID, raw); e != nil {
			return e
		}
		event := "character_created"
		if j.Operation == "regenerate" {
			event = "character_regenerated"
		}
		return creationEvent(jobCtx, tx, owner, id+":result", event, map[string]any{"character_id": characterID, "character_version": next, "rig_version": spec.RigVersion, "voice_key": spec.VoiceKey, "credits": r.ContentSpent, "pricing_version": r.PricingVersion})
	})
	if err != nil {
		return fail(createReason(err.Error()), attempts)
	}
	return nil
}

// Screening is platform-paid, but its worst-case and actual usage still fit
// inside this operation's configured provider ceiling.
func creationScreenLimits(q pricing.Quote, remaining int64) *pb.UsageLimits {
	primary := q.Rate
	fallback := q.Policy.Models[q.Policy.FallbackModels[q.Input.Model]]
	// Conservatively include the platform screening route's configured rates.
	screen := q.Policy.Models["haiku"]
	screenFallback := q.Policy.Models[q.Policy.FallbackModels["haiku"]]
	return &pb.UsageLimits{InputTokens: boundedUint32(q.Bounds["screen_input_tokens"]), OutputTokens: boundedUint32(q.Bounds["screen_output_tokens"]), TimeoutMs: boundedUint32(q.Bounds["screen_timeout_ms"]), MaxAttempts: boundedUint32(q.Bounds["screen_max_attempts"]), MaxCostNanos: boundedUint64(max(remaining, 0)), InputNanos: boundedUint64(max(primary.Input, fallback.Input, screen.Input, screenFallback.Input)), OutputNanos: boundedUint64(max(primary.Output, fallback.Output, screen.Output, screenFallback.Output)), CacheWriteNanos: boundedUint64(max(primary.CacheWrite, fallback.CacheWrite, screen.CacheWrite, screenFallback.CacheWrite)), CacheReadNanos: boundedUint64(max(primary.CacheRead, fallback.CacheRead, screen.CacheRead, screenFallback.CacheRead))}
}
func boundedUint64(n int64) uint64 {
	if n < 0 {
		return 0
	}
	return uint64(n)
}
func boundedUint32(n int64) uint32 {
	if n < 0 || n > math.MaxUint32 {
		return 0
	}
	return uint32(n)
}
func usageCost(a *pb.Usage, b *pb.UsageLimits) int64 {
	total := uint64(0)
	for _, pair := range [][2]uint64{{uint64(a.InputTokens), b.InputNanos}, {uint64(a.OutputTokens), b.OutputNanos}, {uint64(a.CacheWriteTokens), b.CacheWriteNanos}, {uint64(a.CacheReadTokens), b.CacheReadNanos}} {
		if pair[0] > 0 && pair[1] > math.MaxInt64/pair[0] {
			return math.MaxInt64
		}
		term := pair[0] * pair[1]
		if term > math.MaxInt64-total {
			return math.MaxInt64
		}
		total += term
	}
	if total > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(total)
}

// recordMapping owns Character mapping telemetry, including late results after
// cancellation. Financial settlement never needs to invent or repeat attempts.
func (s *Service) recordMapping(ctx context.Context, owner, id string, attempts []*pb.Usage, callErr error, bounds *pb.UsageLimits) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if len(attempts) == 0 {
		err = creationEvent(ctx, tx, owner, id+":mapping:unknown", "$ai_generation", map[string]any{"feature": "character_create", "usage_available": false, "succeeded": false, "error_code": "provider_unavailable"})
	} else {
		for i, a := range attempts {
			if a == nil {
				continue
			}
			props := map[string]any{"feature": "character_create", "provider_cost_nanos": usageCost(a, bounds), "provider": a.Provider, "model": a.Model, "input_tokens": a.InputTokens, "output_tokens": a.OutputTokens, "cache_write_tokens": a.CacheWriteTokens, "cache_read_tokens": a.CacheReadTokens, "attempt": a.Attempt, "latency_ms": a.LatencyMs, "succeeded": a.Delivered && callErr == nil, "usage_available": !a.UsageUnavailable, "fallback_used": a.FallbackUsed, "error_code": a.ErrorCode}
			if err = creationEvent(ctx, tx, owner, fmt.Sprintf("%s:mapping:%d", id, i), "$ai_generation", props); err != nil {
				return err
			}
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func createReason(reason string) string {
	switch reason {
	case "invalid_spec", "limit_exceeded", "screening_unavailable", "real_person", "third_party_ip", "sexual_content", "age_inappropriate", "prompt_injection", "creation_disabled", "cancelled", "timeout", "version_conflict", "provider_unavailable":
		return reason
	}
	return "provider_unavailable"
}
func (s *Service) recordScreen(ctx context.Context, owner, id string, r *pb.ScreenCharacterResponse, err error, q pricing.Quote) string {
	reason := "screening_unavailable"
	if err == nil && r != nil && r.PolicyVersion == "character-v1" && len(r.Attempts) > 0 && int64(len(r.Attempts)) <= q.Bounds["screen_max_attempts"] {
		last := r.Attempts[len(r.Attempts)-1]
		if r.Approved && r.ReasonCode == "" && last != nil && last.Delivered && !last.UsageUnavailable && last.OutputTokens <= 200 && uint64(last.InputTokens)+uint64(last.CacheWriteTokens)+uint64(last.CacheReadTokens) <= 18000 {
			reason = ""
		} else if !r.Approved {
			reason = boundedReason(r.ReasonCode)
		}
	}
	if r != nil {
		for _, a := range r.Attempts {
			if a != nil {
				s.emit(ctx, owner, "$ai_generation", map[string]any{"feature": "character_screen", "provider_cost_nanos": usageCost(a, creationScreenLimits(q, q.Bounds["max_cost_nanos"])), "provider": a.Provider, "model": a.Model, "input_tokens": a.InputTokens, "output_tokens": a.OutputTokens, "cache_write_tokens": a.CacheWriteTokens, "cache_read_tokens": a.CacheReadTokens, "attempt": a.Attempt, "latency_ms": a.LatencyMs, "succeeded": a.Delivered, "usage_available": !a.UsageUnavailable, "fallback_used": a.FallbackUsed, "error_code": a.ErrorCode})
			}
		}
	}
	cost := int64(0)
	bounds := creationScreenLimits(q, q.Bounds["max_cost_nanos"])
	if r != nil {
		for _, a := range r.Attempts {
			if a == nil || a.UsageUnavailable {
				reason = "screening_unavailable"
				continue
			}
			if a.Attempt == 0 || a.Attempt > bounds.MaxAttempts || a.LatencyMs > bounds.TimeoutMs || uint64(a.InputTokens)+uint64(a.CacheWriteTokens)+uint64(a.CacheReadTokens) > uint64(bounds.InputTokens) || a.OutputTokens > bounds.OutputTokens {
				reason = "screening_unavailable"
			}
			attemptCost := usageCost(a, bounds)
			if attemptCost > q.Bounds["max_cost_nanos"]-cost {
				reason = "screening_unavailable"
				cost = q.Bounds["max_cost_nanos"]
				break
			}
			cost += attemptCost
		}
	}
	if cost > q.Bounds["max_cost_nanos"] {
		reason = "screening_unavailable"
	}
	if _, e := s.pool.Exec(ctx, `UPDATE character_create_jobs SET screen_cost_nanos=screen_cost_nanos+$2 WHERE id=$1`, id, cost); e != nil {
		return "screening_unavailable"
	}
	verdict := "approved"
	if reason != "" {
		verdict = "rejected"
	}
	_, e := s.pool.Exec(ctx, `INSERT INTO character_screenings(learner_id,verdict,reason_code,policy_version,operation_id) VALUES($1,$2,$3,'character-v1',$4)`, owner, verdict, reason, id)
	if e != nil {
		return "screening_unavailable"
	}
	return reason
}
func creationEvent(ctx context.Context, tx pgx.Tx, owner, key, event string, props map[string]any) error {
	props["source"] = "api"
	props["platform"] = "web"
	props["app_version"] = "development"
	raw, _ := json.Marshal(props)
	_, err := tx.Exec(ctx, `INSERT INTO credit_outbox(event_key,learner_id,owner_reference,event,properties) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, key, owner, accounts.LearnerReference(owner), event, raw)
	return err
}
func (s *Service) failCreate(ctx context.Context, owner, id, state, reason string, attempts []*pb.Usage) (CreateJob, error) {
	j, err := s.GetCreate(ctx, owner, id)
	if err != nil {
		return j, err
	}
	if CreateTerminal(j.State) {
		return j, nil
	}
	// Only accepted, validated persistence is billable. Actual failed model usage
	// still consumes operational capacity and is reported by the job outbox.
	for _, a := range attempts {
		if a != nil {
			a.Delivered = false
		}
	}
	if j.ReservationID != "" && s.creation != nil {
		if _, err = s.creation.wallet.Settle(ctx, owner, j.ReservationID, attempts); err != nil && !errors.Is(err, credits.ErrUsage) {
			return j, err
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return j, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	reason = createReason(reason)
	tag, err := tx.Exec(ctx, `UPDATE character_create_jobs SET state=$3,reason_code=$4,prompt=NULL,finished_at=now() WHERE id=$1 AND learner_id=$2 AND state NOT IN ('succeeded','failed','rejected','cancelled')`, id, owner, state, reason)
	if err != nil {
		return j, err
	}
	if tag.RowsAffected() == 1 {
		event := "character_create_failed"
		if state == "rejected" {
			event = "character_rejected"
		}
		fallback := false
		for _, a := range attempts {
			fallback = fallback || a != nil && a.FallbackUsed
		}
		err = creationEvent(ctx, tx, owner, id+":result", event, map[string]any{"operation": j.Operation, "reason_code": reason, "fallback_used": fallback})
	}
	if err != nil {
		return j, err
	}
	if err = tx.Commit(ctx); err != nil {
		return j, err
	}
	return s.GetCreate(ctx, owner, id)
}

// CancelCreate fences acceptance before releasing undelivered Credits.
func (s *Service) CancelCreate(ctx context.Context, owner, id string) (CreateJob, error) {
	j, err := s.GetCreate(ctx, owner, id)
	if err != nil || CreateTerminal(j.State) {
		return j, err
	}
	// First fence acceptance under the same row lock used by atomic settlement.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return j, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `UPDATE character_create_jobs SET state='cancelled',reason_code='cancelled',prompt=NULL,finished_at=now() WHERE id=$1 AND learner_id=$2 AND state NOT IN ('succeeded','failed','rejected','cancelled')`, id, owner)
	if err != nil {
		return j, err
	}
	if tag.RowsAffected() == 1 {
		if err = creationEvent(ctx, tx, owner, id+":result", "character_create_failed", map[string]any{"operation": j.Operation, "reason_code": "cancelled", "fallback_used": false}); err != nil {
			return j, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return j, err
	}

	if tag.RowsAffected() == 1 && j.ReservationID != "" {
		_, err = s.creation.wallet.Settle(ctx, owner, j.ReservationID, nil)
	}
	if err != nil {
		return j, err
	}
	return s.GetCreate(ctx, owner, id)
}

// RecoverCreateJobs resumes only confirmed queued work, never a raw abandoned
// prompt. Running jobs lost on restart are released after their fixed deadline.
func (s *Service) RecoverCreateJobs(ctx context.Context) error {
	if s.creation == nil {
		return nil
	}
	rows, err := s.pool.Query(ctx, `SELECT id::text,learner_id::text,state FROM character_create_jobs WHERE (state='queued') OR (state IN ('screening','running') AND deadline_at<=clock_timestamp()) OR (state='quoted' AND prompt_expires_at<=now())`)
	if err != nil {
		return err
	}
	type pending struct{ id, owner, state string }
	var jobs []pending
	for rows.Next() {
		var j pending
		if err = rows.Scan(&j.id, &j.owner, &j.state); err != nil {
			rows.Close()
			return err
		}
		jobs = append(jobs, j)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if j.state == "queued" {
			_ = s.ExecuteCreate(ctx, j.owner, j.id)
		} else {
			_, _ = s.failCreate(ctx, j.owner, j.id, "failed", "timeout", nil)
		}
	}
	_, err = s.pool.Exec(ctx, `UPDATE character_create_jobs SET prompt=NULL WHERE prompt_expires_at<=now()`)
	return err
}
