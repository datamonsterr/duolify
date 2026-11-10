package player

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
)

// SyncEvent represents a non-authoritative playback progress event with a stable action key.
type SyncEvent struct {
	ActionKey  string    `json:"action_key"`
	Version    int       `json:"version"`
	SlideID    string    `json:"slide_id"`
	TimeMS     int       `json:"time_ms"`
	OccurredAt time.Time `json:"occurred_at"`
}

type rawSyncEvent struct {
	ActionKey  *string `json:"action_key"`
	LessonID   *string `json:"lesson_id"`
	Version    *int    `json:"version"`
	SlideID    *string `json:"slide_id"`
	TimeMS     *int    `json:"time_ms"`
	OccurredAt *string `json:"occurred_at"`
}

type rawSyncBatch struct {
	Events []rawSyncEvent `json:"events"`
}

var actionKeyRegex = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

func validateSyncEvent(raw rawSyncEvent, lesson string, m ObjectManifest) (SyncEvent, error) {
	if raw.ActionKey == nil || raw.Version == nil || raw.SlideID == nil || raw.TimeMS == nil || raw.OccurredAt == nil {
		return SyncEvent{}, errInvalid
	}
	if raw.LessonID != nil && *raw.LessonID != lesson {
		return SyncEvent{}, errInvalid
	}
	if !actionKeyRegex.MatchString(*raw.ActionKey) {
		return SyncEvent{}, errInvalid
	}
	if *raw.Version != m.Version || *raw.TimeMS < 0 {
		return SyncEvent{}, errInvalid
	}
	t, err := time.Parse(time.RFC3339Nano, *raw.OccurredAt)
	if err != nil {
		t, err = time.Parse(time.RFC3339, *raw.OccurredAt)
		if err != nil {
			return SyncEvent{}, errInvalid
		}
	}
	found := false
	for _, s := range m.Slides {
		if *raw.SlideID == s.ID {
			if *raw.TimeMS <= s.DurationMS {
				found = true
			}
			break
		}
	}
	if !found {
		return SyncEvent{}, errInvalid
	}
	return SyncEvent{
		ActionKey:  *raw.ActionKey,
		Version:    *raw.Version,
		SlideID:    *raw.SlideID,
		TimeMS:     *raw.TimeMS,
		OccurredAt: t.UTC(),
	}, nil
}

func decodeSyncEvents(reader io.Reader, lesson string, m ObjectManifest) ([]SyncEvent, error) {
	rawBytes, err := io.ReadAll(io.LimitReader(reader, 65536))
	if err != nil || len(rawBytes) == 0 {
		return nil, errInvalid
	}

	// Try decoding as batch
	var batch rawSyncBatch
	dB := json.NewDecoder(bytes.NewReader(rawBytes))
	dB.DisallowUnknownFields()
	var extraB any
	if errB := dB.Decode(&batch); errB == nil && errors.Is(dB.Decode(&extraB), io.EOF) {
		if len(batch.Events) == 0 {
			return nil, errInvalid
		}
		events := make([]SyncEvent, 0, len(batch.Events))
		for _, e := range batch.Events {
			valid, err := validateSyncEvent(e, lesson, m)
			if err != nil {
				return nil, errInvalid
			}
			events = append(events, valid)
		}
		return events, nil
	}

	// Try decoding as single event
	var single rawSyncEvent
	dS := json.NewDecoder(bytes.NewReader(rawBytes))
	dS.DisallowUnknownFields()
	var extraS any
	if errS := dS.Decode(&single); errS == nil && errors.Is(dS.Decode(&extraS), io.EOF) {
		valid, err := validateSyncEvent(single, lesson, m)
		if err != nil {
			return nil, errInvalid
		}
		return []SyncEvent{valid}, nil
	}

	return nil, errInvalid
}

func (s *Service) sync(ctx context.Context, w http.ResponseWriter, r *http.Request, owner, lesson string, m ObjectManifest) {
	if r.Method != http.MethodPost {
		failure(w, http.StatusMethodNotAllowed)
		return
	}

	events, err := decodeSyncEvents(r.Body, lesson, m)
	if err != nil {
		respond(w, http.StatusBadRequest, map[string]string{"error": "invalid sync event"})
		return
	}

	// 1. Verify enrollment and active course
	var courseID string
	err = s.opts.Pool.QueryRow(ctx, `SELECT c.id::text FROM lesson_manifests m
 JOIN course_lesson_stubs l ON l.id=m.lesson_id JOIN course_units u ON u.id=l.unit_id
 JOIN courses c ON c.id=u.course_id JOIN course_enrollments e ON e.course_id=c.id AND e.learner_id=m.learner_id
 WHERE m.learner_id::text=$1 AND m.lesson_id::text=$2 AND m.version=$3 AND c.deleted_at IS NULL`, owner, lesson, m.Version).Scan(&courseID)
	if errors.Is(err, pgx.ErrNoRows) {
		failure(w, http.StatusNotFound)
		return
	}
	if err != nil {
		failure(w, http.StatusServiceUnavailable)
		return
	}

	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		failure(w, http.StatusServiceUnavailable)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	ackedKeys := make([]string, 0, len(events))
	for _, event := range events {
		// Foreign action key check: does this key already belong to another learner?
		var existingOwner string
		err := tx.QueryRow(ctx, "SELECT learner_id::text FROM lesson_playback_sync_events WHERE action_key=$1", event.ActionKey).Scan(&existingOwner)
		if err == nil {
			if existingOwner != owner {
				_ = tx.Rollback(ctx)
				respond(w, http.StatusBadRequest, map[string]string{"error": "foreign action key rejected"})
				return
			}
			// Same owner: already processed, idempotent ack
			ackedKeys = append(ackedKeys, event.ActionKey)
			continue
		} else if !errors.Is(err, pgx.ErrNoRows) {
			failure(w, http.StatusServiceUnavailable)
			return
		}

		// Insert event
		_, err = tx.Exec(ctx, `INSERT INTO lesson_playback_sync_events(action_key, learner_id, lesson_id, version, slide_id, time_ms, occurred_at)
 VALUES($1, $2, $3, $4, $5, $6, $7)`, event.ActionKey, owner, lesson, event.Version, event.SlideID, event.TimeMS, event.OccurredAt)
		if err != nil {
			failure(w, http.StatusServiceUnavailable)
			return
		}

		// Out-of-order merge into lesson_playback_positions
		_, err = tx.Exec(ctx, `INSERT INTO lesson_playback_positions(learner_id, lesson_id, version, slide_id, time_ms, occurred_at)
 VALUES($1, $2, $3, $4, $5, $6)
 ON CONFLICT(learner_id, lesson_id) DO UPDATE SET
   version = EXCLUDED.version,
   slide_id = EXCLUDED.slide_id,
   time_ms = EXCLUDED.time_ms,
   occurred_at = EXCLUDED.occurred_at,
   updated_at = now()
 WHERE lesson_playback_positions.occurred_at <= EXCLUDED.occurred_at`, owner, lesson, event.Version, event.SlideID, event.TimeMS, event.OccurredAt)
		if err != nil {
			failure(w, http.StatusServiceUnavailable)
			return
		}

		ackedKeys = append(ackedKeys, event.ActionKey)
	}

	if err := tx.Commit(ctx); err != nil {
		failure(w, http.StatusServiceUnavailable)
		return
	}

	// Read canonical position
	var p Position
	_ = s.opts.Pool.QueryRow(ctx, `SELECT version, slide_id, time_ms FROM lesson_playback_positions WHERE learner_id::text=$1 AND lesson_id::text=$2`, owner, lesson).Scan(&p.Version, &p.SlideID, &p.TimeMS)

	respond(w, http.StatusOK, map[string]any{
		"synced":      true,
		"action_keys": ackedKeys,
		"position":    p,
	})
}
