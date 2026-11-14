package sharing

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/characters"
	"github.com/jackc/pgx/v5"
)

var (
	// ErrCharacterConfirmation requires informed owner consent before building exported content.
	ErrCharacterConfirmation = errors.New("character_export_confirmation_required")
	// ErrCharacterRelease keeps public exports behind rights, approved voices and live-eval evidence.
	ErrCharacterRelease = errors.New("character_export_release_gated")
)

type exportConsentKey struct{}

// WithCharacterExportConfirmation carries explicit consent from the authenticated request.
func WithCharacterExportConfirmation(ctx context.Context, confirmed bool) context.Context {
	return context.WithValue(ctx, exportConsentKey{}, confirmed)
}

// CharacterDisclosure is owner-only; no private library identifiers leave this endpoint.
type CharacterDisclosure struct {
	Names          []string `json:"names"`
	CharacterCount int      `json:"character_count"`
	CustomCount    int      `json:"custom_count"`
	ExportsEnabled bool     `json:"exports_enabled"`
}

// CharacterDisclosure lists attached names for the owner before any exported version is persisted.
func (s *Service) CharacterDisclosure(ctx context.Context, owner, course string) (CharacterDisclosure, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return CharacterDisclosure{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = owns(ctx, tx, owner, course); err != nil {
		return CharacterDisclosure{}, err
	}
	snap, err := s.load(ctx, tx, course)
	if err != nil {
		return CharacterDisclosure{}, err
	}
	out := CharacterDisclosure{Names: []string{}, ExportsEnabled: s.cfg.CharacterExportsEnabled}
	for _, speaker := range publicationSpeakers(snap) {
		out.Names = append(out.Names, speaker.Spec.Info.Name)
		out.CharacterCount++
		if !speaker.IsProvisional {
			out.CustomCount++
		}
	}
	sort.Strings(out.Names)
	return out, nil
}

func publicationCasts(snap Snapshot) []characters.LessonCastSnapshot {
	var out []characters.LessonCastSnapshot
	if snap.Cast != nil {
		out = append(out, *snap.Cast)
	}
	for _, u := range snap.Units {
		for _, l := range u.Lessons {
			if l.Cast != nil {
				out = append(out, *l.Cast)
			}
		}
	}
	return out
}

func publicationSpeakers(snap Snapshot) []characters.PinnedSpeaker {
	seen := map[string]characters.PinnedSpeaker{}
	var out []characters.PinnedSpeaker
	for _, cast := range publicationCasts(snap) {
		for _, sp := range cast.Speakers {
			prior, ok := seen[sp.CharacterID]
			if !ok || (!sp.IsProvisional && prior.IsProvisional) {
				seen[sp.CharacterID] = sp
			}
		}
	}
	for _, sp := range seen {
		out = append(out, sp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CharacterID < out[j].CharacterID })
	return out
}

func (s *Service) loadCharacters(ctx context.Context, tx pgx.Tx, course string, snap *Snapshot, index map[string][2]int) error {
	var owner string
	var selected bool
	if err := tx.QueryRow(ctx, `SELECT learner_id::text,EXISTS(SELECT 1 FROM course_casts WHERE course_id=$1 AND selection->>'guide'<>'') FROM courses WHERE id=$1`, course).Scan(&owner, &selected); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT g.lesson_stub_id::text,c.snapshot FROM generated_lessons g JOIN lesson_cast_snapshots c ON c.course_id=g.course_id AND c.learner_id=g.learner_id AND c.lesson_key=g.manifest->>'cast_key' WHERE g.course_id=$1 ORDER BY g.lesson_stub_id`, course)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var raw []byte
		if err = rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return err
		}
		pos, ok := index[id]
		if !ok {
			continue
		}
		var source characters.LessonCastSnapshot
		if err = json.Unmarshal(raw, &source); err != nil {
			rows.Close()
			return ErrOutOfAllowlist
		}
		cast, e := characters.ExportCast(source)
		if e != nil {
			rows.Close()
			return ErrOutOfAllowlist
		}
		snap.Units[pos[0]].Lessons[pos[1]].Cast = &cast
		selected = true
	}
	rows.Close()
	if rows.Err() != nil {
		return rows.Err()
	}
	// A copy's independent Cast is its attached selection too, including after source deletion.
	var isCopy bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM course_copies WHERE copy_course_id=$1 AND character_cast IS NOT NULL)`, course).Scan(&isCopy); err != nil {
		return err
	}
	if selected || isCopy {
		source, e := characters.NewService(s.pool, nil, nil).CurrentCast(ctx, owner, course)
		if e != nil {
			return e
		}
		cast, e := characters.ExportCast(source)
		if e != nil {
			return ErrOutOfAllowlist
		}
		snap.Cast = &cast
	}
	return nil
}

func (s *Service) storeCharacterAssets(ctx context.Context, tx pgx.Tx, snap Snapshot) error {
	allDocs := map[string][]byte{}
	assets := map[string]characters.RigAsset{}
	for _, cast := range publicationCasts(snap) {
		docs, err := characters.CompiledAssets(cast)
		if err != nil {
			return err
		}
		for key, raw := range docs {
			allDocs[key] = raw
		}
		for _, asset := range cast.RigAssets {
			assets[asset.SHA256] = asset
		}
	}
	hashes := make([]string, 0, len(assets))
	for hash := range assets {
		hashes = append(hashes, hash)
	}
	sort.Strings(hashes)
	for _, hash := range hashes {
		asset := assets[hash]
		// Lock/create the content hash before any new independent reference commits.
		if _, err := tx.Exec(ctx, `INSERT INTO shared_character_assets(content_hash,object_key) VALUES($1,$2) ON CONFLICT(content_hash) DO UPDATE SET released_at=NULL,purge_after=NULL,purged_at=NULL`, asset.SHA256, asset.ObjectKey); err != nil {
			return err
		}
		if s.objects == nil {
			return errors.New("character assets unavailable")
		}
		if err := s.objects.Put(ctx, asset.ObjectKey, allDocs[asset.ObjectKey]); err != nil {
			return err
		}
	}
	return nil
}

// screenCharacters consumes the same publication deadline and cost cap as the Course classifier.
// Each RPC has ten seconds and at most one bounded retry/fallback in the Engine.
func (s *Service) screenCharacters(ctx context.Context, owner string, snap Snapshot, rates Rates) (Outcome, error) {
	out := Outcome{Verdict: VerdictPass}
	speakers := publicationSpeakers(snap)
	if len(speakers) == 0 {
		return out, nil
	}
	screener, ok := s.screen.(characters.Screener)
	if !ok {
		return Outcome{Verdict: VerdictPending}, errors.New("screening_unavailable")
	}
	for _, sp := range speakers {
		var suppressed bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM character_suppressions WHERE snapshot_id=$1)`, sp.CharacterID).Scan(&suppressed); err != nil {
			return Outcome{Verdict: VerdictPending}, err
		} else if suppressed {
			return Outcome{Verdict: VerdictBlock, ReasonCodes: []string{"harmful_content"}}, nil
		}
		raw, _ := json.Marshal(sp.Spec)
		appearance, err := characters.CompileSVG(&sp.Spec, "idle")
		if err != nil {
			return Outcome{Verdict: VerdictPending}, err
		}
		remaining := s.cfg.MaxCostNanos - out.CostNanos
		if remaining <= 0 {
			return Outcome{Verdict: VerdictPending}, errors.New("screening_unavailable")
		}
		deadline, _ := ctx.Deadline()
		callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		resp, callErr := screener.ScreenCharacter(callCtx, &pb.ScreenCharacterRequest{SpecJson: string(raw), CompiledAppearance: appearance, PublicationExport: true, Limits: &pb.UsageLimits{InputTokens: 18000, OutputTokens: 200, TimeoutMs: 10000, MaxAttempts: min(s.cfg.MaxAttempts, 2), MaxCostNanos: boundedUint64(remaining), InputNanos: rates.Input, OutputNanos: rates.Output, CacheWriteNanos: rates.CacheWrite, CacheReadNanos: rates.CacheRead, DeadlineUnixMs: boundedUint64(deadline.UnixMilli())}})
		cancel()
		if resp != nil {
			for _, a := range resp.Attempts {
				if a == nil {
					continue
				}
				cost := attemptCost(a, rates)
				if cost > remaining {
					out.CostNanos = s.cfg.MaxCostNanos
				} else {
					out.CostNanos += cost
				}
				s.emit(owner, "$ai_generation", map[string]any{"feature": "character_screen", "provider": a.Provider, "model": a.Model, "attempt": a.Attempt, "latency_ms": a.LatencyMs, "input_tokens": a.InputTokens, "output_tokens": a.OutputTokens, "cache_write_tokens": a.CacheWriteTokens, "cache_read_tokens": a.CacheReadTokens, "fallback_used": a.FallbackUsed, "usage_available": !a.UsageUnavailable, "succeeded": a.Delivered, "error_code": a.ErrorCode, "provider_cost_nanos": cost})
			}
		}
		if callErr != nil || resp == nil || resp.PolicyVersion != "character-v1" || !resp.PublicationScreened || len(resp.Attempts) == 0 || len(resp.Attempts) > int(min(s.cfg.MaxAttempts, 2)) {
			return Outcome{Verdict: VerdictPending, CostNanos: out.CostNanos}, errors.New("screening_unavailable")
		}
		for _, a := range resp.Attempts {
			if a == nil || a.UsageUnavailable || a.LatencyMs > 10000 || a.Attempt == 0 || a.Attempt > 2 || uint64(a.InputTokens)+uint64(a.CacheWriteTokens)+uint64(a.CacheReadTokens) > 18000 || a.OutputTokens > 200 {
				return Outcome{Verdict: VerdictPending, CostNanos: out.CostNanos}, errors.New("screening_unavailable")
			}
		}
		if !resp.Attempts[len(resp.Attempts)-1].Delivered || out.CostNanos >= s.cfg.MaxCostNanos {
			return Outcome{Verdict: VerdictPending, CostNanos: out.CostNanos}, errors.New("screening_unavailable")
		}
		if !resp.Approved {
			if !validCharacterReason(resp.ReasonCode) {
				return Outcome{Verdict: VerdictPending, CostNanos: out.CostNanos}, errors.New("screening_unavailable")
			}
			return Outcome{Verdict: VerdictBlock, ReasonCodes: []string{resp.ReasonCode}, CostNanos: out.CostNanos}, nil
		}
		if resp.ReasonCode != "" {
			return Outcome{Verdict: VerdictPending, CostNanos: out.CostNanos}, errors.New("screening_unavailable")
		}
	}
	return out, nil
}

func validCharacterReason(reason string) bool {
	switch reason {
	case "real_person", "third_party_ip", "sexual_content", "age_inappropriate", "prompt_injection":
		return true
	}
	return false
}

func publicationDeadline(ctx context.Context) uint64 {
	deadline, _ := ctx.Deadline()
	return boundedUint64(deadline.UnixMilli())
}

func (s *Service) characterRelease(ctx context.Context, tx pgx.Tx, course string) error {
	if s.cfg.CharacterExportsEnabled {
		return nil
	}
	var attached bool
	if err := tx.QueryRow(ctx, `SELECT snapshot ? 'cast' FROM publication_drafts WHERE course_id=$1 AND verdict='pass' ORDER BY number DESC LIMIT 1`, course).Scan(&attached); err != nil {
		return err
	}
	if attached {
		return ErrCharacterRelease
	}
	return nil
}

// SuppressCharacter is a moderator-only content-hash takedown. It never edits snapshot or grading bytes.
func (s *Service) SuppressCharacter(ctx context.Context, actor, snapshotID, reason string) error {
	if err := s.requireModerator(ctx, actor); err != nil {
		return err
	}
	ledger, ok := s.ledger.(interface {
		RecordCharacterSuppression(context.Context, string, string) error
	})
	if !ok {
		return errors.New("suppression ledger unavailable")
	}
	if err := ledger.RecordCharacterSuppression(ctx, snapshotID, reason); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO character_suppressions(snapshot_id,reason_code) VALUES($1,$2) ON CONFLICT DO NOTHING`, snapshotID, reason)
	return err
}

// emitCharacterPublication claims the durable version marker, including opted-out versions.
func (s *Service) emitCharacterPublication(ctx context.Context, owner, draft string) {
	var raw []byte
	var number int
	err := s.pool.QueryRow(ctx, `UPDATE publication_drafts SET character_event_emitted=true WHERE id=$1 AND verdict='pass' AND NOT character_event_emitted AND (snapshot ? 'cast') RETURNING snapshot,number`, draft).Scan(&raw, &number)
	if err != nil {
		return
	}
	var snap Snapshot
	if json.Unmarshal(raw, &snap) != nil {
		return
	}
	total, custom := 0, 0
	for _, sp := range publicationSpeakers(snap) {
		total++
		if !sp.IsProvisional {
			custom++
		}
	}
	if total > 0 {
		s.emit(owner, "character_snapshot_published", map[string]any{"course_version": number, "character_count": total, "custom_count": custom})
	}
}
