// Package retention removes expired data and preserves the state needed for safe retries.
package retention

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Decided defaults: playback sync events 30 days, lesson test answer operations 7 days.
const (
	DefaultPlaybackSyncEventWindow = 30 * 24 * time.Hour
	DefaultAnswerOperationWindow   = 7 * 24 * time.Hour
	DefaultSweepInterval           = 24 * time.Hour
	DefaultBatchSize               = 5000
	teachBackExplanationWindow     = 30 * 24 * time.Hour
)

// Config holds the retention windows, the sweep cadence and the delete batch size.
type Config struct {
	PlaybackSyncEventWindow time.Duration
	AnswerOperationWindow   time.Duration
	SweepInterval           time.Duration
	BatchSize               int
}

// ConfigFromEnv reads the retention settings. Invalid or non-positive values fall back to the
// defaults. The answer operation window never drops below sessionTTL: an operation key must
// outlive any session that can still receive a replayed answer.
func ConfigFromEnv(getenv func(string) string, sessionTTL time.Duration) Config {
	cfg := Config{
		PlaybackSyncEventWindow: duration(getenv("PLAYBACK_SYNC_EVENT_RETENTION"), DefaultPlaybackSyncEventWindow),
		AnswerOperationWindow:   duration(getenv("LESSON_TEST_ANSWER_OPERATION_RETENTION"), DefaultAnswerOperationWindow),
		SweepInterval:           duration(getenv("ROW_RETENTION_SWEEP_INTERVAL"), DefaultSweepInterval),
		BatchSize:               DefaultBatchSize,
	}
	if n, err := strconv.Atoi(getenv("ROW_RETENTION_BATCH_SIZE")); err == nil && n > 0 {
		cfg.BatchSize = n
	}
	if cfg.AnswerOperationWindow < sessionTTL {
		cfg.AnswerOperationWindow = sessionTTL
	}
	return cfg
}

// duration accepts a Go duration ("720h") or whole days ("30d").
func duration(value string, fallback time.Duration) time.Duration {
	value = strings.TrimSpace(value)
	if days, ok := strings.CutSuffix(value, "d"); ok {
		if n, err := strconv.Atoi(days); err == nil && n > 0 {
			return time.Duration(n) * 24 * time.Hour
		}
		return fallback
	}
	if d, err := time.ParseDuration(value); err == nil && d > 0 {
		return d
	}
	return fallback
}

// Result counts what one sweep deleted.
type Result = struct{ PlaybackSyncEvents, AnswerOperations, GuidedChatDrafts int64 }

// Service purges expired rows.
type Service struct {
	pool   *pgxpool.Pool
	cfg    Config
	logger *slog.Logger
}

// New builds the service. A zero field uses its default.
func New(pool *pgxpool.Pool, cfg Config, logger *slog.Logger) *Service {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = DefaultBatchSize
	}
	if cfg.PlaybackSyncEventWindow <= 0 {
		cfg.PlaybackSyncEventWindow = DefaultPlaybackSyncEventWindow
	}
	if cfg.AnswerOperationWindow <= 0 {
		cfg.AnswerOperationWindow = DefaultAnswerOperationWindow
	}
	if cfg.SweepInterval <= 0 {
		cfg.SweepInterval = DefaultSweepInterval
	}
	return &Service{pool: pool, cfg: cfg, logger: logger}
}

// SweepInterval is how often the worker runs the sweep.
func (s *Service) SweepInterval() time.Duration { return s.cfg.SweepInterval }

// PurgeExpiredRows deletes rows older than their window in batches until a batch comes back
// short. Batches lock rows with SKIP LOCKED, so concurrent sweeps never delete a row twice,
// wait on each other or fail.
func (s *Service) PurgeExpiredRows(ctx context.Context) (Result, error) {
	return s.purgeAt(ctx, time.Now().UTC())
}

func (s *Service) purgeAt(ctx context.Context, now time.Time) (Result, error) {
	var res Result
	var err error
	if res.PlaybackSyncEvents, err = s.purgeTable(ctx, "lesson_playback_sync_events", now.Add(-s.cfg.PlaybackSyncEventWindow)); err != nil {
		return res, err
	}
	if res.AnswerOperations, err = s.purgeTable(ctx, "lesson_test_answer_operations", now.Add(-s.cfg.AnswerOperationWindow)); err != nil {
		return res, err
	}
	// Guided chat drafts expire 30 days after their last save; each row carries its own expiry.
	if res.GuidedChatDrafts, err = s.purgeBy(ctx, "guided_chat_drafts", "expires_at", now); err != nil {
		return res, err
	}
	if err = s.clearAbandonedTeachBacks(ctx, now); err != nil {
		return res, err
	}
	if s.logger != nil && (res.PlaybackSyncEvents > 0 || res.AnswerOperations > 0 || res.GuidedChatDrafts > 0) {
		s.logger.InfoContext(ctx, "row retention purge", "playback_sync_events", res.PlaybackSyncEvents, "lesson_test_answer_operations", res.AnswerOperations, "guided_chat_drafts", res.GuidedChatDrafts)
	}
	return res, nil
}

// Keep each Lesson's turn so retention cannot restart a paid attempt.
func (s *Service) clearAbandonedTeachBacks(ctx context.Context, now time.Time) error {
	cutoff := now.Add(-teachBackExplanationWindow)
	for {
		tag, err := s.pool.Exec(ctx, `
UPDATE lesson_teach_backs SET messages='[]'::jsonb WHERE ctid = ANY(ARRAY(
	SELECT ctid FROM lesson_teach_backs
	WHERE messages <> '[]'::jsonb AND updated_at < $1
		AND (processing_until IS NULL OR processing_until <= $2)
	ORDER BY updated_at LIMIT $3 FOR UPDATE SKIP LOCKED))`, cutoff, now, s.cfg.BatchSize)
		if err != nil {
			return fmt.Errorf("clear lesson teach-back explanations: %w", err)
		}
		if tag.RowsAffected() < int64(s.cfg.BatchSize) {
			return nil
		}
	}
}

// purgeTable takes a fixed table name from this file, never from input.
func (s *Service) purgeTable(ctx context.Context, table string, cutoff time.Time) (int64, error) {
	return s.purgeBy(ctx, table, "created_at", cutoff)
}

// purgeBy deletes rows whose column (a fixed name from this file) is before the cutoff.
func (s *Service) purgeBy(ctx context.Context, table, column string, cutoff time.Time) (int64, error) {
	query := fmt.Sprintf(`DELETE FROM %[1]s WHERE ctid = ANY(ARRAY(
  SELECT ctid FROM %[1]s WHERE %[2]s < $1 ORDER BY %[2]s LIMIT $2 FOR UPDATE SKIP LOCKED))`, table, column)
	var total int64
	for {
		tag, err := s.pool.Exec(ctx, query, cutoff, s.cfg.BatchSize)
		if err != nil {
			return total, fmt.Errorf("purge %s: %w", table, err)
		}
		total += tag.RowsAffected()
		if tag.RowsAffected() < int64(s.cfg.BatchSize) {
			return total, nil
		}
	}
}
