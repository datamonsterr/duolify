package social

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Friend and request codes are stable API codes. Everything that could reveal a
// recipient's state (missing, hidden, refusing, cooling down, self) is one code.
const (
	CodeRequestUnavailable = "request_unavailable"
	CodeInviteUnavailable  = "invite_unavailable"
	CodePendingLimit       = "pending_limit"
	CodeFriendLimit        = "friend_limit"
	CodeNotPending         = "request_not_pending"
)

const (
	statusPending  = "pending"
	statusFriends  = "friends"
	statusDeclined = "declined"
	statusRemoved  = "removed"
	methodUsername = "username"
	methodLink     = "invite_link"
	tokenBytes     = 32
	tokenLength    = 43 // base64url of 32 bytes, unpadded
)

// orderedPair is the canonical (low, high) order stored in friendships.
func orderedPair(a, b string) (string, string) {
	if a < b {
		return a, b
	}
	return b, a
}

// newFriendToken returns an unguessable token and the hash that is stored.
func newFriendToken() (token, hash string, err error) {
	raw := make([]byte, tokenBytes)
	if _, err = rand.Read(raw); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// validToken rejects malformed tokens before any lookup.
func validToken(token string) bool {
	if len(token) != tokenLength {
		return false
	}
	for i := 0; i < len(token); i++ {
		c := token[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

// policyAllows applies the recipient's request_policy. A friend link is the
// recipient's own invitation, so it passes friends_of_friends but never nobody.
func policyAllows(policy string, viaLink, commonFriend bool) bool {
	switch policy {
	case "everyone":
		return true
	case "friends_of_friends":
		return viaLink || commonFriend
	}
	return false
}

// parseSendBody reads {"username"} or {"token"}: exactly one, nothing else.
func parseSendBody(r io.Reader) (username, token string, err error) {
	var in struct {
		Username string `json:"username"`
		Token    string `json:"token"`
	}
	d := json.NewDecoder(io.LimitReader(r, 2048))
	d.DisallowUnknownFields()
	var extra any
	if err = d.Decode(&in); err != nil {
		return "", "", err
	}
	if !errors.Is(d.Decode(&extra), io.EOF) {
		return "", "", errors.New("unexpected trailing data")
	}
	username = strings.TrimPrefix(strings.TrimSpace(in.Username), "@")
	if (username == "") == (in.Token == "") {
		return "", "", errors.New("send needs exactly one of username or token")
	}
	return username, in.Token, nil
}

func sentProps(viaLink bool) map[string]any {
	if viaLink {
		return map[string]any{"method": methodLink}
	}
	return map[string]any{"method": methodUsername}
}

type event struct {
	name  string
	props map[string]any
}

// Result is the idempotent outcome of a friend mutation.
type Result struct {
	Status string `json:"status"`
	ID     string `json:"id,omitempty"`
}

// lockLearners row-locks the learners in id order. It serializes every mutation
// of a pair (and of each learner's friend count), so mutual requests, caps and
// blocking never interleave. It returns how many learners exist.
func lockLearners(ctx context.Context, tx pgx.Tx, ids ...string) (int, error) {
	rows, err := tx.Query(ctx, `SELECT id FROM learners WHERE id = ANY($1::uuid[]) ORDER BY id FOR UPDATE`, ids)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	return n, rows.Err()
}

func friendCount(ctx context.Context, tx pgx.Tx, learnerID string) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM friendships WHERE learner_low=$1::uuid OR learner_high=$1::uuid`, learnerID).Scan(&n)
	return n, err
}

// removeFriendshipTx is the one place a friendship ends: the friendship row and
// every request between the pair go together, inside the caller's transaction.
// The friend streak ends here too (its rows cascade from the friendship); the returned
// events are sent after the caller commits. Block calls it in its own transaction. It
// reports whether a friendship existed.
func (s *Service) endFriendshipTx(ctx context.Context, tx pgx.Tx, a, b string) (bool, []streakEvent, error) {
	if _, err := lockLearners(ctx, tx, a, b); err != nil {
		return false, nil, err
	}
	lo, hi := orderedPair(a, b)
	ended, err := endFriendStreakTx(ctx, tx, lo, hi)
	if err != nil {
		return false, nil, err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM friendships WHERE learner_low=$1::uuid AND learner_high=$2::uuid`, lo, hi)
	if err != nil {
		return false, nil, err
	}
	if err = clearPairRequestsTx(ctx, tx, a, b); err != nil {
		return false, nil, err
	}
	return tag.RowsAffected() > 0, ended, nil
}

// removeFriendshipTx is endFriendshipTx without the streak notifications.
func (s *Service) removeFriendshipTx(ctx context.Context, tx pgx.Tx, a, b string) (bool, error) {
	removed, _, err := s.endFriendshipTx(ctx, tx, a, b)
	return removed, err
}

// clearPairRequestsTx deletes every request between two learners in both
// directions (pending, accepted and declined, which also ends a cooldown).
func clearPairRequestsTx(ctx context.Context, tx pgx.Tx, a, b string) error {
	_, err := tx.Exec(ctx, `DELETE FROM friend_requests WHERE (sender_id=$1::uuid AND recipient_id=$2::uuid) OR (sender_id=$2::uuid AND recipient_id=$1::uuid)`, a, b)
	return err
}

func unavailable(viaLink bool) *Rejection {
	if viaLink {
		return &Rejection{Code: CodeInviteUnavailable}
	}
	return &Rejection{Code: CodeRequestUnavailable}
}

// SendRequest sends a request by username or friend-link token. Re-sends return
// the existing request; a mutual pending request becomes the friendship.
func (s *Service) SendRequest(ctx context.Context, actor, username, token string) (Result, error) {
	cfg, err := s.config(ctx)
	if err != nil {
		return Result{}, err
	}
	viaLink := token != ""
	recipient := ""
	if viaLink {
		if rej, err := s.allow(ctx, actor, bucketLink, cfg.LinkPerHour, time.Hour); rej != nil || err != nil {
			if rej != nil {
				return Result{}, rej
			}
			return Result{}, err
		}
		if !validToken(token) {
			return Result{}, unavailable(true)
		}
		err = s.pool.QueryRow(ctx, `SELECT owner_id::text FROM friend_links WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at>now()`, hashToken(token)).Scan(&recipient)
	} else {
		err = s.pool.QueryRow(ctx, `SELECT learner_id::text FROM learner_usernames WHERE canonical=$1`, Canonical(username)).Scan(&recipient)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		recipient = ""
		if viaLink {
			return Result{}, unavailable(true)
		}
	} else if err != nil {
		return Result{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	res, rej, events, err := s.sendTx(ctx, tx, cfg, actor, recipient, viaLink)
	if err != nil {
		return Result{}, err
	}
	// A refusal still commits: the daily send counter must survive probing.
	if err = tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	for _, e := range events {
		s.emit(actor, e.name, e.props)
	}
	if rej != nil {
		return Result{}, rej
	}
	return res, nil
}

func (s *Service) sendTx(ctx context.Context, tx pgx.Tx, cfg Config, actor, recipient string, viaLink bool) (Result, *Rejection, []event, error) {
	var named bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM learner_usernames WHERE learner_id=$1::uuid)`, actor).Scan(&named); err != nil {
		return Result{}, nil, nil, err
	}
	if !named {
		return Result{}, &Rejection{Code: CodeNotSet}, nil, nil
	}
	ids := []string{actor}
	if recipient != "" && recipient != actor {
		ids = append(ids, recipient)
	}
	found, err := lockLearners(ctx, tx, ids...)
	if err != nil {
		return Result{}, nil, nil, err
	}
	known := len(ids) == 2 && found == 2
	if known {
		// A block in either direction makes the recipient "unknown": caps and the daily
		// counter still run first, so the refusal below is identical to a missing username.
		blocked, err := BlockedEitherWay(ctx, tx, actor, recipient)
		if err != nil {
			return Result{}, nil, nil, err
		}
		known = !blocked
	}
	var ownID, ownStatus string
	var cooling bool
	if known {
		lo, hi := orderedPair(actor, recipient)
		var friends bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM friendships WHERE learner_low=$1::uuid AND learner_high=$2::uuid)`, lo, hi).Scan(&friends); err != nil {
			return Result{}, nil, nil, err
		}
		if friends {
			return Result{Status: statusFriends}, nil, nil, nil
		}
		var mutual string
		switch err = tx.QueryRow(ctx, `SELECT id::text FROM friend_requests WHERE sender_id=$1::uuid AND recipient_id=$2::uuid AND status='pending'`, recipient, actor).Scan(&mutual); {
		case err == nil:
			status, rej, accepted, err := s.acceptLocked(ctx, tx, cfg, mutual, recipient, actor)
			if err != nil || rej != nil {
				return Result{}, rej, nil, err
			}
			return Result{Status: status}, nil, accepted, nil
		case !errors.Is(err, pgx.ErrNoRows):
			return Result{}, nil, nil, err
		}
		err = tx.QueryRow(ctx, `SELECT id::text,status,COALESCE(decided_at+make_interval(days=>$3)>now(),false) FROM friend_requests WHERE sender_id=$1::uuid AND recipient_id=$2::uuid`, actor, recipient, cfg.DeclineCooldownDays).Scan(&ownID, &ownStatus, &cooling)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return Result{}, nil, nil, err
		}
		if err == nil && ownStatus == statusPending {
			return Result{Status: statusPending, ID: ownID}, nil, nil, nil
		}
	}
	// Caps and the daily limit depend only on the sender, so they cannot reveal anything about the target.
	count, err := friendCount(ctx, tx, actor)
	if err != nil {
		return Result{}, nil, nil, err
	}
	if count >= cfg.FriendMax {
		return Result{}, &Rejection{Code: CodeFriendLimit}, nil, nil
	}
	var pending int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM friend_requests WHERE sender_id=$1::uuid AND status='pending'`, actor).Scan(&pending); err != nil {
		return Result{}, nil, nil, err
	}
	if pending >= cfg.FriendPendingMax {
		return Result{}, &Rejection{Code: CodePendingLimit}, nil, nil
	}
	if rej, err := allowOn(ctx, tx, actor, bucketSend, cfg.FriendSendsPerDay, sendWindow); rej != nil || err != nil {
		return Result{}, rej, nil, err
	}
	if !known || (ownID != "" && ownStatus == statusDeclined && cooling) {
		return Result{}, unavailable(viaLink), nil, nil
	}
	policy := "everyone"
	if err = tx.QueryRow(ctx, `SELECT request_policy FROM learner_social_settings WHERE learner_id=$1::uuid`, recipient).Scan(&policy); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Result{}, nil, nil, err
	}
	common := false
	if policy == "friends_of_friends" && !viaLink {
		if err = tx.QueryRow(ctx, commonFriendSQL, actor, recipient).Scan(&common); err != nil {
			return Result{}, nil, nil, err
		}
	}
	if !policyAllows(policy, viaLink, common) {
		return Result{}, unavailable(viaLink), nil, nil
	}
	method := methodUsername
	if viaLink {
		method = methodLink
	}
	var id string
	if err = tx.QueryRow(ctx, `INSERT INTO friend_requests(sender_id,recipient_id,method) VALUES($1::uuid,$2::uuid,$3)
 ON CONFLICT (sender_id,recipient_id) DO UPDATE SET status='pending',method=EXCLUDED.method,created_at=now(),decided_at=NULL
 RETURNING id::text`, actor, recipient, method).Scan(&id); err != nil {
		return Result{}, nil, nil, err
	}
	return Result{Status: statusPending, ID: id}, nil, []event{{"friend_request_sent", sentProps(viaLink)}}, nil
}

const commonFriendSQL = `SELECT EXISTS (
 SELECT 1 FROM (SELECT CASE WHEN learner_low=$1::uuid THEN learner_high ELSE learner_low END AS f FROM friendships WHERE learner_low=$1::uuid OR learner_high=$1::uuid) a
 JOIN (SELECT CASE WHEN learner_low=$2::uuid THEN learner_high ELSE learner_low END AS f FROM friendships WHERE learner_low=$2::uuid OR learner_high=$2::uuid) b USING (f))`

// acceptLocked accepts a request whose pair the caller has locked. Accepting twice is a no-op.
func (s *Service) acceptLocked(ctx context.Context, tx pgx.Tx, cfg Config, requestID, sender, recipient string) (string, *Rejection, []event, error) {
	var status string
	err := tx.QueryRow(ctx, `SELECT status FROM friend_requests WHERE id=$1::uuid AND sender_id=$2::uuid AND recipient_id=$3::uuid FOR UPDATE`, requestID, sender, recipient).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", &Rejection{Code: CodeRequestUnavailable}, nil, nil
	} else if err != nil {
		return "", nil, nil, err
	}
	switch status {
	case "accepted":
		return statusFriends, nil, nil, nil
	case statusDeclined:
		return "", &Rejection{Code: CodeNotPending}, nil, nil
	}
	for _, id := range []string{sender, recipient} {
		n, err := friendCount(ctx, tx, id)
		if err != nil {
			return "", nil, nil, err
		}
		if n >= cfg.FriendMax {
			return "", &Rejection{Code: CodeFriendLimit}, nil, nil
		}
	}
	lo, hi := orderedPair(sender, recipient)
	if _, err = tx.Exec(ctx, `INSERT INTO friendships(learner_low,learner_high) VALUES($1::uuid,$2::uuid) ON CONFLICT DO NOTHING`, lo, hi); err != nil {
		return "", nil, nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE friend_requests SET status='accepted',decided_at=now() WHERE id=$1::uuid`, requestID); err != nil {
		return "", nil, nil, err
	}
	return statusFriends, nil, []event{{"friend_request_accepted", map[string]any{}}}, nil
}

// requestOf reads a request addressed to the actor; anything else is not found.
func (s *Service) requestOf(ctx context.Context, actor, requestID string) (sender string, ok bool, err error) {
	err = s.pool.QueryRow(ctx, `SELECT sender_id::text FROM friend_requests WHERE id=$1::uuid AND recipient_id=$2::uuid`, requestID, actor).Scan(&sender)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return sender, err == nil, err
}

// Accept lets the recipient accept; the friendship exists only after this call.
func (s *Service) Accept(ctx context.Context, actor, requestID string) (Result, error) {
	sender, ok, err := s.requestOf(ctx, actor, requestID)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{}, &Rejection{Code: CodeRequestUnavailable}
	}
	cfg, err := s.config(ctx)
	if err != nil {
		return Result{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = lockLearners(ctx, tx, actor, sender); err != nil {
		return Result{}, err
	}
	status, rej, events, err := s.acceptLocked(ctx, tx, cfg, requestID, sender, actor)
	if err != nil {
		return Result{}, err
	}
	if rej != nil {
		return Result{}, rej
	}
	if err = tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	for _, e := range events {
		s.emit(actor, e.name, e.props)
	}
	return Result{Status: status}, nil
}

// Decline lets the recipient decline. The sender is not told; a re-send within the cooldown is refused neutrally.
func (s *Service) Decline(ctx context.Context, actor, requestID string) (Result, error) {
	sender, ok, err := s.requestOf(ctx, actor, requestID)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{}, &Rejection{Code: CodeRequestUnavailable}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = lockLearners(ctx, tx, actor, sender); err != nil {
		return Result{}, err
	}
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM friend_requests WHERE id=$1::uuid FOR UPDATE`, requestID).Scan(&status); errors.Is(err, pgx.ErrNoRows) {
		return Result{}, &Rejection{Code: CodeRequestUnavailable}
	} else if err != nil {
		return Result{}, err
	}
	switch status {
	case statusDeclined:
		return Result{Status: statusDeclined}, nil
	case "accepted":
		return Result{}, &Rejection{Code: CodeNotPending}
	}
	if _, err = tx.Exec(ctx, `UPDATE friend_requests SET status='declined',decided_at=now() WHERE id=$1::uuid`, requestID); err != nil {
		return Result{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	s.emit(actor, "friend_request_declined", map[string]any{})
	return Result{Status: statusDeclined}, nil
}

// Cancel withdraws the actor's own pending request; anything else is a no-op.
func (s *Service) Cancel(ctx context.Context, actor, requestID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM friend_requests WHERE id=$1::uuid AND sender_id=$2::uuid AND status='pending'`, requestID, actor)
	return err
}

// Remove ends a friendship the actor belongs to. A repeat or a stranger's call is a no-op.
func (s *Service) Remove(ctx context.Context, actor, friendshipID string) (Result, error) {
	var lo, hi string
	err := s.pool.QueryRow(ctx, `SELECT learner_low::text,learner_high::text FROM friendships WHERE id=$1::uuid AND (learner_low=$2::uuid OR learner_high=$2::uuid)`, friendshipID, actor).Scan(&lo, &hi)
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{Status: statusRemoved}, nil
	} else if err != nil {
		return Result{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	removed, ended, err := s.endFriendshipTx(ctx, tx, lo, hi)
	if err != nil {
		return Result{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	s.emitAll(ended)
	if removed {
		s.emit(actor, "friend_removed", map[string]any{})
	}
	return Result{Status: statusRemoved}, nil
}

// Friend and RequestView expose only username, display name, avatar and a privacy-permitted streak.
type Friend struct {
	ID    string    `json:"id"`
	Since time.Time `json:"since"`
	Identity
	FriendStreak *FriendStreakView `json:"friend_streak,omitempty"`
}

// RequestView is one pending request as the other party sees it.
type RequestView struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Identity
}

// rowStreaks adapts the personal streaks read with the friend list to StreakSource.
type rowStreaks map[string]int

func (m rowStreaks) Streak(learnerID string) (int, bool) { d, ok := m[learnerID]; return d, ok }

// Friends lists the actor's friends, alphabetically. A friend's personal and shared
// streak appear only when that friend's show_streak_to_friends is on.
func (s *Service) Friends(ctx context.Context, actor string) ([]Friend, error) {
	rows, err := s.pool.Query(ctx, `SELECT f.id::text,f.created_at,u.learner_id::text,u.username,l.display_name,COALESCE(ss.show_streak_to_friends,true),
 sst.current_days,f.learner_low::text,f.learner_high::text
 FROM friendships f JOIN learner_usernames u ON u.learner_id=CASE WHEN f.learner_low=$1::uuid THEN f.learner_high ELSE f.learner_low END
 JOIN learners l ON l.id=u.learner_id LEFT JOIN learner_social_settings ss ON ss.learner_id=u.learner_id
 LEFT JOIN streak_state sst ON sst.learner_id=u.learner_id
 WHERE (f.learner_low=$1::uuid OR f.learner_high=$1::uuid)`+NotBlockedSQL("$1", "u.learner_id")+` ORDER BY u.canonical LIMIT 1000`, actor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type entry struct {
		friend Friend
		row    identityRow
		lo, hi string
	}
	entries := []entry{}
	personal := rowStreaks{}
	for rows.Next() {
		var e entry
		var days *int
		if err = rows.Scan(&e.friend.ID, &e.friend.Since, &e.row.LearnerID, &e.row.Username, &e.row.DisplayName, &e.row.ShowStreakToFriends, &days, &e.lo, &e.hi); err != nil {
			return nil, err
		}
		if days != nil {
			personal[e.row.LearnerID] = *days
		}
		entries = append(entries, e)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	out := make([]Friend, 0, len(entries))
	for _, e := range entries {
		e.friend.Identity = e.row.Project(personal, true)
		if e.row.ShowStreakToFriends {
			view, err := s.friendStreakFor(ctx, actor, e.row.LearnerID, e.lo, e.hi, e.friend.Since)
			if err != nil {
				return nil, err
			}
			e.friend.FriendStreak = &view
		}
		out = append(out, e.friend)
	}
	return out, nil
}

// friendStreakFor builds the viewer's shared-streak view from the persisted pair state
// and both learners' E5 projections.
func (s *Service) friendStreakFor(ctx context.Context, viewer, friend, lo, hi string, since time.Time) (FriendStreakView, error) {
	st, err := loadPairState(ctx, s.pool, lo, hi)
	if err != nil {
		return FriendStreakView{}, err
	}
	youCursor, friendCursor := st.CursorLow, st.CursorHigh
	if viewer != lo {
		youCursor, friendCursor = friendCursor, youCursor
	}
	you, err := loadSide(ctx, s.pool, viewer, lowerBound(youCursor, since))
	if err != nil {
		return FriendStreakView{}, err
	}
	other, err := loadSide(ctx, s.pool, friend, lowerBound(friendCursor, since))
	if err != nil {
		return FriendStreakView{}, err
	}
	return friendStreakView(st, you, other, viewer == lo, s.clock()), nil
}

// Requests lists pending requests to and from the actor, newest first.
func (s *Service) Requests(ctx context.Context, actor string) (incoming, outgoing []RequestView, err error) {
	list := func(own, other string) ([]RequestView, error) {
		rows, err := s.pool.Query(ctx, `SELECT q.id::text,q.created_at,u.username,l.display_name
 FROM friend_requests q JOIN learner_usernames u ON u.learner_id=q.`+other+` JOIN learners l ON l.id=u.learner_id
 WHERE q.`+own+`=$1::uuid AND q.status='pending' ORDER BY q.created_at DESC LIMIT 200`, actor)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []RequestView{}
		for rows.Next() {
			var v RequestView
			var r identityRow
			if err = rows.Scan(&v.ID, &v.CreatedAt, &r.Username, &r.DisplayName); err != nil {
				return nil, err
			}
			v.Identity = r.Project(nil, false)
			out = append(out, v)
		}
		return out, rows.Err()
	}
	if incoming, err = list("recipient_id", "sender_id"); err != nil {
		return nil, nil, err
	}
	outgoing, err = list("sender_id", "recipient_id")
	return incoming, outgoing, err
}

// LinkState reports whether the actor has a usable friend link; it never returns the token.
type LinkState struct {
	Active    bool       `json:"active"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// LinkStatus reports the actor's current link state.
func (s *Service) LinkStatus(ctx context.Context, actor string) (LinkState, error) {
	var st LinkState
	var exp time.Time
	err := s.pool.QueryRow(ctx, `SELECT expires_at FROM friend_links WHERE owner_id=$1::uuid AND revoked_at IS NULL AND expires_at>now()`, actor).Scan(&exp)
	if errors.Is(err, pgx.ErrNoRows) {
		return st, nil
	}
	if err == nil {
		st.Active, st.ExpiresAt = true, &exp
	}
	return st, err
}

// CreateLink replaces the actor's link with a fresh one. The token is returned once; only its hash is stored.
func (s *Service) CreateLink(ctx context.Context, actor string) (token string, expires time.Time, err error) {
	cfg, err := s.config(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	var named bool
	if err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM learner_usernames WHERE learner_id=$1::uuid)`, actor).Scan(&named); err != nil {
		return "", time.Time{}, err
	}
	if !named {
		return "", time.Time{}, &Rejection{Code: CodeNotSet}
	}
	token, hash, err := newFriendToken()
	if err != nil {
		return "", time.Time{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = lockLearners(ctx, tx, actor); err != nil {
		return "", time.Time{}, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM friend_links WHERE owner_id=$1::uuid`, actor); err != nil {
		return "", time.Time{}, err
	}
	if err = tx.QueryRow(ctx, `INSERT INTO friend_links(owner_id,token_hash,expires_at) VALUES($1::uuid,$2,now()+make_interval(days=>$3)) RETURNING expires_at`, actor, hash, cfg.LinkValidDays).Scan(&expires); err != nil {
		return "", time.Time{}, err
	}
	return token, expires, tx.Commit(ctx)
}

// RevokeLink turns the actor's link off. Repeats are no-ops.
func (s *Service) RevokeLink(ctx context.Context, actor string) error {
	_, err := s.pool.Exec(ctx, `UPDATE friend_links SET revoked_at=now() WHERE owner_id=$1::uuid AND revoked_at IS NULL`, actor)
	return err
}

// ResolveLink previews who owns a valid link. It creates nothing; unusable links, own links and
// owners who accept no requests all return one neutral rejection.
func (s *Service) ResolveLink(ctx context.Context, actor, token string) (Identity, error) {
	cfg, err := s.config(ctx)
	if err != nil {
		return Identity{}, err
	}
	if rej, err := s.allow(ctx, actor, bucketLink, cfg.LinkPerHour, time.Hour); rej != nil || err != nil {
		if rej != nil {
			return Identity{}, rej
		}
		return Identity{}, err
	}
	if !validToken(token) {
		return Identity{}, unavailable(true)
	}
	var owner, policy string
	err = s.pool.QueryRow(ctx, `SELECT f.owner_id::text,COALESCE(ss.request_policy,'everyone') FROM friend_links f LEFT JOIN learner_social_settings ss ON ss.learner_id=f.owner_id
 WHERE f.token_hash=$1 AND f.revoked_at IS NULL AND f.expires_at>now()`, hashToken(token)).Scan(&owner, &policy)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (owner == actor || !policyAllows(policy, true, false))) {
		return Identity{}, unavailable(true)
	}
	if err != nil {
		return Identity{}, err
	}
	blocked, err := BlockedEitherWay(ctx, s.pool, actor, owner)
	if err != nil {
		return Identity{}, err
	}
	if blocked {
		return Identity{}, unavailable(true)
	}
	row, err := s.row(ctx, "u.learner_id=$1::uuid", owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return Identity{}, unavailable(true)
	}
	if err != nil {
		return Identity{}, err
	}
	return row.Project(nil, false), nil
}
