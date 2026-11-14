package sharing

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/datamonsterr/duolify/services/api/internal/social"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Invite errors. They are deliberately coarse: a refused invite never says why.
var (
	// ErrInviteUnavailable covers an unknown, hidden or self target and every invite the actor may
	// not act on (unknown id, wrong party, wrong state, expired, blocked).
	ErrInviteUnavailable = errors.New("invite_unavailable")
	// ErrCourseNotShared means the Course is private or has no passing publication: the client
	// asks the owner to change visibility to link first.
	ErrCourseNotShared = errors.New("course_not_shared")
	// ErrInviteRateLimited is the per-sender daily cap.
	ErrInviteRateLimited = errors.New("invite_rate_limited")
	// ErrInviteDuplicate means the recipient already has a pending invite to this Course.
	ErrInviteDuplicate = errors.New("invite_duplicate")
)

const (
	inviteInboxMax = 50
	inviteSentMax  = 100
	inviteStatus   = "pending"
)

// InviteInput names the recipient by Username or by a friendship of the actor, plus an optional
// operation key that makes a retry a replay.
type InviteInput struct {
	Username     string
	FriendshipID string
	Key          string
}

// Method is the closed analytics value for how the invite was addressed.
func (in InviteInput) Method() string {
	if in.FriendshipID != "" {
		return "friend"
	}
	return "username"
}

// ParseInviteBody reads exactly one of {"username"} or {"friendship_id"} plus an optional
// {"operation_key"}; unknown fields, trailing data and oversized bodies are refused.
func ParseInviteBody(r io.Reader) (InviteInput, error) {
	var in struct {
		Username     *string `json:"username"`
		FriendshipID *string `json:"friendship_id"`
		Key          *string `json:"operation_key"`
	}
	d := json.NewDecoder(io.LimitReader(r, 2048))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil {
		return InviteInput{}, err
	}
	var extra any
	if !errors.Is(d.Decode(&extra), io.EOF) {
		return InviteInput{}, errors.New("unexpected trailing data")
	}
	var out InviteInput
	if (in.Username == nil) == (in.FriendshipID == nil) {
		return out, errors.New("invite needs a username or a friendship_id")
	}
	if in.Username != nil {
		out.Username = strings.TrimPrefix(strings.TrimSpace(*in.Username), "@")
		if out.Username == "" || utf8.RuneCountInString(out.Username) > 64 {
			return InviteInput{}, errors.New("invalid username")
		}
	} else if id, err := uuid.Parse(*in.FriendshipID); err != nil {
		return InviteInput{}, errors.New("invalid friendship_id")
	} else {
		out.FriendshipID = id.String()
	}
	if in.Key != nil {
		k, err := uuid.Parse(*in.Key)
		if err != nil {
			return InviteInput{}, errors.New("invalid operation_key")
		}
		out.Key = k.String()
	}
	return out, nil
}

// InviteExpired is the expiry boundary: an invite is expired at and after expires_at.
func InviteExpired(now, expiresAt time.Time) bool { return !now.Before(expiresAt) }

// Invite is the sender's immediate result.
type Invite struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
}

// InviteOutcome is the result of accept, decline and revoke.
type InviteOutcome struct {
	Status   string `json:"status"`
	CourseID string `json:"course_id,omitempty"`
}

// SentInvite is one row of the owner's per-Course list.
type SentInvite struct {
	ID        string          `json:"id"`
	Status    string          `json:"status"`
	Method    string          `json:"method"`
	CreatedAt time.Time       `json:"created_at"`
	ExpiresAt time.Time       `json:"expires_at"`
	Recipient social.Identity `json:"recipient"`
}

// InviteCourse is the only Course field an invite shows its recipient.
type InviteCourse struct {
	Title string `json:"title"`
}

// ReceivedInvite is one inbox entry: the sender's public identity, the Course title, dates.
type ReceivedInvite struct {
	ID        string          `json:"id"`
	CreatedAt time.Time       `json:"created_at"`
	ExpiresAt time.Time       `json:"expires_at"`
	Read      bool            `json:"read"`
	Course    InviteCourse    `json:"course"`
	Sender    social.Identity `json:"sender"`
}

// WithPush sets the push adapter in place and returns the service. nil means disabled.
func (s *Service) WithPush(p PushSender) *Service { s.push = p; return s }

func (s *Service) pusher() PushSender {
	if s.push == nil {
		return DisabledPush{}
	}
	return s.push
}

// lockPair takes the same learner row locks as the E6 block, send and accept paths (ids sorted,
// FOR NO KEY UPDATE), so a CanInvite check made after it cannot race a concurrent block. It must
// not be FOR UPDATE: that conflicts with the FOR KEY SHARE that an in-flight enrollment's foreign
// keys take on both learner rows, and the two deadlocked (one accept aborted with no copy id).
func lockPair(ctx context.Context, tx pgx.Tx, ids ...string) error {
	rows, err := tx.Query(ctx, `SELECT id FROM learners WHERE id = ANY($1::uuid[]) ORDER BY id FOR NO KEY UPDATE`, ids)
	if err != nil {
		return err
	}
	rows.Close()
	return rows.Err()
}

func avatarOf(display, username string) social.Avatar {
	name := strings.TrimSpace(display)
	if name == "" {
		name = username
	}
	r, _ := utf8.DecodeRuneInString(name)
	if r == utf8.RuneError {
		return social.Avatar{Initial: "?"}
	}
	return social.Avatar{Initial: strings.ToUpper(string(r))}
}

func identityOf(username, display string) social.Identity {
	return social.Identity{Username: username, DisplayName: display, Avatar: avatarOf(display, username)}
}

// resolveRecipient maps the input to a learner id. A friendship id must belong to the actor.
func (s *Service) resolveRecipient(ctx context.Context, actor string, in InviteInput) (string, error) {
	var id string
	var err error
	if in.FriendshipID != "" {
		err = s.pool.QueryRow(ctx, `SELECT CASE WHEN learner_low=$2::uuid THEN learner_high ELSE learner_low END::text
 FROM friendships WHERE id=$1::uuid AND (learner_low=$2::uuid OR learner_high=$2::uuid)`, in.FriendshipID, actor).Scan(&id)
	} else {
		err = s.pool.QueryRow(ctx, `SELECT learner_id::text FROM learner_usernames WHERE canonical=$1`, social.Canonical(strings.TrimPrefix(strings.TrimSpace(in.Username), "@"))).Scan(&id)
	}
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && id == actor) {
		return "", ErrInviteUnavailable
	}
	return id, err
}

// SendInvite invites a Username or friend to the owner's shared Course. Every check that depends on
// the pair runs inside one transaction that first locks both learners, and CanInvite runs in that
// same transaction, so a concurrent block either happens before (the invite is refused) or after
// (the block's OnBlock callback cancels it).
func (s *Service) SendInvite(ctx context.Context, actor, courseID string, in InviteInput) (Invite, error) {
	if !validID(actor) || !validID(courseID) {
		return Invite{}, ErrCourseUnavailable
	}
	var visibility string
	var passing bool
	err := s.pool.QueryRow(ctx, `SELECT c.visibility::text, EXISTS (SELECT 1 FROM publication_drafts d WHERE d.course_id=c.id AND d.verdict='pass')
 FROM courses c WHERE c.id=$1 AND c.learner_id=$2 AND c.deleted_at IS NULL`, courseID, actor).Scan(&visibility, &passing)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invite{}, ErrCourseUnavailable
	}
	if err != nil {
		return Invite{}, err
	}
	if (visibility != "link" && visibility != "public") || !passing {
		return Invite{}, ErrCourseNotShared
	}
	recipient, err := s.resolveRecipient(ctx, actor, in)
	if err != nil {
		return Invite{}, err
	}
	key := in.Key
	if key == "" {
		key = uuid.NewString()
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Invite{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockPair(ctx, tx, actor, recipient); err != nil {
		return Invite{}, err
	}
	var out Invite
	err = tx.QueryRow(ctx, `SELECT id::text,status,expires_at FROM course_invites WHERE sender_id=$1::uuid AND operation_key=$2::uuid`, actor, key).Scan(&out.ID, &out.Status, &out.ExpiresAt)
	if err == nil {
		return out, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Invite{}, err
	}
	// A pending invite past its expiry must not hold the one-pending slot.
	if _, err = tx.Exec(ctx, `UPDATE course_invites SET status='expired', resolved_at=expires_at WHERE recipient_id=$1::uuid AND course_id=$2::uuid AND status='pending' AND expires_at<=now()`, recipient, courseID); err != nil {
		return Invite{}, err
	}
	// Reachable like in search: a friend, or an exact username the recipient allows, never across a
	// block. Anything else is the same answer as an unknown username.
	var reachable bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM friendships WHERE learner_low=LEAST($1::uuid,$2::uuid) AND learner_high=GREATEST($1::uuid,$2::uuid))
 OR (EXISTS (SELECT 1 FROM learner_usernames u LEFT JOIN learner_social_settings ss ON ss.learner_id=u.learner_id WHERE u.learner_id=$2::uuid
     AND (COALESCE(ss.discoverable,true) OR COALESCE(ss.request_policy,'everyone')='everyone'))
  AND NOT EXISTS (SELECT 1 FROM learner_blocks b WHERE (b.blocker_id=$1::uuid AND b.blocked_id=$2::uuid) OR (b.blocker_id=$2::uuid AND b.blocked_id=$1::uuid)))`, actor, recipient).Scan(&reachable); err != nil {
		return Invite{}, err
	}
	if !reachable {
		return Invite{}, ErrInviteUnavailable
	}
	var pending bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM course_invites WHERE recipient_id=$1::uuid AND course_id=$2::uuid AND status='pending')`, recipient, courseID).Scan(&pending); err != nil {
		return Invite{}, err
	}
	if pending {
		return Invite{}, ErrInviteDuplicate
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "course-invite-cap:"+actor); err != nil {
		return Invite{}, err
	}
	var recent int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM course_invites WHERE sender_id=$1::uuid AND created_at > now() - interval '24 hours'`, actor).Scan(&recent); err != nil {
		return Invite{}, err
	}
	if s.cfg.InviteSendsPerDay > 0 && recent >= s.cfg.InviteSendsPerDay {
		slog.WarnContext(ctx, "sharing abuse signal: daily invite limit reached", "learner_id", actor, "limit", s.cfg.InviteSendsPerDay)
		return Invite{}, ErrInviteRateLimited
	}
	allowed, err := social.CanInvite(ctx, tx, actor, recipient)
	if err != nil {
		return Invite{}, err // fail closed: no invite is written on an authorization error
	}
	err = tx.QueryRow(ctx, `INSERT INTO course_invites(course_id,sender_id,recipient_id,operation_key,method,delivered,expires_at)
 VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,$6,now()+make_interval(days=>$7)) RETURNING id::text,status,expires_at`,
		courseID, actor, recipient, key, in.Method(), allowed, s.cfg.InviteExpiryDays).Scan(&out.ID, &out.Status, &out.ExpiresAt)
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return Invite{}, ErrInviteDuplicate
	}
	if err != nil {
		return Invite{}, err
	}
	if allowed {
		if _, err = tx.Exec(ctx, `INSERT INTO course_invite_deliveries(invite_id,channel,status,sent_at) VALUES($1::uuid,'in_app','sent',now())`, out.ID); err != nil {
			return Invite{}, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO course_invite_deliveries(invite_id,channel,status) VALUES($1::uuid,'push','pending')`, out.ID); err != nil {
			return Invite{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Invite{}, err
	}
	s.emit(actor, "course_invite_sent", map[string]any{"method": in.Method(), "course_id": courseID, "invite_id": out.ID})
	return out, nil
}

// SentInvites lists the owner's invites for one Course. An invite cancelled by a block is shown as
// pending until it expires, so the list never reveals a block.
func (s *Service) SentInvites(ctx context.Context, actor, courseID string) ([]SentInvite, error) {
	if !validID(actor) || !validID(courseID) {
		return nil, ErrCourseUnavailable
	}
	var owned bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM courses WHERE id=$1 AND learner_id=$2 AND deleted_at IS NULL)`, courseID, actor).Scan(&owned); err != nil {
		return nil, err
	}
	if !owned {
		return nil, ErrCourseUnavailable
	}
	rows, err := s.pool.Query(ctx, `SELECT i.id::text,
 CASE WHEN i.status='revoked' AND i.cancel_reason='block' THEN CASE WHEN i.expires_at<=now() THEN 'expired' ELSE 'pending' END
      WHEN i.status='pending' AND i.expires_at<=now() THEN 'expired' ELSE i.status END,
 i.method,i.created_at,i.expires_at,COALESCE(u.username,''),l.display_name
 FROM course_invites i JOIN learners l ON l.id=i.recipient_id LEFT JOIN learner_usernames u ON u.learner_id=i.recipient_id
 WHERE i.sender_id=$1::uuid AND i.course_id=$2::uuid ORDER BY i.created_at DESC, i.id LIMIT `+strconv.Itoa(inviteSentMax), actor, courseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SentInvite{}
	for rows.Next() {
		var i SentInvite
		var username, display string
		if err = rows.Scan(&i.ID, &i.Status, &i.Method, &i.CreatedAt, &i.ExpiresAt, &username, &display); err != nil {
			return nil, err
		}
		i.Recipient = identityOf(username, display)
		out = append(out, i)
	}
	return out, rows.Err()
}

// ReceivedInvites is the in-app inbox: pending, delivered, unexpired invites from unblocked
// senders to Courses that are still shared. It shows the Course title and the sender's public
// identity only, newest first and bounded.
func (s *Service) ReceivedInvites(ctx context.Context, actor string) ([]ReceivedInvite, int, error) {
	rows, err := s.pool.Query(ctx, `SELECT i.id::text,i.created_at,i.expires_at,i.read_at IS NOT NULL,c.title,COALESCE(u.username,''),l.display_name
 FROM course_invites i JOIN courses c ON c.id=i.course_id AND c.deleted_at IS NULL AND c.visibility<>'private'
 JOIN learners l ON l.id=i.sender_id LEFT JOIN learner_usernames u ON u.learner_id=i.sender_id
 WHERE i.recipient_id=$1::uuid AND i.status='pending' AND i.delivered AND i.expires_at>now()`+social.NotBlockedSQL("i.recipient_id", "i.sender_id")+`
 ORDER BY i.created_at DESC, i.id LIMIT `+strconv.Itoa(inviteInboxMax), actor)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out, unread := []ReceivedInvite{}, 0
	for rows.Next() {
		var i ReceivedInvite
		var username, display string
		if err = rows.Scan(&i.ID, &i.CreatedAt, &i.ExpiresAt, &i.Read, &i.Course.Title, &username, &display); err != nil {
			return nil, 0, err
		}
		i.Sender = identityOf(username, display)
		if !i.Read {
			unread++
		}
		out = append(out, i)
	}
	return out, unread, rows.Err()
}

// MarkInvitesRead marks the actor's inbox read.
func (s *Service) MarkInvitesRead(ctx context.Context, actor string) error {
	_, err := s.pool.Exec(ctx, `UPDATE course_invites SET read_at=now() WHERE recipient_id=$1::uuid AND delivered AND status='pending' AND read_at IS NULL`, actor)
	return err
}

type inviteRow struct {
	sender, course, status, cancel string
	copyID                         *string
	delivered                      bool
	expired                        bool
}

func (s *Service) lockInvite(ctx context.Context, tx pgx.Tx, id, column, actor string) (inviteRow, error) {
	var r inviteRow
	var cancel *string
	if !validID(id) || !validID(actor) {
		return r, ErrInviteUnavailable
	}
	err := tx.QueryRow(ctx, `SELECT sender_id::text,course_id::text,status,cancel_reason,copy_course_id::text,delivered,expires_at<=now()
 FROM course_invites WHERE id=$1::uuid AND `+column+`=$2::uuid FOR UPDATE`, id, actor).Scan(&r.sender, &r.course, &r.status, &cancel, &r.copyID, &r.delivered, &r.expired)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrInviteUnavailable
	}
	if cancel != nil {
		r.cancel = *cancel
	}
	return r, err
}

// settle ends a pending invite as the given final status, or reports the replay of the same status.
func (s *Service) settle(ctx context.Context, id, column, actor, to string) (InviteOutcome, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return InviteOutcome{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	r, err := s.lockInvite(ctx, tx, id, column, actor)
	if err != nil {
		return InviteOutcome{}, false, err
	}
	if r.status == to {
		return InviteOutcome{Status: to}, false, nil // replay
	}
	if r.status != inviteStatus || r.expired || (column == "recipient_id" && !r.delivered) {
		return InviteOutcome{}, false, ErrInviteUnavailable
	}
	if _, err = tx.Exec(ctx, `UPDATE course_invites SET status=$2, resolved_at=now() WHERE id=$1::uuid`, id, to); err != nil {
		return InviteOutcome{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return InviteOutcome{}, false, err
	}
	return InviteOutcome{Status: to}, true, nil
}

// DeclineInvite lets the recipient decline a pending invite; repeating it is a replay.
func (s *Service) DeclineInvite(ctx context.Context, actor, id string) (InviteOutcome, error) {
	out, changed, err := s.settle(ctx, id, "recipient_id", actor, "declined")
	if err != nil {
		return out, err
	}
	if changed {
		s.emit(actor, "course_invite_declined", map[string]any{"invite_id": id, "course_id": s.inviteCourse(ctx, id)})
	}
	return out, nil
}

// RevokeInvite lets the sender withdraw a pending invite; repeating it is a replay.
func (s *Service) RevokeInvite(ctx context.Context, actor, id string) (InviteOutcome, error) {
	out, _, err := s.settle(ctx, id, "sender_id", actor, "revoked")
	return out, err
}

func (s *Service) inviteCourse(ctx context.Context, id string) string {
	var c string
	_ = s.pool.QueryRow(ctx, `SELECT course_id::text FROM course_invites WHERE id=$1::uuid`, id).Scan(&c)
	return c
}

// AcceptInvite marks the invite accepted and enrolls the recipient through the one free-enrollment
// path (source "invite"). The invite is committed before enrolling because enrollment inserts
// learner-owned rows, which would deadlock against the learner row locks held here. An accepted
// invite without a copy is finished by a repeated accept, so the copy exists exactly once.
func (s *Service) AcceptInvite(ctx context.Context, actor, id string) (InviteOutcome, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return InviteOutcome{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var sender string
	// Use the connection already held by this transaction. Acquiring another
	// pooled connection here can starve concurrent accepts on small pools.
	if err = tx.QueryRow(ctx, `SELECT sender_id::text FROM course_invites WHERE id=$1::uuid AND recipient_id=$2::uuid`, id, actor).Scan(&sender); err != nil {
		if errors.Is(err, pgx.ErrNoRows) || !validID(id) {
			return InviteOutcome{}, ErrInviteUnavailable
		}
		return InviteOutcome{}, err
	}
	if err = lockPair(ctx, tx, sender, actor); err != nil {
		return InviteOutcome{}, err
	}
	r, err := s.lockInvite(ctx, tx, id, "recipient_id", actor)
	if err != nil {
		return InviteOutcome{}, err
	}
	first := false
	switch {
	case r.status == "accepted" && r.copyID != nil:
		return InviteOutcome{Status: "accepted", CourseID: *r.copyID}, nil
	case r.status == "accepted":
	case r.status != inviteStatus || !r.delivered:
		return InviteOutcome{}, ErrInviteUnavailable
	case r.expired:
		if _, err = tx.Exec(ctx, `UPDATE course_invites SET status='expired', resolved_at=expires_at WHERE id=$1::uuid`, id); err == nil {
			err = tx.Commit(ctx)
		}
		if err != nil {
			return InviteOutcome{}, err
		}
		return InviteOutcome{}, ErrInviteUnavailable
	default:
		first = true
	}
	if ok, cerr := social.CanInteract(ctx, tx, sender, actor); cerr != nil {
		return InviteOutcome{}, cerr
	} else if !ok {
		return InviteOutcome{}, ErrInviteUnavailable
	}
	var shared bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM courses c WHERE c.id=$1::uuid AND c.deleted_at IS NULL AND c.visibility<>'private'
 AND EXISTS (SELECT 1 FROM publication_drafts d WHERE d.course_id=c.id AND d.verdict='pass'))`, r.course).Scan(&shared); err != nil {
		return InviteOutcome{}, err
	}
	if !shared {
		return InviteOutcome{}, ErrInviteUnavailable
	}
	if first {
		if _, err = tx.Exec(ctx, `UPDATE course_invites SET status='accepted', resolved_at=now() WHERE id=$1::uuid`, id); err != nil {
			return InviteOutcome{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return InviteOutcome{}, err
	}
	if first {
		s.emit(actor, "course_invite_accepted", map[string]any{"invite_id": id, "course_id": r.course})
	}
	enrolled, err := s.enrollCourse(ctx, actor, r.course, r.sender, "invite")
	if err != nil {
		return InviteOutcome{}, err
	}
	if _, err = s.pool.Exec(ctx, `UPDATE course_invites SET copy_course_id=$2::uuid WHERE id=$1::uuid AND copy_course_id IS NULL`, id, enrolled.CourseID); err != nil {
		return InviteOutcome{}, err
	}
	return InviteOutcome{Status: "accepted", CourseID: enrolled.CourseID}, nil
}

// CancelInvitesOnBlock is the social.OnBlock callback. It runs inside the block transaction and
// cancels pending invites between the pair in both directions.
func (s *Service) CancelInvitesOnBlock(ctx context.Context, tx pgx.Tx, blocker, blocked string) error {
	_, err := tx.Exec(ctx, `UPDATE course_invites SET status='revoked', cancel_reason='block', resolved_at=now()
 WHERE status='pending' AND ((sender_id=$1::uuid AND recipient_id=$2::uuid) OR (sender_id=$2::uuid AND recipient_id=$1::uuid))`, blocker, blocked)
	return err
}

// DrainInviteDeliveries sends queued push deliveries through the adapter. Each is re-checked at
// send time (invite still pending and delivered, not expired, no block, setting still admits the
// sender); a denied or disabled adapter suppresses it, any other failure is retried up to the
// configured attempts and then fails. None of this touches the in-app inbox.
func (s *Service) DrainInviteDeliveries(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT d.id::text,d.invite_id::text,d.attempts,i.sender_id::text,i.recipient_id::text,i.status='pending' AND i.delivered AND i.expires_at>now()
 FROM course_invite_deliveries d JOIN course_invites i ON i.id=d.invite_id
 WHERE d.channel='push' AND d.status='pending' ORDER BY d.created_at,d.id LIMIT 50 FOR UPDATE OF d SKIP LOCKED`)
	if err != nil {
		return err
	}
	type job struct {
		id, invite, sender, recipient string
		attempts                      int
		live                          bool
	}
	var jobs []job
	for rows.Next() {
		var j job
		if err = rows.Scan(&j.id, &j.invite, &j.attempts, &j.sender, &j.recipient, &j.live); err != nil {
			rows.Close()
			return err
		}
		jobs = append(jobs, j)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, j := range jobs {
		status := "suppressed"
		attempts := j.attempts
		if allowed, cerr := social.CanInvite(ctx, tx, j.sender, j.recipient); j.live && cerr == nil && allowed {
			attempts++
			switch serr := s.pusher().Send(ctx, PushMessage{RecipientID: j.recipient, InviteID: j.invite, Kind: PushKindCourseInvite}); {
			case serr == nil:
				status = "sent"
			case errors.Is(serr, ErrPushDenied), errors.Is(serr, ErrPushDisabled):
			case attempts >= s.cfg.PushMaxAttempts:
				status = "failed"
				slog.WarnContext(ctx, "course invite push failed", "invite_id", j.invite, "attempts", attempts)
			default:
				status = "pending"
			}
		}
		if _, err = tx.Exec(ctx, `UPDATE course_invite_deliveries SET status=$2, attempts=$3, sent_at=CASE WHEN $2='sent' THEN now() ELSE sent_at END WHERE id=$1::uuid`, j.id, status, attempts); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// SweepInvites persists expiry and purges final invites (and their delivery rows, by cascade)
// the configured number of days after they ended.
func (s *Service) SweepInvites(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `UPDATE course_invites SET status='expired', resolved_at=expires_at WHERE status='pending' AND expires_at<=now()`); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM course_invites WHERE status<>'pending' AND resolved_at < now() - make_interval(days => $1)`, s.cfg.InvitePurgeDays)
	return err
}

// MaintainInvites drains push deliveries every few seconds and sweeps expiry and retention hourly.
func (s *Service) MaintainInvites(ctx context.Context) {
	drain := time.NewTicker(10 * time.Second)
	sweep := time.NewTicker(time.Hour)
	defer drain.Stop()
	defer sweep.Stop()
	for {
		if err := s.SweepInvites(ctx); err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "course invite sweep failed", "error", err)
		}
	wait:
		for {
			select {
			case <-ctx.Done():
				return
			case <-drain.C:
				if err := s.DrainInviteDeliveries(ctx); err != nil && ctx.Err() == nil {
					slog.ErrorContext(ctx, "course invite push drain failed", "error", err)
				}
			case <-sweep.C:
				break wait
			}
		}
	}
}
