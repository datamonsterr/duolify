package accounts

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DeletionLedger is a durable append-only boundary outside restored databases.
type DeletionLedger interface {
	Append(context.Context, string) error
	Contains(context.Context, string) (bool, error)
}

// ConfigureDeletionLedger must be called before serving deletion requests.
func (s *Service) ConfigureDeletionLedger(ledger DeletionLedger) { s.deletionLedger = ledger }

// privacyAccess follows the same session-backed 15-minute freshness policy as
// pricing administration. Access-token refresh does not change this timestamp.
// Each learner gets three exports and three deletion attempts per hour across
// all sessions and replicas. Denials also consume the budget; DB errors deny.
func (s *Service) privacyAccess(action string, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			failure(w, err)
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()
		err = s.limit(ctx, tx, "privacy:"+action+":"+LearnerID(ctx), 3)
		if err != nil {
			if errors.Is(err, errLimited) {
				err = errors.Join(err, tx.Commit(ctx))
				w.Header().Set("Retry-After", "3600")
			}
			failure(w, err)
			return
		}
		var fresh bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM auth_sessions WHERE id=$1::uuid AND learner_id=$2::uuid AND revoked_at IS NULL AND expires_at>now() AND reauthenticated_at>=now()-interval '15 minutes' AND reauthenticated_at<=now())`, SessionID(ctx), contextID(r)).Scan(&fresh)
		if err == nil {
			err = tx.Commit(ctx)
		}
		if err != nil {
			failure(w, err)
			return
		}
		if !fresh {
			respond(w, 403, map[string]string{"error": "Sign in again to export or delete account data.", "code": "reauth_required"})
			return
		}
		next(w, r)
	})
}

// ReplayDeletionLedger merges external deletion intents into the local replay
// cache, then reapplies erasure. Call after every restore and before listening.
// A missing ledger or any storage/database failure prevents startup.
func ReplayDeletionLedger(ctx context.Context, pool *pgxpool.Pool, ledger DeletionLedger) error {
	if ledger == nil {
		return errors.New("external deletion ledger is required")
	}
	// Preserve deletions made before the external ledger was introduced. The SQL
	// table remains a cache, never the only durable copy of a new deletion.
	legacy, err := pool.Query(ctx, "SELECT learner_reference FROM account_deletion_tombstones")
	if err != nil {
		return err
	}
	var refs []string
	for legacy.Next() {
		var ref string
		if err := legacy.Scan(&ref); err != nil {
			legacy.Close()
			return err
		}
		refs = append(refs, ref)
	}
	err = legacy.Err()
	legacy.Close()
	if err != nil {
		return err
	}
	for _, ref := range refs {
		if err := ledger.Append(ctx, ref); err != nil {
			return err
		}
	}
	rows, err := pool.Query(ctx, "SELECT id::text FROM learners")
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		ref := LearnerReference(id)
		deleted, err := ledger.Contains(ctx, ref)
		if err != nil {
			return err
		}
		if deleted {
			if _, err := pool.Exec(ctx, "INSERT INTO account_deletion_tombstones(learner_reference) VALUES($1) ON CONFLICT DO NOTHING", ref); err != nil {
				return err
			}
		}
	}
	return ApplyDeletionTombstones(ctx, pool)
}
