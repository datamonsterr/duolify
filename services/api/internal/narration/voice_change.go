package narration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// Entitlements is the verified funded-plan lookup (E7-07); it is the only source of voice rights.
type Entitlements interface {
	FundedPlan(ctx context.Context, owner string, at time.Time) (*credits.FundedPlan, error)
}

var (
	// ErrNarrationOff means the Learner turned narration off or the server flag is off.
	ErrNarrationOff = errors.New("narration is off")
	// ErrNotEntitled means the verified plan does not include curated voices.
	ErrNotEntitled = errors.New("curated voices are not in the plan")
	// ErrQuoteChanged asks the Learner to review a fresh quote and consent again.
	ErrQuoteChanged = errors.New("voice quote changed")
)

// plan returns the verified plan snapshot; a missing lookup or an error entitles nothing.
func (s *Service) plan(ctx context.Context, owner string) (*credits.FundedPlan, error) {
	if s.opts.Entitlements == nil {
		return nil, nil
	}
	return s.opts.Entitlements.FundedPlan(ctx, owner, time.Now())
}

// preference reads the Learner's global narration switch and the voice they chose, falling back
// to the default voice when the chosen one is no longer approved or entitled.
func (s *Service) preference(ctx context.Context, owner string) (on bool, voice string) {
	on, voice = true, VoiceKey
	var stored string
	if err := s.opts.Pool.QueryRow(ctx, `SELECT narration, narration_voice FROM learner_profiles WHERE learner_id::text=$1`, owner).Scan(&on, &stored); err != nil {
		return true, VoiceKey
	}
	if v, ok := s.voices.Find(stored); ok {
		if !v.Curated {
			return on, v.Key
		}
		if plan, err := s.plan(ctx, owner); err == nil && EntitledToCuratedVoices(plan) {
			return on, v.Key
		}
	}
	return on, VoiceKey
}

// skipCode names why a run must do no work at all, or "" when it may proceed.
func (s *Service) skipCode(ctx context.Context, owner, voice string) string {
	if s.opts.Off {
		return "narration_off"
	}
	var on bool
	if err := s.opts.Pool.QueryRow(ctx, `SELECT narration FROM learner_profiles WHERE learner_id::text=$1`, owner).Scan(&on); err == nil && !on {
		return "narration_off"
	}
	v, ok := s.voices.Find(voice)
	if !ok {
		return "voice_unavailable"
	}
	if v.Curated {
		plan, err := s.plan(ctx, owner)
		if err != nil || !EntitledToCuratedVoices(plan) {
			return "not_entitled"
		}
	}
	return ""
}

// skip parks a pending row without an event, a quote or a charge.
func (s *Service) skip(ctx context.Context, owner, lessonID, voice, slideID, code string) error {
	_, err := s.opts.Pool.Exec(ctx, `UPDATE slide_narrations SET status='disabled',failure_code=$5,updated_at=now()
		WHERE lesson_id::text=$1 AND learner_id::text=$2 AND slide_id=$3 AND voice_key=$4 AND status='pending'`, lessonID, owner, slideID, voice, code)
	return err
}

// VoiceQuote is the disclosed cost of switching a Lesson's narration to another voice.
type VoiceQuote struct {
	LessonID       string `json:"lesson_id"`
	VoiceKey       string `json:"voice_key"`
	QuoteID        string `json:"quote_id,omitempty"`
	Characters     int64  `json:"characters"`
	Credits        int64  `json:"credits"`
	Free           bool   `json:"free"`
	PricingVersion string `json:"pricing_version,omitempty"`
	ConfigHash     string `json:"config_hash"`
	ExpiresAt      string `json:"expires_at,omitempty"`
}

type slideText struct{ id, text string }

// lessonTexts lists each slide's narration text once, whichever voice stored it.
func (s *Service) lessonTexts(ctx context.Context, owner, lessonID string) ([]slideText, error) {
	rows, err := s.opts.Pool.Query(ctx, `SELECT DISTINCT ON (slide_id) slide_id, narration_text FROM slide_narrations
		WHERE lesson_id::text=$1 AND learner_id::text=$2 AND narration_text<>'' AND speaker_key='' ORDER BY slide_id, updated_at DESC`, lessonID, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []slideText
	for rows.Next() {
		var t slideText
		if err = rows.Scan(&t.id, &t.text); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// needed counts the characters that would be newly synthesized: texts whose content hash under the
// voice has no stored asset. Replaying or switching back to a stored voice needs none.
func (s *Service) needed(ctx context.Context, owner, lessonID, voice string) (int64, []slideText, error) {
	texts, err := s.lessonTexts(ctx, owner, lessonID)
	if err != nil {
		return 0, nil, err
	}
	if len(texts) == 0 {
		return 0, nil, ErrNotFound
	}
	resolved, err := s.opts.Speech.SynthesizeNarration(ctx, &enginev1.SynthesizeNarrationRequest{SlideId: texts[0].id, VoiceKey: voice, ResolveOnly: true})
	if err != nil {
		return 0, nil, err
	}
	seen := map[string]bool{}
	var characters int64
	for _, t := range texts {
		text := NormalizeText(t.text)
		if ValidText(text) != nil {
			continue
		}
		hash := ContentHash(text, resolved.VoiceId, resolved.ModelId, resolved.SettingsFingerprint)
		if seen[hash] {
			continue
		}
		seen[hash] = true
		var state string
		if e := s.opts.Pool.QueryRow(ctx, `SELECT state FROM narration_assets WHERE content_hash=$1`, hash).Scan(&state); e == nil && state == "ready" {
			continue
		} else if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return 0, nil, e
		}
		characters += int64(len([]rune(text)))
	}
	return characters, texts, nil
}

func (s *Service) gate(ctx context.Context, owner, voice string) (Voice, error) {
	if s.opts.Off {
		return Voice{}, ErrNarrationOff
	}
	if on, _ := s.preference(ctx, owner); !on {
		return Voice{}, ErrNarrationOff
	}
	v, ok := s.voices.Find(voice)
	if !ok {
		return Voice{}, ErrVoice
	}
	if v.Curated {
		plan, err := s.plan(ctx, owner)
		if err != nil {
			return Voice{}, err
		}
		if !EntitledToCuratedVoices(plan) {
			return Voice{}, ErrNotEntitled
		}
	}
	return v, nil
}

// QuoteVoice discloses the cost of narrating the Lesson in a voice. It creates no reservation,
// starts no synthesis and changes no setting.
func (s *Service) QuoteVoice(ctx context.Context, owner, lessonID, voice string) (VoiceQuote, error) {
	out := VoiceQuote{LessonID: lessonID, VoiceKey: voice, ConfigHash: s.voices.Hash()}
	if _, err := s.gate(ctx, owner, voice); err != nil {
		return out, err
	}
	characters, _, err := s.needed(ctx, owner, lessonID, voice)
	if err != nil {
		return out, err
	}
	if characters == 0 {
		out.Free = true
		return out, nil
	}
	quote, err := pricing.NewQuotes(s.opts.Pool).WithApprovedVoices(s.voices.Keys()).Create(ctx, owner, pricing.QuoteInput{Action: "narration", NarrationCharacters: characters, Voice: voice})
	if err != nil {
		return out, err
	}
	out.QuoteID, out.Characters, out.Credits = quote.ID, characters, quote.AudioCeiling
	out.PricingVersion, out.ExpiresAt = quote.PricingVersion, quote.ExpiresAt.UTC().Format(time.RFC3339)
	return out, nil
}

// ApplyVoice records consent and queues synthesis in the chosen voice. Old assets stay referenced
// and playable; nothing is charged here, and the queued run can never exceed the accepted quote.
func (s *Service) ApplyVoice(ctx context.Context, owner, lessonID, voice, quoteID, configHash string) (string, error) {
	if _, err := s.gate(ctx, owner, voice); err != nil {
		return "", err
	}
	if configHash != s.voices.Hash() {
		return "", ErrQuoteChanged
	}
	if s.client == nil {
		return "", errors.New("narration queue not configured")
	}
	characters, texts, err := s.needed(ctx, owner, lessonID, voice)
	if err != nil {
		return "", err
	}
	if characters > 0 {
		quotes := pricing.NewQuotes(s.opts.Pool).WithApprovedVoices(s.voices.Keys())
		quote, e := quotes.Get(ctx, owner, quoteID)
		if quoteID == "" || e != nil || quote.Action != "narration" || quote.Input.Voice != voice || quote.Expired || quote.Input.NarrationCharacters < characters {
			return "", ErrQuoteChanged
		}
		if e = quotes.Accept(ctx, owner, quoteID); e != nil {
			return "", ErrQuoteChanged
		}
	}
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	for _, t := range texts {
		if _, err = tx.Exec(ctx, `INSERT INTO slide_narrations(lesson_id,learner_id,slide_id,voice_key,status,narration_text) VALUES($1,$2,$3,$4,'pending',$5)
			ON CONFLICT(lesson_id,slide_id,voice_key) DO UPDATE SET status='pending',failure_code='',updated_at=now()
			WHERE slide_narrations.status IN ('failed','disabled') AND slide_narrations.failure_code<>'missing_text'`, lessonID, owner, t.id, voice, t.text); err != nil {
			return "", err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE learner_profiles SET narration_voice=$2 WHERE learner_id::text=$1`, owner, voice); err != nil {
		return "", err
	}
	var pending int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM slide_narrations WHERE lesson_id::text=$1 AND learner_id::text=$2 AND voice_key=$3 AND speaker_key='' AND status='pending'`, lessonID, owner, voice).Scan(&pending); err != nil {
		return "", err
	}
	if pending > 0 {
		if _, err = s.client.InsertTx(ctx, tx, Args{LearnerID: owner, LessonID: lessonID, VoiceKey: voice, ConsentQuote: quoteID}, &river.InsertOpts{MaxAttempts: s.opts.JobAttempts}); err != nil {
			return "", err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	if pending == 0 {
		// Every slide already has this voice stored: the switch is a free cache hit.
		for _, t := range texts {
			s.emit(owner, "narration_cache_hit", map[string]any{"lesson_id": lessonID, "slide_id": t.id, "model": s.modelFor(ctx, lessonID, owner, t.id, voice)})
		}
		return "ready", nil
	}
	return "queued", nil
}

func (s *Service) modelFor(ctx context.Context, lessonID, owner, slideID, voice string) string {
	var model string
	_ = s.opts.Pool.QueryRow(ctx, `SELECT model_id FROM slide_narrations WHERE lesson_id::text=$1 AND learner_id::text=$2 AND slide_id=$3 AND voice_key=$4`, lessonID, owner, slideID, voice).Scan(&model)
	return model
}

// VoiceChange is the state of the selected voice's synthesis for one Lesson.
type VoiceChange struct {
	VoiceKey string `json:"voice_key"`
	// State is none, pending, ready, disabled or failed; Reason is a fixed code.
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

// VoiceState reports the selected voice's rows for a Lesson, worst state first.
func (s *Service) VoiceState(ctx context.Context, owner, lessonID, voice string) (VoiceChange, error) {
	out := VoiceChange{VoiceKey: voice, State: "none"}
	rows, err := s.opts.Pool.Query(ctx, `SELECT status, failure_code FROM slide_narrations WHERE lesson_id::text=$1 AND learner_id::text=$2 AND voice_key=$3 AND speaker_key=''`, lessonID, owner, voice)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	rank := map[string]int{"none": 0, "ready": 1, "pending": 2, "failed": 3, "disabled": 4}
	for rows.Next() {
		var st, code string
		if err = rows.Scan(&st, &code); err != nil {
			return out, err
		}
		if rank[st] > rank[out.State] || out.State == "none" {
			out.State, out.Reason = st, code
		}
	}
	return out, rows.Err()
}

// VoiceInfo is the Learner-facing voice list.
type VoiceInfo struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Curated   bool   `json:"curated"`
	Available bool   `json:"available"`
}

// VoiceList is the response of GET /v1/me/narration/voices.
type VoiceList struct {
	NarrationEnabled bool         `json:"narration_enabled"`
	Selected         string       `json:"selected"`
	Entitled         bool         `json:"entitled"`
	UpsellAllowed    bool         `json:"upsell_allowed"`
	ConfigHash       string       `json:"config_hash"`
	Voices           []VoiceInfo  `json:"voices"`
	Change           *VoiceChange `json:"change,omitempty"`
}

// Voices lists approved voices with the Learner's verified rights.
func (s *Service) Voices(ctx context.Context, owner string) (VoiceList, error) {
	plan, err := s.plan(ctx, owner)
	if err != nil {
		return VoiceList{}, err
	}
	on, selected := s.preference(ctx, owner)
	entitled := EntitledToCuratedVoices(plan)
	out := VoiceList{NarrationEnabled: on && !s.opts.Off, Selected: selected, Entitled: entitled, UpsellAllowed: !UpsellSuppressed(plan), ConfigHash: s.voices.Hash()}
	for _, v := range s.voices.Voices {
		out.Voices = append(out.Voices, VoiceInfo{Key: v.Key, Label: v.Label, Curated: v.Curated, Available: !v.Curated || entitled})
	}
	sort.SliceStable(out.Voices, func(i, j int) bool { return !out.Voices[i].Curated && out.Voices[j].Curated })
	return out, nil
}

// HTTP serves the voice list, quote and consent endpoints. Mount it behind authentication.
func (s *Service) HTTP() http.Handler {
	reply := func(w http.ResponseWriter, status int, value any) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(value)
	}
	fail := func(w http.ResponseWriter, owner string, err error) {
		switch {
		case errors.Is(err, ErrNarrationOff):
			reply(w, http.StatusConflict, map[string]any{"code": "narration_off"})
		case errors.Is(err, ErrVoice), errors.Is(err, ErrNotFound):
			reply(w, http.StatusNotFound, map[string]any{"code": "voice_unavailable"})
		case errors.Is(err, ErrNotEntitled):
			plan, _ := s.plan(context.Background(), owner)
			reply(w, http.StatusForbidden, map[string]any{"code": "voice_not_entitled", "upsell_allowed": !UpsellSuppressed(plan)})
		case errors.Is(err, ErrQuoteChanged):
			reply(w, http.StatusConflict, map[string]any{"code": "quote_changed"})
		default:
			s.opts.Logger.Warn("narration voice request failed", "error", err)
			reply(w, http.StatusServiceUnavailable, map[string]any{"code": "unavailable"})
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner := accounts.LearnerID(r.Context())
		if owner == "" {
			reply(w, http.StatusUnauthorized, map[string]any{"code": "unauthorized"})
			return
		}
		lesson := r.PathValue("lesson")
		var in struct {
			VoiceKey   string `json:"voice_key"`
			QuoteID    string `json:"quote_id"`
			ConfigHash string `json:"config_hash"`
		}
		if r.Method == http.MethodPost {
			if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil {
				reply(w, http.StatusBadRequest, map[string]any{"code": "bad_request"})
				return
			}
		}
		switch {
		case lesson == "" && r.Method == http.MethodGet:
			list, err := s.Voices(r.Context(), owner)
			if err != nil {
				fail(w, owner, err)
				return
			}
			reply(w, http.StatusOK, list)
		case lesson != "" && r.Method == http.MethodGet:
			list, err := s.Voices(r.Context(), owner)
			if err != nil {
				fail(w, owner, err)
				return
			}
			change, err := s.VoiceState(r.Context(), owner, lesson, list.Selected)
			if err != nil {
				fail(w, owner, err)
				return
			}
			list.Change = &change
			reply(w, http.StatusOK, list)
		case lesson != "" && len(r.URL.Path) > 6 && r.URL.Path[len(r.URL.Path)-6:] == "/quote":
			quote, err := s.QuoteVoice(r.Context(), owner, lesson, in.VoiceKey)
			if err != nil {
				fail(w, owner, err)
				return
			}
			reply(w, http.StatusOK, quote)
		case lesson != "":
			state, err := s.ApplyVoice(r.Context(), owner, lesson, in.VoiceKey, in.QuoteID, in.ConfigHash)
			if err != nil {
				fail(w, owner, err)
				return
			}
			reply(w, http.StatusAccepted, map[string]any{"state": state, "voice_key": in.VoiceKey})
		default:
			reply(w, http.StatusNotFound, map[string]any{"code": "not_found"})
		}
	})
}
