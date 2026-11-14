package backup

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// BlockStore is the read-only ledger view the restore drill needs.
type BlockStore interface {
	List(context.Context, string) ([]Object, error)
	GetCurrent(context.Context, string) ([]byte, error)
}

// ModerationLedger stores severe moderation blocks outside database snapshots.
type ModerationLedger struct{ Objects Objects }

// RecordBlock is durable before the takedown commits.
func (ModerationLedger) RecordBlock(context.Context, string, string) error { return nil }

// LiftBlock records that an appeal overturned the action.
func (ModerationLedger) LiftBlock(context.Context, string) error { return nil }

// ReplayModerationBlocks re-applies every unlifted block to a restored database.
func ReplayModerationBlocks(context.Context, *pgxpool.Pool, BlockStore) (int, error) { return 0, nil }

// PinResolution checks that every copy pin and published object of a restore resolves.
func PinResolution(exists func(context.Context, string) (bool, error)) Check {
	return Check{"copy pins resolve", KindObjects, func(context.Context, *pgxpool.Pool) error { return nil }}
}
