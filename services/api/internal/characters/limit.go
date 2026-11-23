package characters

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// LimitError is the "limit_exceeded" rejection. RetryAfter is set for windowed
// limits (edits per hour, creates per minute and day) and zero for the count cap.
type LimitError struct{ RetryAfter time.Duration }

func (e *LimitError) Error() string { return "limit_exceeded" }

// limitError finds when the window next frees a slot: the query returns the
// created_at of the attempt whose expiry brings the count back under the limit
// (offset = count - limit, oldest first).
func limitError(ctx context.Context, q pgx.Tx, query, owner string, offset int64, window time.Duration) *LimitError {
	var oldest time.Time
	if err := q.QueryRow(ctx, query, owner, offset).Scan(&oldest); err != nil {
		return &LimitError{}
	}
	return &LimitError{RetryAfter: time.Until(oldest.Add(window))}
}

// RetryAfterSeconds rounds up to whole seconds, with a minimum of one.
func (e *LimitError) RetryAfterSeconds() int {
	if e.RetryAfter <= 0 {
		return 0
	}
	n := int((e.RetryAfter + time.Second - 1) / time.Second)
	if n < 1 {
		n = 1
	}
	return n
}

// setRetryAfter adds Retry-After for windowed limits; the count cap has none.
func setRetryAfter(w http.ResponseWriter, err error) {
	var limit *LimitError
	if errors.As(err, &limit) {
		if n := limit.RetryAfterSeconds(); n > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(n))
		}
	}
}
