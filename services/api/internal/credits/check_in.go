package credits

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	dbsql "github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/jackc/pgx/v5"
)

// CheckInResponse describes only the grant committed by this invocation.
type CheckInResponse struct {
	Granted       int64     `json:"granted"`
	FirstTime     bool      `json:"first_time"`
	Balance       int64     `json:"balance"`
	BankedCheckIn int64     `json:"banked_checkin"`
	Cap           int64     `json:"cap"`
	NextCheckInAt time.Time `json:"next_check_in_at"`
	CheckedIn     bool      `json:"checked_in"`
}

func checkInAmount(c pricing.CheckInConfig, bank int64, first bool) int64 {
	grant := min(c.DailyCredits, max(int64(0), c.DailyCredits*c.BankDays-bank))
	if first {
		grant += c.FirstTimeBonus
	}
	return grant
}

func checkInDay(at time.Time, zone string) (string, time.Time, error) {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return "", time.Time{}, err
	}
	local := at.In(loc)
	day := local.Format(time.DateOnly)
	next := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, loc)
	if !next.After(at) || next.In(loc).Format(time.DateOnly) <= day {
		// Some zones advance at midnight. time.Date may resolve a nonexistent
		// midnight into the previous date; locate the actual date transition.
		low, high := at, at.Add(48*time.Hour)
		for high.Sub(low) > time.Nanosecond {
			mid := low.Add(high.Sub(low) / 2)
			if mid.In(loc).Format(time.DateOnly) > day {
				high = mid
			} else {
				low = mid
			}
		}
		next = high
	}
	return day, next, nil
}

// CheckInAt records attendance atomically across API replicas. The clock is
// supplied explicitly for deterministic calendar tests; HTTP always uses DB time.
func (s *Service) CheckInAt(ctx context.Context, owner string, at time.Time) (CheckInResponse, error) {
	var result CheckInResponse
	if s.pool() == nil {
		return result, ErrReservation
	}
	tx, err := s.pool().Begin(ctx)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Existing reserve/settle/refund paths use this same lock. Cap accounting and
	// source movements therefore observe one serialized financial snapshot.
	if err = financialLock(ctx, tx); err != nil {
		return result, err
	}
	var locked string
	if err = tx.QueryRow(ctx, "SELECT id::text FROM learners WHERE id=$1::uuid FOR UPDATE", owner).Scan(&locked); err != nil {
		return result, err
	}
	if at.IsZero() {
		if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&at); err != nil {
			return result, err
		}
	}
	catalogue, err := pricing.Load(ctx, dbsql.New(tx))
	if err != nil {
		return result, err
	}
	cfg := catalogue.Config()
	if cfg.CheckIn == nil {
		return result, errors.New("check-in is not configured")
	}
	result.Cap = cfg.CheckIn.DailyCredits * cfg.CheckIn.BankDays
	zone, err := effectiveTimezone(ctx, tx, owner, at)
	if err != nil {
		return result, err
	}
	day, next, err := checkInDay(at, zone)
	if err != nil {
		return result, err
	}
	result.NextCheckInAt = next
	var previous time.Time
	err = tx.QueryRow(ctx, "SELECT next_check_in_at FROM credit_check_ins WHERE learner_id=$1::uuid ORDER BY checked_at DESC,local_date DESC LIMIT 1", owner).Scan(&previous)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	eligible := err != nil || !at.Before(previous)
	if !eligible {
		result.NextCheckInAt = previous
	}
	// Unspent includes held funds: reserves/releases are holds, not consumption.
	if err = tx.QueryRow(ctx, `SELECT COALESCE(sum(amount),0) FROM credit_entries WHERE learner_id=$1::uuid AND (kind IN ('check_in_grant','check_in_bonus') OR provenance->>'bucket'='check_in') AND kind NOT IN ('reserve','release')`, owner).Scan(&result.BankedCheckIn); err != nil {
		return result, err
	}
	if eligible {
		var already bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM credit_check_ins WHERE learner_id=$1::uuid AND local_date=$2::date)", owner, day).Scan(&already); err != nil {
			return result, err
		}
		if !already {
			var bonusGranted bool
			if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM credit_entries WHERE learner_id=$1::uuid AND kind='check_in_bonus')", owner).Scan(&bonusGranted); err != nil {
				return result, err
			}
			result.FirstTime = !bonusGranted
			regular := checkInAmount(*cfg.CheckIn, result.BankedCheckIn, false)
			result.Granted = checkInAmount(*cfg.CheckIn, result.BankedCheckIn, result.FirstTime)
			prov := map[string]any{"bucket": "check_in", "date": day, "timezone": zone, "source": "Check-in Credits"}
			if regular > 0 {
				if err = ledgerTx(ctx, tx, owner, "check_in:"+day, cfg.Version, "check_in_grant", regular, prov); err != nil {
					return result, err
				}
			}
			// A zero bonus still gets a durable lifetime marker.
			if result.FirstTime {
				if err = ledgerTx(ctx, tx, owner, "check_in_bonus", cfg.Version, "check_in_bonus", cfg.CheckIn.FirstTimeBonus, prov); err != nil {
					return result, err
				}
			}
			if _, err = tx.Exec(ctx, `INSERT INTO credit_check_ins(learner_id,local_date,time_zone,checked_at,next_check_in_at,granted,first_time) VALUES($1::uuid,$2::date,$3,$4,$5,$6,$7)`, owner, day, zone, at, next, result.Granted, result.FirstTime); err != nil {
				return result, err
			}
			result.CheckedIn = true
			result.BankedCheckIn += result.Granted
			err = insertOutbox(ctx, tx, owner, "check_in:"+owner+":"+day, "check_in_credits_granted", map[string]any{"source": "api", "platform": "web", "app_version": "development", "amount": result.Granted, "first_time": result.FirstTime, "banked_after": result.BankedCheckIn, "capped": regular < cfg.CheckIn.DailyCredits})
			if err != nil {
				return result, err
			}
		}
	}
	// Return current spendable balance without loading activity, a cache, another
	// connection or a global expiration sweep. Historical daily buckets stay out.
	err = tx.QueryRow(ctx, `SELECT COALESCE(sum(total),0) FROM (
 SELECT GREATEST(0,sum(amount)) AS total FROM credit_entries WHERE learner_id=$1::uuid AND kind!='reward' AND (kind NOT IN ('daily_grant','expire') AND COALESCE(provenance->>'bucket','')!='daily_free' OR provenance->>'date'=$2) GROUP BY CASE WHEN kind='daily_grant' OR kind='expire' THEN 'daily_free' WHEN kind IN ('check_in_grant','check_in_bonus') THEN 'check_in' WHEN kind='subscription_grant' THEN 'subscription' ELSE COALESCE(provenance->>'bucket','purchase') END) balances`, owner, day).Scan(&result.Balance)
	if err != nil {
		return result, fmt.Errorf("check-in balance: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	if result.Granted > 0 {
		_ = s.InvalidateCache(ctx, owner)
		s.NotifyCreditArrival(ctx, owner)
	}
	return result, nil
}

func (s *Service) handleCheckIn(w http.ResponseWriter, r *http.Request) {
	owner := extractLearnerID(r)
	if owner == "" {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign in required"})
		return
	}
	// Disposable browser suites that assert exact wallet states run with the grant switched off; the
	// switch is honoured only in development so it can never withhold Credits in a deployed environment.
	if os.Getenv("DUOLIFY_ENV") == "development" && os.Getenv("CHECK_IN_DISABLED") == "true" {
		respondJSON(w, http.StatusOK, CheckInResponse{NextCheckInAt: time.Now().Add(24 * time.Hour)})
		return
	}
	// The returned balance is the spendable total, which includes today's Daily free credits.
	// They are granted lazily, so materialise them first, as a wallet read does.
	if s.store != nil {
		_, _ = s.EnsureDailyGrant(r.Context(), owner, time.Now())
	}
	result, err := s.CheckInAt(r.Context(), owner, time.Time{})
	if err != nil {
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "unable to check in; retry"})
		return
	}
	respondJSON(w, http.StatusOK, result)
}
