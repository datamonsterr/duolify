package characters

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"time"

	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Screener is deliberately separate from prompt-to-spec generation and Credits.
type Screener interface {
	ScreenCharacter(context.Context, *pb.ScreenCharacterRequest) (*pb.ScreenCharacterResponse, error)
}

// ConfigureScreening injects the platform-paid safety boundary.
func (s *Service) ConfigureScreening(screen Screener) { s.screener = screen }

// Maintain enforces retention even when no Character endpoint is visited.
func (s *Service) Maintain(ctx context.Context) {
	_ = s.RecoverCreateJobs(ctx)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.RecoverCreateJobs(ctx)
			_ = s.Purge(ctx)
		}
	}
}

// Record is an owned approved immutable Character version.
type Record struct {
	ID      string        `json:"id"`
	Version int           `json:"version"`
	Spec    CharacterSpec `json:"spec"`
}

// EditRequest pins the version the learner inspected.
type EditRequest struct {
	ExpectedVersion int             `json:"expected_version"`
	Spec            json.RawMessage `json:"spec"`
}

// CastSelection contains one guide and up to two distinct supporting IDs.
type CastSelection struct {
	Guide      string   `json:"guide"`
	Supporting []string `json:"supporting"`
}

// CastRecord is the versioned Course selection.
type CastRecord struct {
	CastSelection
	Version  int                 `json:"version"`
	Snapshot *LessonCastSnapshot `json:"snapshot,omitempty"`
}

// ErrNotFound conceals foreign and missing private identifiers alike.
var ErrNotFound = errors.New("not_found")

func boundedReason(reason string) string {
	switch reason {
	case "real_person", "third_party_ip", "sexual_content", "age_inappropriate", "prompt_injection", "invalid_spec", "limit_exceeded", "screening_unavailable":
		return reason
	}
	return "screening_unavailable"
}

// List returns current owned Characters only.
func (s *Service) List(ctx context.Context, owner string) ([]Record, error) {
	rows, err := s.pool.Query(ctx, `SELECT c.id::text,c.current_version,v.spec FROM characters c JOIN character_versions v ON v.character_id=c.id AND v.version=c.current_version WHERE c.learner_id=$1 AND c.deleted_at IS NULL ORDER BY c.created_at,c.id`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		var r Record
		var raw []byte
		if err := rows.Scan(&r.ID, &r.Version, &raw); err != nil {
			return nil, err
		}
		spec, err := ValidateSpecJSON(raw)
		if err != nil {
			continue
		}
		r.Spec = *spec
		out = append(out, r)
	}
	return out, rows.Err()
}

// Get reads a current owned Character without disclosing foreign content.
func (s *Service) Get(ctx context.Context, owner, id string) (Record, error) {
	var r Record
	var raw []byte
	if _, err := uuid.Parse(id); err != nil {
		return r, ErrNotFound
	}
	err := s.pool.QueryRow(ctx, `SELECT c.id::text,c.current_version,v.spec FROM characters c JOIN character_versions v ON v.character_id=c.id AND v.version=c.current_version WHERE c.id=$1 AND c.learner_id=$2 AND c.deleted_at IS NULL`, id, owner).Scan(&r.ID, &r.Version, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	spec, err := ValidateSpecJSON(raw)
	if err != nil {
		return r, errors.New("invalid_spec")
	}
	r.Spec = *spec
	return r, nil
}

// beginAttempt commits the quota before validation or a provider call. The
// learner row serializes parallel attempts, including rejections and crashes.
func (s *Service) beginAttempt(ctx context.Context, owner, id string) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM learners WHERE id=$1 FOR UPDATE`, owner).Scan(&locked); err != nil {
		return "", ErrNotFound
	}
	var n int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM character_screenings WHERE learner_id=$1 AND operation_id IS NULL AND created_at>now()-interval '1 hour'`, owner).Scan(&n); err != nil {
		return "", err
	}
	if n >= 20 {
		return "", errors.New("limit_exceeded")
	}
	var op string
	if err = tx.QueryRow(ctx, `INSERT INTO character_screenings(learner_id,character_id) VALUES($1,$2) RETURNING id::text`, owner, id).Scan(&op); err != nil {
		return "", err
	}
	return op, tx.Commit(ctx)
}

// Edit validates and screens a free structured edit before an immutable write.
func (s *Service) Edit(ctx context.Context, owner, id string, in EditRequest) (Record, error) {
	old, err := s.Get(ctx, owner, id)
	if err != nil {
		return Record{}, err
	}
	op, err := s.beginAttempt(ctx, owner, id)
	if err != nil {
		return Record{}, err
	}
	reject := func(reason string) (Record, error) {
		reason = boundedReason(reason)
		_, _ = s.pool.Exec(ctx, `UPDATE character_screenings SET verdict='rejected',reason_code=$2 WHERE id=$1`, op, reason)
		s.emit(ctx, owner, "character_rejected", map[string]any{"operation": "manual_edit", "reason_code": reason})
		return Record{}, errors.New(reason)
	}
	spec, err := ValidateSpecJSON(in.Spec)
	if err != nil {
		return reject("invalid_spec")
	}
	if in.ExpectedVersion != old.Version {
		return Record{}, errors.New("version_conflict")
	}
	if s.screener == nil {
		return reject("screening_unavailable")
	}
	compiled, err := CompileSVG(spec, "idle")
	if err != nil {
		return reject("invalid_spec")
	}
	screenCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	response, err := s.screener.ScreenCharacter(screenCtx, &pb.ScreenCharacterRequest{SpecJson: string(in.Spec), CompiledAppearance: compiled})
	cancel()
	if response != nil {
		for _, a := range response.Attempts {
			if a == nil {
				continue
			}
			s.emit(ctx, owner, "$ai_generation", map[string]any{"feature": "character_screen", "provider": a.Provider, "model": a.Model, "input_tokens": a.InputTokens, "output_tokens": a.OutputTokens, "cache_write_tokens": a.CacheWriteTokens, "cache_read_tokens": a.CacheReadTokens, "latency_ms": a.LatencyMs, "attempt": a.Attempt, "fallback_used": a.FallbackUsed, "succeeded": a.Delivered, "usage_available": !a.UsageUnavailable, "error_code": a.ErrorCode})
		}
	}
	if err != nil || response == nil || response.PolicyVersion != "character-v1" || len(response.Attempts) == 0 || len(response.Attempts) > 2 {
		return reject("screening_unavailable")
	}
	last := response.Attempts[len(response.Attempts)-1]
	if response.Approved && (last == nil || !last.Delivered || last.UsageUnavailable || uint64(last.InputTokens)+uint64(last.CacheWriteTokens)+uint64(last.CacheReadTokens) > 18000 || last.OutputTokens > 200) {
		return reject("screening_unavailable")
	}
	if !response.Approved {
		return reject(response.ReasonCode)
	}
	if response.ReasonCode != "" {
		return reject("screening_unavailable")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Record{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `UPDATE characters SET current_version=current_version+1 WHERE id=$1 AND learner_id=$2 AND deleted_at IS NULL AND current_version=$3`, id, owner, in.ExpectedVersion)
	if err != nil {
		return Record{}, err
	}
	if tag.RowsAffected() != 1 {
		return Record{}, errors.New("version_conflict")
	}
	version := old.Version + 1
	if _, err = tx.Exec(ctx, `INSERT INTO character_versions(character_id,version,spec) VALUES($1,$2,$3)`, id, version, in.Spec); err != nil {
		return Record{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE character_screenings SET verdict='approved',policy_version=$2 WHERE id=$1`, op, response.PolicyVersion); err != nil {
		return Record{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Record{}, err
	}
	s.emit(ctx, owner, "character_updated", map[string]any{"character_id": id, "character_version": version, "fields_changed": changedFields(old.Spec, *spec)})
	return Record{ID: id, Version: version, Spec: *spec}, nil
}
func changedFields(before, after CharacterSpec) []string {
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(after)
	var av, bv map[string]any
	_ = json.Unmarshal(a, &av)
	_ = json.Unmarshal(b, &bv)
	out := []string{}
	for _, group := range []string{"info", "personality", "appearance"} {
		for key, value := range av[group].(map[string]any) {
			if !reflect.DeepEqual(value, bv[group].(map[string]any)[key]) {
				out = append(out, group+"."+key)
			}
		}
	}
	if before.VoiceKey != after.VoiceKey {
		out = append(out, "voice_key")
	}
	sort.Strings(out)
	return out
}

// Delete removes a Character from future selections.
func (s *Service) Delete(ctx context.Context, owner, id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return ErrNotFound
	}
	tag, err := s.pool.Exec(ctx, `UPDATE characters SET deleted_at=now() WHERE id=$1 AND learner_id=$2 AND deleted_at IS NULL`, id, owner)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	s.emit(ctx, owner, "character_deleted", map[string]any{"character_id": id})
	return nil
}

// ValidateCastSelection enforces distinct roles and the supporting bound.
func ValidateCastSelection(in CastSelection) error {
	if in.Guide == "" && len(in.Supporting) > 0 || len(in.Supporting) > 2 {
		return errors.New("invalid_cast")
	}
	seen := map[string]bool{in.Guide: true}
	for _, id := range in.Supporting {
		if id == "" || seen[id] {
			return errors.New("invalid_cast")
		}
		seen[id] = true
	}
	return nil
}
func (s *Service) checkCourse(ctx context.Context, owner, course string) error {
	if _, err := uuid.Parse(course); err != nil {
		return ErrNotFound
	}
	var found bool
	// Only authored/forked courses are owner records. An enrollment alone is
	// insufficient; published copies remain under their publication's Cast.
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM courses WHERE id=$1 AND learner_id=$2 AND deleted_at IS NULL)`, course, owner).Scan(&found)
	if err != nil {
		return err
	}
	if !found {
		return ErrNotFound
	}
	return nil
}
func builtIn(id string) (CharacterSpec, bool) {
	switch id {
	case "default-guide":
		return DefaultGuideFixture(), true
	case "default-supporter":
		return DefaultSupporterFixture(), true
	case "default-companion":
		return DefaultCompanionFixture(), true
	}
	return CharacterSpec{}, false
}
func (s *Service) resolve(ctx context.Context, owner, id string) (Record, error) {
	if spec, ok := builtIn(id); ok {
		return Record{ID: id, Version: 1, Spec: spec}, nil
	}
	return s.Get(ctx, owner, id)
}

// SelectCast saves a free owner-checked selection for future jobs.
func (s *Service) SelectCast(ctx context.Context, owner, course string, in CastSelection) (CastRecord, error) {
	if err := s.checkCourse(ctx, owner, course); err != nil {
		return CastRecord{}, err
	}
	if err := ValidateCastSelection(in); err != nil {
		return CastRecord{}, err
	}
	if in.Guide != "" {
		for _, id := range append([]string{in.Guide}, in.Supporting...) {
			if _, err := s.resolve(ctx, owner, id); err != nil {
				return CastRecord{}, ErrNotFound
			}
		}
	}
	if in.Supporting == nil {
		in.Supporting = []string{}
	}
	raw, _ := json.Marshal(in)
	var version int
	if err := s.pool.QueryRow(ctx, `INSERT INTO course_casts(course_id,learner_id,selection) VALUES($1,$2,$3) ON CONFLICT(course_id) DO UPDATE SET selection=EXCLUDED.selection,version=course_casts.version+1 RETURNING version`, course, owner, raw).Scan(&version); err != nil {
		return CastRecord{}, err
	}
	source := "custom"
	if in.Guide == "" {
		source = "default"
	}
	s.emit(ctx, owner, "course_cast_selected", map[string]any{"cast_source": source, "supporting_count": len(in.Supporting), "cast_version": version})
	return CastRecord{CastSelection: in, Version: version}, nil
}

// ReadCast returns only an owned Course selection.
func (s *Service) ReadCast(ctx context.Context, owner, course string) (CastRecord, error) {
	if err := s.checkCourse(ctx, owner, course); err != nil {
		return CastRecord{}, err
	}
	var raw []byte
	var result CastRecord
	err := s.pool.QueryRow(ctx, `SELECT selection,version FROM course_casts WHERE course_id=$1 AND learner_id=$2`, course, owner).Scan(&raw, &result.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		result.Supporting = []string{}
		return result, nil
	}
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(raw, &result.CastSelection)
	return result, err
}
func (s *Service) effectiveCast(ctx context.Context, owner, course string) (LessonCastSnapshot, error) {
	selected, err := s.ReadCast(ctx, owner, course)
	if err != nil {
		return LessonCastSnapshot{}, err
	}
	manifest, _ := ProjectDefaultCastManifest(course, "")
	out := manifest.Cast
	if selected.Guide == "" {
		return out, nil
	}
	out.Speakers = map[string]PinnedSpeaker{}
	out.CastSource = "custom"
	ids := append([]string{selected.Guide}, selected.Supporting...)
	for index, id := range ids {
		key := "guide"
		role := "guide"
		fallback := "default-guide"
		if index > 0 {
			role = "supporter"
			key = "supporter"
			fallback = "default-supporter"
		}
		if index == 2 {
			key = "supporter_2"
			fallback = "default-companion"
		}
		r, err := s.resolve(ctx, owner, id)
		if err != nil {
			r, _ = s.resolve(ctx, owner, fallback)
			out.FallbackUsed = true
		}
		_, provisional := builtIn(r.ID)
		out.Speakers[key] = PinnedSpeaker{SpeakerKey: key, Role: role, CharacterID: r.ID, CharacterVersion: r.Version, Spec: r.Spec, IsProvisional: provisional}
	}
	return out, nil
}

// PinCast persists a full independent payload. Concurrent first attempts use
// the winner and retries never resolve current private versions again.
func (s *Service) PinCast(ctx context.Context, owner, course, lesson string) (LessonCastSnapshot, error) {
	var out LessonCastSnapshot
	if err := s.checkCourse(ctx, owner, course); err != nil {
		return out, err
	}
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT snapshot FROM lesson_cast_snapshots WHERE course_id=$1 AND lesson_key=$2 AND learner_id=$3`, course, lesson, owner).Scan(&raw)
	if err == nil {
		err = json.Unmarshal(raw, &out)
		return out, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	out, err = s.effectiveCast(ctx, owner, course)
	if err != nil {
		return out, err
	}
	raw, _ = json.Marshal(out)
	_, err = s.pool.Exec(ctx, `INSERT INTO lesson_cast_snapshots(course_id,learner_id,lesson_key,snapshot) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, course, owner, lesson, raw)
	if err != nil {
		return out, err
	}
	err = s.pool.QueryRow(ctx, `SELECT snapshot FROM lesson_cast_snapshots WHERE course_id=$1 AND lesson_key=$2`, course, lesson).Scan(&raw)
	if err == nil {
		err = json.Unmarshal(raw, &out)
	}
	return out, err
}

// Purge retains independent historical snapshots and erases unreferenced
// private records within 30 days. Bundled rigs have no per-learner S3 objects.
func (s *Service) Purge(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `UPDATE character_create_jobs SET prompt=NULL WHERE prompt_expires_at<=now() OR state IN ('succeeded','failed','rejected','cancelled'); DELETE FROM character_create_jobs WHERE character_id IN (SELECT id FROM characters WHERE deleted_at<now()-interval '30 days')`); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM character_screenings WHERE created_at<now()-interval '30 days'`)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM characters WHERE deleted_at<now()-interval '30 days'`)
	return err
}
func (s *Service) emit(ctx context.Context, owner, event string, props map[string]any) {
	if s.events == nil {
		return
	}
	var optedOut bool
	if err := s.pool.QueryRow(ctx, `SELECT COALESCE((SELECT analytics_opt_out FROM learner_profiles WHERE learner_id=$1),false)`, owner).Scan(&optedOut); err != nil || optedOut {
		return
	}
	props["source"] = "api"
	props["platform"] = "web"
	props["app_version"] = "0.1.0"
	_ = s.events.Capture(owner, event, props)
}
