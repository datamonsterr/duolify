package narration

import (
	"context"
	"errors"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// Deleter is implemented by stores that can delete an object (a versioned bucket keeps its versions).
type Deleter interface {
	Delete(ctx context.Context, key string) error
}

// Restorer is implemented by versioned stores that can bring back the latest retained version
// of a deleted key. It reports whether anything was restored.
type Restorer interface {
	Restore(ctx context.Context, key string) (bool, error)
}

// ErrNotFound hides other Learners' slides.
var ErrNotFound = player.ErrNarrationNotFound

// Retry queues a failed slide again under the same content hash. The failed attempt released its
// reservation, so only a delivered artifact is ever charged, and only once.
func (s *Service) Retry(ctx context.Context, owner, lessonID, slideID string) (string, error) {
	rows, err := s.opts.Pool.Query(ctx, `UPDATE slide_narrations SET status='pending',failure_code='',updated_at=now()
		WHERE lesson_id=$1::uuid AND learner_id=$2::uuid AND slide_id=$3 AND narration_text<>''
		AND ((status='failed' AND failure_code<>'missing_text') OR (status='disabled' AND failure_code='insufficient_credits')) RETURNING voice_key`, lessonID, owner, slideID)
	if err != nil {
		return "", err
	}
	var voices []string
	for rows.Next() {
		var v string
		if err = rows.Scan(&v); err != nil {
			rows.Close()
			return "", err
		}
		voices = append(voices, v)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return "", err
	}
	if len(voices) == 0 {
		var st string
		err = s.opts.Pool.QueryRow(ctx, `SELECT status FROM slide_narrations WHERE lesson_id=$1::uuid AND learner_id=$2::uuid AND slide_id=$3 ORDER BY (status='ready') DESC, (voice_key=COALESCE((SELECT narration_voice FROM learner_profiles WHERE learner_id=$2::uuid),'default')) DESC, updated_at DESC LIMIT 1`, lessonID, owner, slideID).Scan(&st)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return "", ErrNotFound
		case err != nil:
			return "", err
		case st == StatusReady:
			return RecoveryOK, nil
		case st == StatusPending:
			return RecoveryPending, nil
		}
		return RecoveryFailed, nil
	}
	if s.client == nil {
		return "", errors.New("narration queue not configured")
	}
	for _, voice := range voices {
		if _, err = s.client.Insert(ctx, Args{LearnerID: owner, LessonID: lessonID, SlideID: slideID, VoiceKey: voice}, &river.InsertOpts{MaxAttempts: s.opts.JobAttempts}); err != nil {
			return "", err
		}
	}
	return RecoveryPending, nil
}

func (s *Service) present(ctx context.Context, key string) bool {
	raw, err := s.opts.Objects.Get(ctx, key)
	return err == nil && len(raw) > 0
}

// Recover repairs a ready slide whose object is missing, never charging the Learner. Order:
// restore the latest retained bucket version of the key; repoint to a retained prior settings
// version of the same text; otherwise queue a tracked operator regeneration.
func (s *Service) Recover(ctx context.Context, owner, lessonID, slideID string) (string, error) {
	var st, failure, hash, audioKey, alignmentKey, text, voiceID string
	err := s.opts.Pool.QueryRow(ctx, `SELECT status,failure_code,content_hash,audio_key,alignment_key,narration_text,voice_id FROM slide_narrations
		WHERE lesson_id=$1::uuid AND learner_id=$2::uuid AND slide_id=$3 ORDER BY (status='ready') DESC, (voice_key=COALESCE((SELECT narration_voice FROM learner_profiles WHERE learner_id=$2::uuid),'default')) DESC, updated_at DESC LIMIT 1`, lessonID, owner, slideID).Scan(&st, &failure, &hash, &audioKey, &alignmentKey, &text, &voiceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	switch {
	case st == StatusPending && failure == "object_missing":
		return RecoveryRegenerating, s.requestRegeneration(ctx, s.opts.Pool, hash)
	case st == StatusPending:
		return RecoveryPending, nil
	case st != StatusReady:
		return RecoveryFailed, nil
	}
	if s.present(ctx, audioKey) && s.present(ctx, alignmentKey) {
		return RecoveryOK, nil
	}
	if restorer, ok := s.opts.Objects.(Restorer); ok {
		for _, key := range []string{audioKey, alignmentKey} {
			if !s.present(ctx, key) {
				if _, err = restorer.Restore(ctx, key); err != nil {
					s.opts.Logger.Warn("narration version restore failed", "error", err)
				}
			}
		}
		if s.present(ctx, audioKey) && s.present(ctx, alignmentKey) {
			_, err = s.opts.Pool.Exec(ctx, `UPDATE narration_assets SET state='ready',updated_at=now() WHERE content_hash=$1`, hash)
			return RecoveryRestored, err
		}
	}
	// Retained prior settings versions of the same normalized text, newest first.
	rows, err := s.opts.Pool.Query(ctx, `SELECT content_hash,voice_id,model_id,settings_fingerprint,audio_key,alignment_key,characters,audio_bytes,duration_ms
		FROM narration_assets WHERE text_hash=$2 AND content_hash<>$1 AND voice_id=$3 AND state='ready' AND provider=COALESCE((SELECT provider FROM narration_assets WHERE content_hash=$1),'elevenlabs') ORDER BY created_at DESC`, hash, TextHash(NormalizeText(text)), voiceID)
	if err != nil {
		return "", err
	}
	type version struct{ meta }
	var versions []version
	for rows.Next() {
		var v version
		if err = rows.Scan(&v.hash, &v.voiceID, &v.modelID, &v.fingerprint, &v.audioKey, &v.alignmentKey, &v.characters, &v.bytes, &v.duration); err != nil {
			rows.Close()
			return "", err
		}
		versions = append(versions, v)
	}
	rows.Close()
	for _, v := range versions {
		if !s.present(ctx, v.audioKey) || !s.present(ctx, v.alignmentKey) {
			continue
		}
		// Every slide sharing the broken asset moves to the retained version together.
		_, err = s.opts.Pool.Exec(ctx, `UPDATE slide_narrations SET content_hash=$2,voice_id=$3,model_id=$4,settings_fingerprint=$5,audio_key=$6,alignment_key=$7,
			characters=$8,audio_bytes=$9,duration_ms=$10,version=version+1,updated_at=now() WHERE content_hash=$1 AND status='ready'`,
			hash, v.hash, v.voiceID, v.modelID, v.fingerprint, v.audioKey, v.alignmentKey, v.characters, v.bytes, v.duration)
		if err == nil {
			_, err = s.opts.Pool.Exec(ctx, `UPDATE narration_assets SET state='missing',updated_at=now() WHERE content_hash=$1`, hash)
		}
		return RecoveryRestored, err
	}
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err = tx.Exec(ctx, `UPDATE narration_assets SET state='missing',updated_at=now() WHERE content_hash=$1`, hash); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `UPDATE slide_narrations SET status='pending',failure_code='object_missing',updated_at=now() WHERE content_hash=$1 AND status='ready'`, hash); err != nil {
		return "", err
	}
	if err = s.requestRegeneration(ctx, tx, hash); err != nil {
		return "", err
	}
	return RecoveryRegenerating, tx.Commit(ctx)
}

// RegenerateMissing is the operator queue: it regenerates every requested asset at its original
// hash and settings with platform-funded provider usage. No Learner reservation or charge exists.
func (s *Service) RegenerateMissing(ctx context.Context) (int, error) {
	rows, err := s.opts.Pool.Query(ctx, `SELECT r.id::text,a.content_hash,a.voice_id,a.model_id,a.settings_fingerprint,a.audio_key,a.alignment_key,a.provider
		FROM narration_regenerations r JOIN narration_assets a USING(content_hash) WHERE r.status='requested' ORDER BY r.requested_at`)
	if err != nil {
		return 0, err
	}
	type job struct{ id, hash, voice, model, fingerprint, audioKey, alignmentKey, provider string }
	var jobs []job
	for rows.Next() {
		var j job
		if err = rows.Scan(&j.id, &j.hash, &j.voice, &j.model, &j.fingerprint, &j.audioKey, &j.alignmentKey, &j.provider); err != nil {
			rows.Close()
			return 0, err
		}
		jobs = append(jobs, j)
	}
	rows.Close()
	done := 0
	for _, j := range jobs {
		if err = s.regenerate(ctx, j.id, j.hash, j.voice, j.model, j.fingerprint, j.audioKey, j.alignmentKey, j.provider); errors.Is(err, errBudgetExhausted) || errors.Is(err, errRegenerationBusy) {
			continue // busy or budget-blocked requests stay queued
		} else if err != nil {
			s.opts.Logger.Warn("narration regeneration failed", "id", j.id, "code", failureCode(err))
			// Best effort: the request stays 'requested' if this fails and is retried next cycle.
			_, _ = s.opts.Pool.Exec(ctx, `UPDATE narration_regenerations SET status='failed',failure_code=$2,completed_at=now() WHERE id=$1::uuid`, j.id, failureCode(err))
			continue
		}
		done++
	}
	return done, nil
}

var errRegenerationBusy = errors.New("narration regeneration already claimed")

func (s *Service) regenerate(ctx context.Context, id, hash, voice, model, fingerprint, audioKey, alignmentKey, provider string) error {
	select {
	case s.lockSlots <- struct{}{}:
		defer func() { <-s.lockSlots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	conn, err := s.opts.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var locked bool
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, "narration:"+hash).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return errRegenerationBusy
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(hashtextextended($1,0))`, "narration:"+hash)
	}()
	var raw, owner, lesson, slide, voiceKey, speaker string
	err = s.opts.Pool.QueryRow(ctx, `SELECT n.narration_text,n.learner_id::text,n.lesson_id::text,n.slide_id,n.voice_key,n.speaker_key
		FROM slide_narrations n JOIN narration_regenerations r USING(content_hash)
		WHERE r.id=$1 AND r.status='requested' ORDER BY n.lesson_id,n.slide_id LIMIT 1`, id).
		Scan(&raw, &owner, &lesson, &slide, &voiceKey, &speaker)
	if errors.Is(err, pgx.ErrNoRows) {
		return errRegenerationBusy
	}
	if err != nil {
		return err
	}
	text := NormalizeText(raw)
	if ValidText(text) != nil {
		return ErrMissingText
	}
	language, err := s.courseLanguage(ctx, owner, lesson)
	if err != nil {
		return err
	}
	if speaker != "" {
		voiceKey, _ = s.voices.ResolveCharacter(voiceKey, func() bool { return true })
	}
	limits := &enginev1.UsageLimits{Provider: provider, Model: model, TimeoutMs: 30000, MaxAttempts: 2}
	resolved, err := s.opts.Speech.SynthesizeNarration(ctx, &enginev1.SynthesizeNarrationRequest{
		SlideId: slide, SegmentId: segmentOf(speaker, slide), SpeakerKey: speaker, VoiceKey: voiceKey, Language: language, ResolveOnly: true, Limits: limits,
	})
	if err != nil {
		return err
	}
	if !validResolution(resolved) {
		return ErrOutput
	}
	route := resolvedAsset(text, resolved)
	if legacy, ok := route.legacyHash(text); ok && legacy == hash {
		route.hash, route.legacy = legacy, true
	}
	if route.hash != hash || route.voiceID != voice || route.modelID != model || route.fingerprint != fingerprint || route.provider != provider {
		return ErrOutput
	}
	route.audioKey, route.alignmentKey = audioKey, alignmentKey
	it := &item{hash: hash, text: text, language: language, callVoice: voiceKey, characters: len([]rune(text)), meta: route,
		routes: []meta{route}, slides: []string{slide}, rows: map[string]rowInfo{slide: {voice: voiceKey, speaker: speaker}}}
	policy, err := pricing.NewQuotes(s.opts.Pool).ActivePolicy(ctx)
	if err != nil {
		return err
	}
	code, err := s.admitNext(ctx, "00000000-0000-0000-0000-000000000000", it, policy, 0, false, nil)
	if err != nil {
		return err
	}
	if code != "" {
		return errBudgetExhausted
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	response, err := s.callRoute(callCtx, it, limits)
	if err != nil {
		return err
	}
	if it.meta.audioKey != audioKey || it.meta.alignmentKey != alignmentKey {
		return ErrOutput
	}
	if route.legacy {
		response.TimingSource = ""
	}
	if err = s.store(ctx, it, response); err != nil {
		return err
	}
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err = tx.Exec(ctx, `UPDATE narration_assets SET state='ready',updated_at=now() WHERE content_hash=$1`, hash); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE slide_narrations SET status='ready',failure_code='',updated_at=now() WHERE content_hash=$1 AND status='pending' AND failure_code='object_missing'`, hash); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE narration_regenerations SET status='completed',characters=$2,completed_at=now() WHERE id=$1::uuid`, id, int(response.Characters)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// PurgeUnreferenced deletes assets whose last reference ended before the retention window.
// Rows and objects go together; a versioned bucket keeps its non-current versions.
func (s *Service) PurgeUnreferenced(ctx context.Context) (int, error) {
	deleter, ok := s.opts.Objects.(Deleter)
	if !ok {
		return 0, errors.New("narration object store cannot delete")
	}
	cutoff := time.Now().Add(-s.opts.Retention)
	rows, err := s.opts.Pool.Query(ctx, `SELECT content_hash,audio_key,alignment_key FROM narration_assets a
		WHERE ref_count=0 AND unreferenced_at IS NOT NULL AND unreferenced_at<=$1
		AND NOT EXISTS(SELECT 1 FROM slide_narrations n WHERE n.content_hash=a.content_hash)
		AND NOT EXISTS(SELECT 1 FROM published_narration_pins p WHERE p.content_hash=a.content_hash AND (p.unpinned_at IS NULL OR p.unpinned_at>$1))
		ORDER BY unreferenced_at LIMIT 500`, cutoff)
	if err != nil {
		return 0, err
	}
	type expired struct{ hash, audio, alignment string }
	var list []expired
	for rows.Next() {
		var e expired
		if err = rows.Scan(&e.hash, &e.audio, &e.alignment); err != nil {
			rows.Close()
			return 0, err
		}
		list = append(list, e)
	}
	rows.Close()
	purged := 0
	for _, e := range list {
		// Re-check inside the delete: a new reference since the scan keeps the asset.
		tag, err := s.opts.Pool.Exec(ctx, `DELETE FROM narration_assets a WHERE content_hash=$1 AND ref_count=0 AND NOT EXISTS(SELECT 1 FROM slide_narrations n WHERE n.content_hash=a.content_hash)
		AND NOT EXISTS(SELECT 1 FROM published_narration_pins p WHERE p.content_hash=a.content_hash AND (p.unpinned_at IS NULL OR p.unpinned_at>$2))`, e.hash, cutoff)
		if err != nil {
			return purged, err
		}
		if tag.RowsAffected() == 0 {
			continue
		}
		for _, key := range []string{e.audio, e.alignment} {
			if err = deleter.Delete(ctx, key); err != nil {
				return purged, err
			}
		}
		purged++
	}
	return purged, nil
}

// RunMaintenance drains the operator regeneration queue until ctx ends. Purging expired assets
// is MaintainOrphanedAudio, which the API process runs because it holds the object-store keys.
func (s *Service) RunMaintenance(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		if _, err := s.RegenerateMissing(ctx); err != nil {
			s.opts.Logger.Warn("narration regeneration run failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// MaintainOrphanedAudio purges unreferenced narration audio until ctx ends. It needs only the
// database and an object store that can delete, never the speech provider, so it runs whether or
// not narration synthesis is enabled: audio of erased learners must leave storage either way. The
// reference count is re-checked inside every delete, so concurrent replicas are safe.
func (s *Service) MaintainOrphanedAudio(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		if _, err := s.PurgeUnreferenced(ctx); err != nil {
			s.opts.Logger.Warn("narration purge failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
