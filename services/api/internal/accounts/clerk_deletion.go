package accounts

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/user"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ClerkUserDeleter deletes one Clerk user. A user Clerk no longer has counts as deleted, so
// repeating a request is safe.
type ClerkUserDeleter interface {
	DeleteUser(ctx context.Context, clerkUserID string) error
}

// ClerkUserAPI is the part of the Clerk Backend API user deletion needs.
type ClerkUserAPI interface {
	Delete(ctx context.Context, id string) (*clerk.DeletedResource, error)
}

type clerkUserDeleter struct{ users ClerkUserAPI }

// NewClerkUserDeleter wraps a Clerk Backend users client (or a fake in tests).
func NewClerkUserDeleter(users ClerkUserAPI) ClerkUserDeleter {
	return clerkUserDeleter{users: users}
}

// ClerkUserDeleterFromEnv returns nil when Clerk is not configured, so no Clerk call is ever
// made without keys. A secret that is not a Clerk secret key is an error.
func ClerkUserDeleterFromEnv(getenv func(string) string) (ClerkUserDeleter, error) {
	secret := getenv("CLERK_SECRET_KEY")
	if secret == "" {
		return nil, nil
	}
	if !strings.HasPrefix(secret, "sk_") {
		return nil, errors.New("CLERK_SECRET_KEY must be a Clerk secret key (sk_...)")
	}
	return NewClerkUserDeleter(user.NewClient(&clerk.ClientConfig{BackendConfig: clerk.BackendConfig{Key: clerk.String(secret)}})), nil
}

// DeleteUser calls the Clerk Backend API (DELETE /v1/users/{id}). Clerk answers 404
// (resource_not_found) for a user that is already gone; that is the goal state, not an error.
func (d clerkUserDeleter) DeleteUser(ctx context.Context, id string) error {
	if _, err := d.users.Delete(ctx, id); err != nil && !clerkNotFound(err) {
		return fmt.Errorf("clerk delete user: %w", err)
	}
	return nil
}

func clerkNotFound(err error) bool {
	var apiErr *clerk.APIErrorResponse
	return errors.As(err, &apiErr) && apiErr.HTTPStatusCode == http.StatusNotFound
}

// ClerkUserEraser drains clerk_user_deletion_requests. It is deliberately independent of the
// HTTP Service: the River worker builds it from a pool and the optional deleter alone.
type ClerkUserEraser struct {
	pool    *pgxpool.Pool
	deleter ClerkUserDeleter
	timeout time.Duration
}

// NewClerkUserEraser returns an eraser; a nil deleter disables it entirely.
func NewClerkUserEraser(pool *pgxpool.Pool, deleter ClerkUserDeleter) *ClerkUserEraser {
	return &ClerkUserEraser{pool: pool, deleter: deleter, timeout: 10 * time.Second}
}

// Enabled reports whether Clerk is configured. Without it nothing is scheduled or called, and
// queued requests wait until Clerk is configured.
func (e *ClerkUserEraser) Enabled() bool { return e != nil && e.deleter != nil }

const clerkDeletionBatch = 4 // At most 40 seconds of remote calls, within River's one-minute deadline.

// ProcessClerkUserDeletions deletes queued Clerk users. A request leaves the queue only after
// Clerk accepted it or reported the user already gone; any other failure keeps it (with an
// attempt count) for the next run. It returns how many users were deleted and a joined error
// naming the failures, without ever including a Clerk user id.
func (e *ClerkUserEraser) ProcessClerkUserDeletions(ctx context.Context) (int, error) {
	if !e.Enabled() {
		return 0, nil
	}
	rows, err := e.pool.Query(ctx, `SELECT clerk_user_id FROM clerk_user_deletion_requests
 WHERE last_attempt_at IS NULL OR last_attempt_at <= now() - LEAST(interval '1 hour', interval '10 seconds' * power(2,LEAST(attempts,9)))
 ORDER BY last_attempt_at NULLS FIRST,requested_at,clerk_user_id LIMIT $1`, clerkDeletionBatch)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	done, failed := 0, 0
	var firstErr error
	for _, id := range ids {
		tx, txErr := e.pool.Begin(ctx)
		if txErr != nil {
			return done, txErr
		}
		var locked bool
		txErr = tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended('clerk-subject:' || $1,0))", id).Scan(&locked)
		if txErr != nil || !locked {
			_ = tx.Rollback(ctx)
			if txErr != nil {
				return done, txErr
			}
			continue
		}
		var pending bool
		txErr = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM clerk_user_deletion_requests WHERE clerk_user_id=$1 AND
 (last_attempt_at IS NULL OR last_attempt_at <= now() - LEAST(interval '1 hour', interval '10 seconds' * power(2,LEAST(attempts,9)))))`, id).Scan(&pending)
		if txErr != nil || !pending {
			_ = tx.Rollback(ctx)
			if txErr != nil {
				return done, txErr
			}
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, e.timeout)
		err = e.deleter.DeleteUser(callCtx, id)
		cancel()
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			if _, dbErr := tx.Exec(ctx, "UPDATE clerk_user_deletion_requests SET attempts=attempts+1,last_attempt_at=now() WHERE clerk_user_id=$1", id); dbErr != nil {
				_ = tx.Rollback(ctx)
				return done, errors.Join(firstErr, dbErr)
			}
			if err = tx.Commit(ctx); err != nil {
				return done, err
			}
			continue
		}
		if _, err = tx.Exec(ctx, "DELETE FROM clerk_user_deletion_requests WHERE clerk_user_id=$1", id); err != nil {
			_ = tx.Rollback(ctx)
			return done, err
		}
		if err = tx.Commit(ctx); err != nil {
			return done, err
		}
		done++
	}
	if failed > 0 {
		return done, fmt.Errorf("%d of %d clerk user deletions failed and stay queued: %w", failed, len(ids), firstErr)
	}
	return done, nil
}

// queueClerkUserDeletion records every Clerk user bound to the Learner. It runs inside the
// account-erasure transaction, before the learner row (and with it the binding) is deleted.
func queueClerkUserDeletion(ctx context.Context, tx pgx.Tx, learnerID string, ledgers ...DeletionLedger) (bool, error) {
	rows, err := tx.Query(ctx, "SELECT subject FROM auth_provider_identities WHERE learner_id=$1::uuid AND provider='clerk'", learnerID)
	if err != nil {
		return false, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return false, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('clerk-subject:' || $1,0))", id); err != nil {
			return false, err
		}
		if len(ledgers) > 0 {
			ledger, ok := ledgers[0].(ClerkDeletionLedger)
			if !ok {
				return false, errors.New("recoverable Clerk erasure ledger required")
			}
			if err = ledger.AppendClerk(ctx, id); err != nil {
				return false, err
			}
		}
		if _, err = tx.Exec(ctx, "INSERT INTO clerk_user_deletion_requests(clerk_user_id) VALUES($1) ON CONFLICT DO NOTHING", id); err != nil {
			return false, err
		}
	}
	return len(ids) > 0, nil
}
