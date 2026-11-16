package sharing

import (
	"context"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AccountAge reads only the date on the account, never a client-computed age or signup confirmation.
type AccountAge struct{ Pool *pgxpool.Pool }

// MeetsMinAge distinguishes an unknown date from a known underage publisher.
func (a AccountAge) MeetsMinAge(ctx context.Context, learnerID string, minimum int) (bool, error) {
	var id pgtype.UUID
	if err := id.Scan(learnerID); err != nil {
		return false, ErrUnavailable
	}
	date, err := db.New(a.Pool).GetLearnerBirthDate(ctx, id)
	if err != nil {
		return false, err
	}
	if !date.Valid {
		return false, ErrBirthDateRequired
	}
	return date.InfinityModifier == pgtype.Finite && accounts.MeetsMinimumAge(date.Time, time.Now(), max(18, minimum)), nil
}
