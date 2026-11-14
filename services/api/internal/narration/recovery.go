package narration

import (
	"context"
	"errors"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/datamonsterr/duolify/services/api/internal/player"
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
		WHERE lesson_id::text=$1 AND learner_id::text=$2 AND slide_id=$3 AND narration_text<>''
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
		err = s.opts.Pool.QueryRow(ctx, `SELECT status FROM slide_narrations WHERE lesson_id::text=$1 AND learner_id::text=$2 AND slide_id=$3 ORDER BY (status='ready') DESC, (voice_key=COALESCE((SELECT narration_voice FROM learner_profiles WHERE learner_id::text=$2),'default')) DESC, updated_at DESC LIMIT 1`, lessonID, owner, slideID).Scan(&st)
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
		WHERE lesson_id::text=$1 AND learner_id::text=$2 AND slide_id=$3 ORDER BY (status='ready') DESC, (voice_key=COALESCE((SELECT narration_voice FROM learner_profiles WHERE learner_id::text=$2),'default')) DESC, updated_at DESC LIMIT 1`, lessonID, owner, slideID).Scan(&st, &failure, &hash, &audioKey, &alignmentKey, &text, &voiceID)
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
		FROM narration_assets WHERE text_hash=$2 AND content_hash<>$1 AND voice_id=$3 AND state='ready' ORDER BY created_at DESC`, hash, TextHash(NormalizeText(text)), voiceID)
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
	rows, err := s.opts.Pool.Query(ctx, `SELECT r.id::text,a.content_hash,a.voice_id,a.model_id,a.settings_fingerprint,a.audio_key,a.alignment_key
		FROM narration_regenerations r JOIN narration_assets a USING(content_hash) WHERE r.status='requested' ORDER BY r.requested_at`)
	if err != nil {
		return 0, err
	}
	type job struct{ id, hash, voice, model, fingerprint, audioKey, alignmentKey string }
	var jobs []job
	for rows.Next() {
		var j job
		if err = rows.Scan(&j.id, &j.hash, &j.voice, &j.model, &j.fingerprint, &j.audioKey, &j.alignmentKey); err != nil {
			rows.Close()
			return 0, err
		}
		jobs = append(jobs, j)
	}
	rows.Close()
	done := 0
	for _, j := range jobs {
		if err = s.regenerate(ctx, j.id, j.hash, j.voice, j.model, j.fingerprint, j.audioKey, j.alignmentKey); err != nil {
			s.opts.Logger.Warn("narration regeneration failed", "id", j.id, "code", failureCode(err))
			_, _ = s.opts.Pool.Exec(ctx, `UPDATE narration_regenerations SET status='failed',failure_code=$2,completed_at=now() WHERE id::text=$1`, j.id, failureCode(err))
			continue
		}
		done++
	}
	return done, nil
}

func (s *Service) regenerate(ctx context.Context, id, hash, voice, model, fingerprint, audioKey, alignmentKey string) error {
	var raw string
	if err := s.opts.Pool.QueryRow(ctx, `SELECT narration_text FROM slide_narrations WHERE content_hash=$1 LIMIT 1`, hash).Scan(&raw); err != nil {
		return ErrMissingText
	}
	text := NormalizeText(raw)
	if ValidText(text) != nil {
		return ErrMissingText
	}
	resolved, err := s.opts.Speech.SynthesizeNarration(ctx, &enginev1.SynthesizeNarrationRequest{SlideId: "regenerate", VoiceKey: VoiceKey, ResolveOnly: true})
	if err != nil {
		return err
	}
	// Only the identical hash may be regenerated: changed settings would need a new, charged version.
	if resolved.VoiceId != voice || resolved.ModelId != model || resolved.SettingsFingerprint != fingerprint || ContentHash(text, voice, model, fingerprint) != hash {
		return ErrOutput
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	response, err := s.opts.Speech.SynthesizeNarration(callCtx, &enginev1.SynthesizeNarrationRequest{SlideId: "regenerate", Text: text, VoiceKey: VoiceKey,
		Limits: &enginev1.UsageLimits{Provider: "elevenlabs", Model: model}})
	if err == nil {
		err = ValidateOutput(text, response)
	}
	if err != nil {
		return err
	}
	it := &item{hash: hash, meta: meta{audioKey: audioKey, alignmentKey: alignmentKey}}
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
	if _, err = tx.Exec(ctx, `UPDATE narration_regenerations SET status='completed',characters=$2,completed_at=now() WHERE id::text=$1`, id, int(response.Characters)); err != nil {
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
		AND NOT EXISTS(SELECT 1 FROM slide_narrations n WHERE n.content_hash=a.content_hash) ORDER BY unreferenced_at LIMIT 500`, cutoff)
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
		tag, err := s.opts.Pool.Exec(ctx, `DELETE FROM narration_assets a WHERE content_hash=$1 AND ref_count=0 AND NOT EXISTS(SELECT 1 FROM slide_narrations n WHERE n.content_hash=a.content_hash)`, e.hash)
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

// RunMaintenance drains the operator regeneration queue and purges expired assets until ctx ends.
func (s *Service) RunMaintenance(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		if _, err := s.RegenerateMissing(ctx); err != nil {
			s.opts.Logger.Warn("narration regeneration run failed", "error", err)
		}
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
