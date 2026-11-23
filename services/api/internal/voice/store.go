package voice

import (
	"context"
	"errors"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// PoolLessons checks the Lesson against the Learner's own rows: a Lesson that is not theirs, or does
// not exist, is the same answer.
type PoolLessons struct{ Pool *pgxpool.Pool }

// Entitled implements Lessons.
func (p PoolLessons) Entitled(ctx context.Context, learner, lesson string) (bool, error) {
	var one int
	err := p.Pool.QueryRow(ctx, `SELECT 1 FROM course_lesson_stubs WHERE id=$1::uuid AND learner_id=$2::uuid`, lesson, learner).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// PoolDays resolves the Learner's local day with the same timezone history as the check-in.
type PoolDays struct{ Pool *pgxpool.Pool }

// LocalDay implements Days.
func (p PoolDays) LocalDay(ctx context.Context, learner string, at time.Time) (string, time.Time, error) {
	return credits.LocalDay(ctx, p.Pool, learner, at)
}

// RedisCounters keeps the limit counters in Redis. Every key gets an expiry in the same transaction
// that creates it, so no counter outlives its window.
type RedisCounters struct{ Client *redis.Client }

// Incr implements Counters.
func (c RedisCounters) Incr(ctx context.Context, key string, ttl time.Duration) (int64, time.Duration, error) {
	pipe := c.Client.TxPipeline()
	count := pipe.Incr(ctx, key)
	pipe.ExpireNX(ctx, key, ttl)
	left := pipe.PTTL(ctx, key)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, 0, err
	}
	return count.Val(), left.Val(), nil
}

// Decr implements Counters.
func (c RedisCounters) Decr(ctx context.Context, key string) error {
	// A key that already expired would come back as -1 with no expiry: give it one.
	pipe := c.Client.TxPipeline()
	pipe.Decr(ctx, key)
	pipe.ExpireNX(ctx, key, 25*time.Hour)
	_, err := pipe.Exec(ctx)
	return err
}
