package social

import (
	"context"
	"io"

	"github.com/jackc/pgx/v5"
)

const (
	CodeBlockUnavailable = "block_unavailable"
	CodeBlockLimit       = "block_limit"
)

// Skeleton so the red tests compile and fail on assertions; the real code lands after them.

// Querier is the read side shared by *pgxpool.Pool, pgx.Tx and pgx.Conn.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func BlockedEitherWay(ctx context.Context, q Querier, a, b string) (bool, error) { return false, nil }
func CanInteract(ctx context.Context, q Querier, a, b string) (bool, error)      { return true, nil }
func CanInvite(ctx context.Context, q Querier, inviter, invitee string) (bool, error) {
	return true, nil
}
func NotBlockedSQL(viewer, other string) string { return "" }

// OnBlockFunc runs inside the block transaction.
type OnBlockFunc func(ctx context.Context, tx pgx.Tx, blocker, blocked string) error

func (s *Service) OnBlock(fn OnBlockFunc) {}
func (s *Service) runOnBlock(ctx context.Context, tx pgx.Tx, blocker, blocked string) error {
	return nil
}
func ParseBlockBody(r io.Reader) (string, error)     { return "", nil }
func inviteAllows(setting string, friends bool) bool { return true }
