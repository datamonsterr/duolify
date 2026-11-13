package social

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Block codes. Both are stable API codes; the first is deliberately the same for an
// unknown username, self, and a learner the actor cannot see.
const (
	CodeBlockUnavailable = "block_unavailable"
	CodeBlockLimit       = "block_limit"
)

// ---------------------------------------------------------------------------
// Shared authorization contract (E15 course invites and sharing).
//
// Every code path that lets one learner reach another — a Course invite, a share,
// a notification — MUST call CanInvite (or CanInteract for non-invite contact) in
// the same transaction that writes the reference, passing that transaction as q.
// Because block, send and accept all lock both learner rows (lockLearners) before
// they read or write, a check made inside a transaction that first locked the pair
// cannot race a concurrent block.
//
// E15 also registers an OnBlock callback that cancels invites between the pair in
// both directions.
// ---------------------------------------------------------------------------

// Querier is the read side shared by *pgxpool.Pool, pgx.Tx and pgx.Conn.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// BlockedEitherWay reports whether a blocked b or b blocked a. It fails closed:
// callers must treat an error as "blocked".
func BlockedEitherWay(ctx context.Context, q Querier, a, b string) (bool, error) {
	var blocked bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM learner_blocks
 WHERE (blocker_id=$1::uuid AND blocked_id=$2::uuid) OR (blocker_id=$2::uuid AND blocked_id=$1::uuid))`, a, b).Scan(&blocked)
	return blocked, err
}

// CanInteract is false for a blocked pair in either direction. A learner can always
// "interact" with themselves; callers that forbid self-targeting check that first.
func CanInteract(ctx context.Context, q Querier, a, b string) (bool, error) {
	blocked, err := BlockedEitherWay(ctx, q, a, b)
	return !blocked && err == nil, err
}

// CanInvite is the Course-invite rule: no block either way, never to oneself, and
// the invitee's course_invites_from setting (default friends) must admit the inviter.
func CanInvite(ctx context.Context, q Querier, inviter, invitee string) (bool, error) {
	if inviter == invitee {
		return false, nil
	}
	if ok, err := CanInteract(ctx, q, inviter, invitee); err != nil || !ok {
		return false, err
	}
	var setting string
	var friends bool
	err := q.QueryRow(ctx, `SELECT COALESCE((SELECT course_invites_from FROM learner_social_settings WHERE learner_id=$2::uuid),'friends'),
 EXISTS (SELECT 1 FROM friendships WHERE (learner_low=$1::uuid AND learner_high=$2::uuid) OR (learner_low=$2::uuid AND learner_high=$1::uuid))`,
		inviter, invitee).Scan(&setting, &friends)
	if err != nil {
		return false, err
	}
	return inviteAllows(setting, friends), nil
}

// inviteAllows applies course_invites_from; unknown values fail closed.
func inviteAllows(setting string, friends bool) bool {
	switch setting {
	case "everyone":
		return true
	case "friends":
		return friends
	}
	return false
}

var sqlOperand = regexp.MustCompile(`^(\$[0-9]+|[a-z_][a-z0-9_]*(\.[a-z_][a-z0-9_]*)?)(::uuid)?$`)

// NotBlockedSQL returns an " AND NOT EXISTS (...)" fragment that removes rows whose
// learner is blocked either way with the viewer, for list queries that cannot call
// CanInteract per row. viewer and other are SQL operands (a "$n" parameter or a
// column such as "u.learner_id", optionally "::uuid"); anything else panics, because
// they are code constants and never user input.
func NotBlockedSQL(viewer, other string) string {
	if !sqlOperand.MatchString(viewer) || !sqlOperand.MatchString(other) {
		panic(fmt.Sprintf("social: unsafe NotBlockedSQL operand %q/%q", viewer, other))
	}
	return fmt.Sprintf(` AND NOT EXISTS (SELECT 1 FROM learner_blocks b WHERE (b.blocker_id=%[1]s AND b.blocked_id=%[2]s) OR (b.blocker_id=%[2]s AND b.blocked_id=%[1]s))`, viewer, other)
}

// OnBlockFunc runs inside the block transaction, after the friendship and the pair's
// requests are removed and the block row exists. It receives the open transaction and
// MUST do all its writes through it. It must cancel references in BOTH directions
// between blocker and blocked. Returning an error rolls the whole block back.
type OnBlockFunc func(ctx context.Context, tx pgx.Tx, blocker, blocked string) error

// OnBlock registers a callback. Register at startup, before serving requests.
// Callbacks run in registration order and only for a new block, not a repeat.
func (s *Service) OnBlock(fn OnBlockFunc) {
	if fn != nil {
		s.onBlock = append(s.onBlock, fn)
	}
}

func (s *Service) runOnBlock(ctx context.Context, tx pgx.Tx, blocker, blocked string) error {
	for _, fn := range s.onBlock {
		if err := fn(ctx, tx, blocker, blocked); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------

// ParseBlockBody reads {"username"}: exactly that, bounded, non-empty.
func ParseBlockBody(r io.Reader) (string, error) {
	var in struct {
		Username string `json:"username"`
	}
	d := json.NewDecoder(io.LimitReader(r, 2048))
	d.DisallowUnknownFields()
	var extra any
	if err := d.Decode(&in); err != nil {
		return "", err
	}
	if !errors.Is(d.Decode(&extra), io.EOF) {
		return "", errors.New("unexpected trailing data")
	}
	name := strings.TrimPrefix(strings.TrimSpace(in.Username), "@")
	if name == "" {
		return "", errors.New("block needs a username")
	}
	return name, nil
}

// BlockedLearner is one entry of the actor's own blocked list: public identity and date only.
type BlockedLearner struct {
	Identity
	BlockedAt time.Time `json:"blocked_at"`
}

func (s *Service) lookupByName(ctx context.Context, username string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT learner_id::text FROM learner_usernames WHERE canonical=$1`, Canonical(username)).Scan(&id)
	return id, err
}

// Block blocks the learner with that username. It is one transaction: lock both
// learners, record the block, end the friendship (and, from E6-06, the friend streak)
// and the pair's requests, then run the registered callbacks. Repeats succeed and
// change nothing. A target the actor cannot see is refused like an unknown username.
func (s *Service) Block(ctx context.Context, actor, username string) error {
	cfg, err := s.config(ctx)
	if err != nil {
		return err
	}
	target, err := s.lookupByName(ctx, username)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && target == actor) {
		return &Rejection{Code: CodeBlockUnavailable}
	}
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if found, err := lockLearners(ctx, tx, actor, target); err != nil {
		return err
	} else if found != 2 {
		return &Rejection{Code: CodeBlockUnavailable}
	}
	var already, reachable bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM learner_blocks WHERE blocker_id=$1::uuid AND blocked_id=$2::uuid),
 EXISTS (SELECT 1 FROM learner_usernames u LEFT JOIN learner_social_settings ss ON ss.learner_id=u.learner_id WHERE u.learner_id=$2::uuid AND `+exactVisible+`)
 OR EXISTS (SELECT 1 FROM friendships WHERE (learner_low=$1::uuid AND learner_high=$2::uuid) OR (learner_low=$2::uuid AND learner_high=$1::uuid))
 OR EXISTS (SELECT 1 FROM friend_requests WHERE (sender_id=$1::uuid AND recipient_id=$2::uuid) OR (sender_id=$2::uuid AND recipient_id=$1::uuid))`, actor, target).Scan(&already, &reachable); err != nil {
		return err
	}
	if already {
		return nil
	}
	if !reachable {
		return &Rejection{Code: CodeBlockUnavailable}
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM learner_blocks WHERE blocker_id=$1::uuid`, actor).Scan(&count); err != nil {
		return err
	}
	if count >= cfg.BlockMax {
		return &Rejection{Code: CodeBlockLimit}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO learner_blocks(blocker_id,blocked_id) VALUES($1::uuid,$2::uuid)`, actor, target); err != nil {
		return err
	}
	wasFriend, ended, err := s.endFriendshipTx(ctx, tx, actor, target)
	if err != nil {
		return err
	}
	if err = s.runOnBlock(ctx, tx, actor, target); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	for _, e := range ended {
		if e.learner == actor { // the blocked learner is told nothing beyond the friendship ending
			s.emitAll([]streakEvent{e})
		}
	}
	s.emit(actor, "learner_blocked", map[string]any{"was_friend": wasFriend})
	return nil
}

// Unblock removes only the actor's own block. Unknown names and repeats are no-ops;
// nothing that the block removed comes back.
func (s *Service) Unblock(ctx context.Context, actor, username string) error {
	target, err := s.lookupByName(ctx, username)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM learner_blocks WHERE blocker_id=$1::uuid AND blocked_id=$2::uuid`, actor, target)
	return err
}

// Blocks lists the actor's own blocks, newest first.
func (s *Service) Blocks(ctx context.Context, actor string) ([]BlockedLearner, error) {
	rows, err := s.pool.Query(ctx, `SELECT u.username,l.display_name,b.created_at
 FROM learner_blocks b JOIN learner_usernames u ON u.learner_id=b.blocked_id JOIN learners l ON l.id=b.blocked_id
 WHERE b.blocker_id=$1::uuid ORDER BY b.created_at DESC, u.canonical LIMIT 1000`, actor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BlockedLearner{}
	for rows.Next() {
		var r identityRow
		var item BlockedLearner
		if err = rows.Scan(&r.Username, &r.DisplayName, &item.BlockedAt); err != nil {
			return nil, err
		}
		item.Identity = r.Project(nil, false)
		out = append(out, item)
	}
	return out, rows.Err()
}
