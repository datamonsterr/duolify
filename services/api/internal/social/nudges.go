package social

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Nudge codes. unavailable is the same answer for a stranger, a block, a removed or
// erased friend, a friend who already finished and an unknown id.
const (
	CodeNudgeUnavailable = "nudge_unavailable"
	CodeNudgeLimited     = "nudge_limited"

	statusSent  = "sent"
	bucketNudge = "friend_nudge"
	inboxMax    = 50
)

// nudgeDay is the sender's local date and the instant their next local day starts.
// An unknown zone falls back to UTC.
func nudgeDay(now time.Time, zone string) (string, time.Time) {
	loc, err := time.LoadLocation(zone)
	if err != nil || zone == "" {
		loc = time.UTC
	}
	local := now.In(loc)
	y, m, d := local.Date()
	return local.Format(time.DateOnly), time.Date(y, m, d+1, 0, 0, 0, 0, loc)
}

// nudgeAllowed is the eligibility rule: a friend, no block, not yet finished today.
func nudgeAllowed(friends, blocked, recipientDone bool) bool {
	return friends && !blocked && !recipientDone
}

// nudgeSentProps are the analytics properties: the nudge UUID and the sender's day.
func nudgeSentProps(id, localDate string) map[string]any {
	return map[string]any{"event_id": id, "local_date": localDate}
}

// parseNudgeBody reads the optional {"operation_key": uuid}; an empty body has no key.
func parseNudgeBody(r io.Reader) (string, error) {
	var in struct {
		Key *string `json:"operation_key"`
	}
	d := json.NewDecoder(io.LimitReader(r, 2048))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); errors.Is(err, io.EOF) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	var extra any
	if !errors.Is(d.Decode(&extra), io.EOF) {
		return "", errors.New("unexpected trailing data")
	}
	if in.Key == nil {
		return "", nil
	}
	key, err := uuid.Parse(*in.Key)
	if err != nil {
		return "", err
	}
	return key.String(), nil
}

// NudgeResult is the idempotent outcome of a nudge.
type NudgeResult struct {
	Status string `json:"status"`
	ID     string `json:"id"`
}

// NudgeState is the sender's view of one friend: whether today's nudge is spent.
type NudgeState struct {
	Sent   bool       `json:"sent"`
	NextAt *time.Time `json:"next_at,omitempty"`
}

func (s *Service) zoneOf(ctx context.Context, q Querier, learnerID string) string {
	var zone string
	if err := q.QueryRow(ctx, `SELECT COALESCE(NULLIF(time_zone,''),'UTC') FROM learner_profiles WHERE learner_id=$1::uuid`, learnerID).Scan(&zone); err != nil {
		return "UTC"
	}
	return zone
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// pruneNudges deletes nudges older than the configured retention: bounded, not hidden.
func pruneNudges(ctx context.Context, db execer, days int) error {
	_, err := db.Exec(ctx, `DELETE FROM friend_nudges WHERE created_at < now() - make_interval(days => $1)`, days)
	return err
}

// Nudge sends the actor's one nudge of the day to a friend, inside one transaction
// that holds both learner locks (so it cannot race a block, removal or deletion). The
// operation key replays the first result. Every refusal other than the daily limit is
// the same neutral answer. A recipient who hides their streak from friends gets
// nothing delivered, yet the sender sees a normal "sent" and the day is spent.
func (s *Service) Nudge(ctx context.Context, actor, friendshipID, key string) (NudgeResult, error) {
	unavailable := &Rejection{Code: CodeNudgeUnavailable}
	if key == "" {
		key = uuid.NewString()
	}
	cfg, err := s.config(ctx)
	if err != nil {
		return NudgeResult{}, err
	}
	var lo, hi string
	err = s.pool.QueryRow(ctx, `SELECT learner_low::text,learner_high::text FROM friendships WHERE id=$1::uuid AND (learner_low=$2::uuid OR learner_high=$2::uuid)`, friendshipID, actor).Scan(&lo, &hi)
	if errors.Is(err, pgx.ErrNoRows) {
		return NudgeResult{}, unavailable
	} else if err != nil {
		return NudgeResult{}, err
	}
	recipient := lo
	if recipient == actor {
		recipient = hi
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return NudgeResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = lockLearners(ctx, tx, actor, recipient); err != nil {
		return NudgeResult{}, err
	}
	var friends, show bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM friendships WHERE id=$1::uuid), COALESCE((SELECT show_streak_to_friends FROM learner_social_settings WHERE learner_id=$2::uuid),true)`, friendshipID, recipient).Scan(&friends, &show); err != nil {
		return NudgeResult{}, err
	}
	blocked, err := BlockedEitherWay(ctx, tx, actor, recipient)
	if err != nil {
		return NudgeResult{}, err
	}
	if !nudgeAllowed(friends, blocked, false) {
		return NudgeResult{}, unavailable
	}
	var existing string
	err = tx.QueryRow(ctx, `SELECT id::text FROM friend_nudges WHERE sender_id=$1::uuid AND operation_key=$2::uuid`, actor, key).Scan(&existing)
	if err == nil {
		return NudgeResult{Status: statusSent, ID: existing}, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return NudgeResult{}, err
	}
	if err = pruneNudges(ctx, tx, cfg.NudgeRetentionDays); err != nil {
		return NudgeResult{}, err
	}
	now := s.clock()
	date, next := nudgeDay(now, s.zoneOf(ctx, tx, actor))
	var spent bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM friend_nudges WHERE sender_id=$1::uuid AND recipient_id=$2::uuid AND sender_local_date=$3::date)`, actor, recipient, date).Scan(&spent); err != nil {
		return NudgeResult{}, err
	}
	if spent {
		return NudgeResult{}, &Rejection{Code: CodeNudgeLimited, RetryAfter: next.Sub(now)}
	}
	if rej, err := allowOn(ctx, tx, actor, bucketNudge, cfg.NudgeSendsPerDay, sendWindow); err != nil {
		return NudgeResult{}, err
	} else if rej != nil {
		return NudgeResult{}, rej
	}
	if show { // a hidden streak is never disclosed through the done check
		recipientDate, _ := nudgeDay(now, s.zoneOf(ctx, tx, recipient))
		var done bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM streak_days WHERE learner_id=$1::uuid AND local_date=$2::date)`, recipient, recipientDate).Scan(&done); err != nil {
			return NudgeResult{}, err
		}
		if !nudgeAllowed(friends, blocked, done) {
			return NudgeResult{}, unavailable
		}
	}
	var id string
	if err = tx.QueryRow(ctx, `INSERT INTO friend_nudges(sender_id,recipient_id,operation_key,sender_local_date,delivered) VALUES($1::uuid,$2::uuid,$3::uuid,$4::date,$5) RETURNING id::text`,
		actor, recipient, key, date, show).Scan(&id); err != nil {
		return NudgeResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return NudgeResult{}, err
	}
	s.emit(actor, "friend_nudge_sent", nudgeSentProps(id, date))
	return NudgeResult{Status: statusSent, ID: id}, nil
}

// NudgeEntry is one inbox item: the sender's public identity and when, nothing else.
type NudgeEntry struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Avatar      Avatar    `json:"avatar"`
	CreatedAt   time.Time `json:"created_at"`
	Read        bool      `json:"read"`
}

// Nudges lists the actor's delivered nudges, newest first. The inbox only shows nudges
// from current, unblocked friends while the actor still allows friends to see their
// streak, so a block, a removal or the setting hides pending nudges at once.
func (s *Service) Nudges(ctx context.Context, actor string) ([]NudgeEntry, int, error) {
	cfg, err := s.config(ctx)
	if err != nil {
		return nil, 0, err
	}
	if err = pruneNudges(ctx, s.pool, cfg.NudgeRetentionDays); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT n.id::text,n.created_at,n.read_at IS NOT NULL,u.username,l.display_name
 FROM friend_nudges n
 JOIN friendships f ON f.learner_low=LEAST(n.sender_id,n.recipient_id) AND f.learner_high=GREATEST(n.sender_id,n.recipient_id)
 JOIN learner_usernames u ON u.learner_id=n.sender_id JOIN learners l ON l.id=n.sender_id
 LEFT JOIN learner_social_settings ss ON ss.learner_id=n.recipient_id
 WHERE n.recipient_id=$1::uuid AND n.delivered AND COALESCE(ss.show_streak_to_friends,true)`+NotBlockedSQL("n.recipient_id", "n.sender_id")+`
 ORDER BY n.created_at DESC, n.id LIMIT `+strconv.Itoa(inboxMax), actor)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out, unread := []NudgeEntry{}, 0
	for rows.Next() {
		var e NudgeEntry
		if err = rows.Scan(&e.ID, &e.CreatedAt, &e.Read, &e.Username, &e.DisplayName); err != nil {
			return nil, 0, err
		}
		e.Avatar = Avatar{Initial: strings.ToUpper(e.Username[:1])}
		if !e.Read {
			unread++
		}
		out = append(out, e)
	}
	return out, unread, rows.Err()
}

// MarkNudgesRead marks the actor's delivered nudges read.
func (s *Service) MarkNudgesRead(ctx context.Context, actor string) error {
	_, err := s.pool.Exec(ctx, `UPDATE friend_nudges SET read_at=now() WHERE recipient_id=$1::uuid AND delivered AND read_at IS NULL`, actor)
	return err
}

// nudgeStates tells the actor which friends already got today's nudge.
func (s *Service) nudgeStates(ctx context.Context, actor string) (map[string]*NudgeState, error) {
	date, next := nudgeDay(s.clock(), s.zoneOf(ctx, s.pool, actor))
	rows, err := s.pool.Query(ctx, `SELECT recipient_id::text FROM friend_nudges WHERE sender_id=$1::uuid AND sender_local_date=$2::date`, actor, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*NudgeState{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = &NudgeState{Sent: true, NextAt: &next}
	}
	return out, rows.Err()
}
