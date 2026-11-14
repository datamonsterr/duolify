package sharing

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

var (
	// ErrNotModerator is the neutral refusal for anyone without a moderator grant.
	ErrNotModerator = errors.New("not_moderator")
	// ErrInvalidModeration rejects unknown actions, reason codes, ids or a missing reason.
	ErrInvalidModeration = errors.New("invalid_moderation")
	// ErrModerated stops a moderated owner from relisting or republishing.
	ErrModerated = errors.New("moderated")
	// ErrAppealUsed refuses a second appeal of the same action.
	ErrAppealUsed = errors.New("appeal_used")
	// ErrSameModerator refuses deciding an appeal of your own action.
	ErrSameModerator = errors.New("same_moderator")
	// ErrSelfModeration refuses acting on your own content.
	ErrSelfModeration = errors.New("self_moderation")
)

// ScreeningInfo is what the moderator sees about a held publication.
type ScreeningInfo struct {
	Verdict        string   `json:"verdict"`
	Codes          []string `json:"codes"`
	SampledLessons int      `json:"sampled_lessons"`
}

// AuditEntry is one past action on the same target.
type AuditEntry struct {
	At         time.Time `json:"at"`
	Action     string    `json:"action"`
	ReasonCode string    `json:"reason_code"`
}

// ModerationItem is one queue row with everything the detail panel shows.
type ModerationItem struct {
	ID          string         `json:"id"`
	TargetType  string         `json:"target_type"`
	TargetID    string         `json:"target_id"`
	CourseID    string         `json:"course_id"`
	Title       string         `json:"title"`
	Severity    int            `json:"severity"`
	Reasons     []string       `json:"reasons"`
	ReportCount int            `json:"report_count"`
	OpenedAt    time.Time      `json:"opened_at"`
	DueAt       time.Time      `json:"due_at"`
	Overdue     bool           `json:"overdue"`
	Hidden      bool           `json:"hidden"`
	Version     int            `json:"version"`
	Screening   *ScreeningInfo `json:"screening,omitempty"`
	Audit       []AuditEntry   `json:"audit"`
}

// AppealItem is an open appeal waiting for a moderator other than the one who acted.
type AppealItem struct {
	ID         string    `json:"id"`
	ActionID   string    `json:"action_id"`
	CourseID   string    `json:"course_id"`
	Title      string    `json:"title"`
	Action     string    `json:"action"`
	ReasonCode string    `json:"reason_code"`
	Text       string    `json:"text"`
	CreatedAt  time.Time `json:"created_at"`
}

// ModerationView is the whole moderator surface.
type ModerationView struct {
	Items          []ModerationItem `json:"items"`
	Appeals        []AppealItem     `json:"appeals"`
	OpenCount      int              `json:"open_count"`
	OverdueCount   int              `json:"overdue_count"`
	ScreeningCount int              `json:"screening_count"`
}

// ActionInput is one moderator decision.
type ActionInput struct{ Action, ReasonCode, Reason string }

// ActionResult reports what the action did.
type ActionResult struct {
	ID            string `json:"id"`
	Action        string `json:"action"`
	Severe        bool   `json:"severe"`
	CopiesBlocked int    `json:"copies_blocked"`
}

// AppealState is the author's view of their one appeal.
type AppealState struct {
	Status    string     `json:"status"`
	DecidedAt *time.Time `json:"decided_at,omitempty"`
}

// Notice is what an author sees about one action against their content.
type Notice struct {
	ID         string       `json:"id"`
	Action     string       `json:"action"`
	ReasonCode string       `json:"reason_code"`
	Reason     string       `json:"reason"`
	TargetType string       `json:"target_type"`
	CourseID   string       `json:"course_id"`
	Title      string       `json:"title"`
	Severe     bool         `json:"severe"`
	CreatedAt  time.Time    `json:"created_at"`
	CanAppeal  bool         `json:"can_appeal"`
	Appeal     *AppealState `json:"appeal,omitempty"`
}

var actionMatrix = map[string][]string{
	"course":      {"dismiss", "warn", "delist", "takedown", "ban_publishing"},
	"publication": {"dismiss", "delist", "takedown", "ban_publishing"},
	"review":      {"dismiss", "warn", "delist", "takedown"},
	"user":        {"dismiss", "warn", "ban_publishing"},
}

// ActionAllowed is the action matrix per target type.
func ActionAllowed(targetType, action string) bool {
	return slices.Contains(actionMatrix[targetType], action)
}

// ValidateModeration checks the matrix, the closed reason code and the reason text bounds.
func ValidateModeration(targetType string, in ActionInput, cfg Config) error {
	reason := strings.TrimSpace(in.Reason)
	if !ActionAllowed(targetType, in.Action) || !slices.Contains(cfg.ModerationReasonCodes, in.ReasonCode) || reason == "" ||
		!utf8.ValidString(reason) || strings.ContainsRune(reason, 0) || utf8.RuneCountInString(reason) > cfg.ModerationReasonMaxChars {
		return ErrInvalidModeration
	}
	return nil
}

// IsSevere is true for a takedown whose reason code is configured as severe.
func IsSevere(action, code string, cfg Config) bool {
	return action == "takedown" && slices.Contains(cfg.ModerationSevereCodes, code)
}

// DueAt is the first-response deadline of a queue item.
func DueAt(opened time.Time, cfg Config) time.Time {
	return opened.Add(time.Duration(cfg.ModerationTargetHours) * time.Hour)
}

// IsOverdue reports an item still open past its first-response target.
func IsOverdue(opened, now time.Time, cfg Config) bool { return now.After(DueAt(opened, cfg)) }

// HeldSeverity is the queue severity of a held publication verdict.
func HeldSeverity(verdict string, cfg Config) int {
	if verdict != VerdictReview && verdict != VerdictPending {
		return 0
	}
	return cfg.ModerationHeldSeverity[verdict]
}

// IsModerator reports whether the account holds a moderator grant.
func (s *Service) IsModerator(ctx context.Context, actor string) (bool, error) {
	if !validID(actor) {
		return false, nil
	}
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM moderator_grants WHERE account_id=$1::uuid)`, actor).Scan(&ok)
	return ok, err
}

func (s *Service) requireModerator(ctx context.Context, actor string) error {
	ok, err := s.IsModerator(ctx, actor)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotModerator
	}
	return nil
}

// moderated refuses a moderated course or a banned publisher. It is checked on every listing,
// link and republish path, so an owner cannot undo an action by publishing again.
func (s *Service) moderated(ctx context.Context, q pgx.Tx, owner, courseID string) error {
	var blocked bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM courses WHERE id=$1 AND moderation_state<>'none') OR EXISTS(SELECT 1 FROM publishing_bans WHERE learner_id=$2)`, courseID, owner).Scan(&blocked); err != nil {
		return err
	}
	if blocked {
		return ErrModerated
	}
	return nil
}

// enqueueHeld opens (or refreshes) the single queue item of a held publication and resolves it
// once the draft is no longer held. Called with the screening result inside its transaction.
func (s *Service) enqueueHeld(ctx context.Context, tx pgx.Tx, draftID, verdict string) error {
	if severity := HeldSeverity(verdict, s.cfg); severity > 0 {
		_, err := tx.Exec(ctx, `INSERT INTO moderation_queue_items(target_type,target_id,severity,report_count,draft_id)
 SELECT 'publication',d.course_id,$2,0,d.id FROM publication_drafts d WHERE d.id=$1
 ON CONFLICT (target_type,target_id) WHERE status='open' DO UPDATE SET severity=EXCLUDED.severity,draft_id=EXCLUDED.draft_id`, draftID, severity)
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE moderation_queue_items SET status='resolved',resolved_at=now()
 WHERE target_type='publication' AND status='open' AND draft_id=$1`, draftID)
	return err
}

// materializeReports turns open review and user reports into queue rows. Course reports queue
// themselves at the hide threshold (E15-09); reviews and users have no threshold.
func (s *Service) materializeReports(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `SELECT target_type,target_id::text,array_agg(reason ORDER BY reason),count(*),min(created_at) FROM content_reports
 WHERE status='open' AND target_type IN ('review','user') GROUP BY target_type,target_id`)
	if err != nil {
		return err
	}
	type group struct {
		kind, id string
		reasons  []string
		n        int
		first    time.Time
	}
	var groups []group
	for rows.Next() {
		var g group
		if err = rows.Scan(&g.kind, &g.id, &g.reasons, &g.n, &g.first); err != nil {
			rows.Close()
			return err
		}
		groups = append(groups, g)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, g := range groups {
		if _, err = s.pool.Exec(ctx, `INSERT INTO moderation_queue_items(target_type,target_id,severity,report_count,opened_at) VALUES($1,$2,$3,$4,$5)
 ON CONFLICT (target_type,target_id) WHERE status='open' DO UPDATE SET severity=EXCLUDED.severity,report_count=EXCLUDED.report_count`,
			g.kind, g.id, QueueSeverity(g.reasons, s.cfg), g.n, g.first); err != nil {
			return err
		}
	}
	return nil
}

// ModerationView is the moderator surface: open items by severity then age, open appeals and counts.
func (s *Service) ModerationView(ctx context.Context, actor string, limit int) (ModerationView, error) {
	view := ModerationView{Items: []ModerationItem{}, Appeals: []AppealItem{}}
	if err := s.requireModerator(ctx, actor); err != nil {
		return view, err
	}
	if err := s.materializeReports(ctx); err != nil {
		return view, err
	}
	limit = min(max(limit, 1), 200)
	if err := s.pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE opened_at + make_interval(hours => $1) < now()),count(*) FILTER (WHERE target_type='publication')
 FROM moderation_queue_items WHERE status='open'`, s.cfg.ModerationTargetHours).Scan(&view.OpenCount, &view.OverdueCount, &view.ScreeningCount); err != nil {
		return view, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id::text,target_type,target_id::text,severity,report_count,opened_at,COALESCE(draft_id::text,'') FROM moderation_queue_items
 WHERE status='open' ORDER BY severity DESC,opened_at,id LIMIT $1`, limit)
	if err != nil {
		return view, err
	}
	var draftIDs []string
	for rows.Next() {
		var it ModerationItem
		var draft string
		if err = rows.Scan(&it.ID, &it.TargetType, &it.TargetID, &it.Severity, &it.ReportCount, &it.OpenedAt, &draft); err != nil {
			rows.Close()
			return view, err
		}
		it.DueAt = DueAt(it.OpenedAt, s.cfg)
		it.Overdue = IsOverdue(it.OpenedAt, time.Now(), s.cfg)
		view.Items = append(view.Items, it)
		draftIDs = append(draftIDs, draft)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return view, err
	}
	for i := range view.Items {
		if err = s.describe(ctx, &view.Items[i], draftIDs[i]); err != nil {
			return view, err
		}
	}
	arows, err := s.pool.Query(ctx, `SELECT ap.id::text,a.id::text,COALESCE(a.course_id::text,''),COALESCE(c.title,''),a.action,a.reason_code,ap.text,ap.created_at
 FROM moderation_appeals ap JOIN moderation_actions a ON a.id=ap.action_id LEFT JOIN courses c ON c.id=a.course_id
 WHERE ap.status='open' ORDER BY ap.created_at,ap.id LIMIT $1`, limit)
	if err != nil {
		return view, err
	}
	defer arows.Close()
	for arows.Next() {
		var a AppealItem
		if err = arows.Scan(&a.ID, &a.ActionID, &a.CourseID, &a.Title, &a.Action, &a.ReasonCode, &a.Text, &a.CreatedAt); err != nil {
			return view, err
		}
		view.Appeals = append(view.Appeals, a)
	}
	return view, arows.Err()
}

func (s *Service) describe(ctx context.Context, it *ModerationItem, draftID string) error {
	it.Reasons, it.Audit = []string{}, []AuditEntry{}
	switch it.TargetType {
	case "course", "publication":
		it.CourseID = it.TargetID
	case "review":
		if err := s.pool.QueryRow(ctx, `SELECT course_id::text FROM course_reviews WHERE id=$1`, it.TargetID).Scan(&it.CourseID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	var title string
	switch it.TargetType {
	case "user":
		if err := s.pool.QueryRow(ctx, `SELECT COALESCE((SELECT username FROM learner_usernames WHERE learner_id=$1),'')`, it.TargetID).Scan(&title); err != nil {
			return err
		}
		if title != "" {
			title = "@" + title
		}
	default:
		if err := s.pool.QueryRow(ctx, `SELECT COALESCE((SELECT title FROM courses WHERE id=NULLIF($1,'')::uuid),'')`, it.CourseID).Scan(&title); err != nil {
			return err
		}
	}
	it.Title = title
	if it.TargetType == "course" {
		if err := s.pool.QueryRow(ctx, `SELECT COALESCE((SELECT version FROM marketplace_listings WHERE course_id=$1),0),COALESCE((SELECT hidden_at IS NOT NULL FROM marketplace_listings WHERE course_id=$1),false)`, it.TargetID).Scan(&it.Version, &it.Hidden); err != nil {
			return err
		}
	}
	if it.TargetType == "publication" {
		info := ScreeningInfo{Codes: []string{}}
		if err := s.pool.QueryRow(ctx, `SELECT number,verdict,reason_codes,sampled_lessons FROM publication_drafts WHERE id=NULLIF($1,'')::uuid`, draftID).Scan(&it.Version, &info.Verdict, &info.Codes, &info.SampledLessons); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		it.Screening = &info
	}
	if it.TargetType != "publication" {
		if err := s.pool.QueryRow(ctx, `SELECT COALESCE(array_agg(DISTINCT reason ORDER BY reason),'{}') FROM content_reports WHERE target_type=$1 AND target_id=$2 AND status='open'`, it.TargetType, it.TargetID).Scan(&it.Reasons); err != nil {
			return err
		}
	}
	rows, err := s.pool.Query(ctx, `SELECT created_at,action,reason_code FROM moderation_actions WHERE target_type=$1 AND target_id=$2 ORDER BY created_at,id LIMIT 20`, it.TargetType, it.TargetID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var a AuditEntry
		if err = rows.Scan(&a.At, &a.Action, &a.ReasonCode); err != nil {
			return err
		}
		it.Audit = append(it.Audit, a)
	}
	return rows.Err()
}

// target holds what an action needs about its subject.
type target struct {
	course, author, draft, verdict string
	listingActive                  bool
}

func (s *Service) loadTarget(ctx context.Context, tx pgx.Tx, kind, id, draftID string) (target, error) {
	var t target
	var err error
	switch kind {
	case "course":
		t.course = id
		err = tx.QueryRow(ctx, `SELECT c.learner_id::text,EXISTS(SELECT 1 FROM marketplace_listings WHERE course_id=c.id AND delisted_at IS NULL) FROM courses c WHERE c.id=$1 FOR UPDATE`, id).Scan(&t.author, &t.listingActive)
	case "publication":
		t.course = id
		err = tx.QueryRow(ctx, `SELECT d.owner_id::text,d.id::text,d.verdict FROM publication_drafts d WHERE d.course_id=$1 AND ($2='' OR d.id=NULLIF($2,'')::uuid) ORDER BY d.number DESC LIMIT 1 FOR UPDATE`, id, draftID).Scan(&t.author, &t.draft, &t.verdict)
	case "review":
		err = tx.QueryRow(ctx, `SELECT COALESCE(learner_id::text,''),course_id::text,verdict FROM course_reviews WHERE id=$1 FOR UPDATE`, id).Scan(&t.author, &t.course, &t.verdict)
	case "user":
		t.author = id
	}
	return t, err
}

// ModerateItem applies one audited action to a queue item. Everything below happens in one
// transaction with the audit row, so an action is never half applied.
func (s *Service) ModerateItem(ctx context.Context, actor, itemID string, in ActionInput) (ActionResult, error) {
	var res ActionResult
	if err := s.requireModerator(ctx, actor); err != nil {
		return res, err
	}
	if !validID(itemID) {
		return res, ErrCourseUnavailable
	}
	in.Reason = strings.TrimSpace(in.Reason)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return res, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var kind, id, draftID string
	if err = tx.QueryRow(ctx, `SELECT target_type,target_id::text,COALESCE(draft_id::text,'') FROM moderation_queue_items WHERE id=$1 AND status='open' FOR UPDATE`, itemID).Scan(&kind, &id, &draftID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return res, ErrCourseUnavailable
		}
		return res, err
	}
	if err = ValidateModeration(kind, in, s.cfg); err != nil {
		return res, err
	}
	t, err := s.loadTarget(ctx, tx, kind, id, draftID)
	if errors.Is(err, pgx.ErrNoRows) {
		return res, ErrCourseUnavailable
	}
	if err != nil {
		return res, err
	}
	if t.author == actor {
		return res, ErrSelfModeration
	}
	res = ActionResult{Action: in.Action, Severe: IsSevere(in.Action, in.ReasonCode, s.cfg)}
	if err = tx.QueryRow(ctx, `INSERT INTO moderation_actions(queue_item_id,target_type,target_id,course_id,draft_id,author_id,moderator_id,action,reason_code,reason,severe,listing_was_active,prior_verdict)
 VALUES($1,$2,$3,NULLIF($4,'')::uuid,NULLIF($5,'')::uuid,NULLIF($6,'')::uuid,$7,$8,$9,$10,$11,$12,NULLIF($13,'')) RETURNING id::text`,
		itemID, kind, id, t.course, t.draft, t.author, actor, in.Action, in.ReasonCode, in.Reason, res.Severe, t.listingActive, t.verdict).Scan(&res.ID); err != nil {
		return res, err
	}
	changes, blocked, err := s.apply(ctx, tx, kind, id, t, in, res)
	if err != nil {
		return res, err
	}
	res.CopiesBlocked = blocked
	if _, err = tx.Exec(ctx, `UPDATE moderation_queue_items SET status='resolved',resolved_at=now() WHERE id=$1`, itemID); err != nil {
		return res, err
	}
	if kind != "publication" {
		if _, err = tx.Exec(ctx, `UPDATE content_reports SET status=CASE WHEN $3 THEN 'dismissed' ELSE 'actioned' END,updated_at=now() WHERE target_type=$1 AND target_id=$2 AND status='open'`, kind, id, in.Action == "dismiss"); err != nil {
			return res, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return res, err
	}
	s.afterAction(ctx, actor, t, kind, in, res, changes)
	return res, nil
}

type visibilityChange struct{ course, owner, from, to string }

func (s *Service) afterAction(ctx context.Context, actor string, t target, kind string, in ActionInput, res ActionResult, changes []visibilityChange) {
	if len(changes) > 0 || kind == "review" || (kind == "course" && in.Action != "ban_publishing") || in.Action == "ban_publishing" || kind == "publication" {
		s.invalidateCatalogue(ctx)
	}
	if kind == "review" && t.course != "" {
		s.refreshListingStats(ctx, t.course)
	}
	props := map[string]any{"action_id": res.ID, "action": in.Action, "reason_code": in.ReasonCode, "target_type": kind}
	if t.course != "" {
		props["course_id"] = t.course
	}
	s.emit(actor, "moderation_action", props)
	for _, c := range changes {
		s.emit(c.owner, "course_visibility_changed", map[string]any{"course_id": c.course, "from": c.from, "to": c.to, "cause": "moderation"})
	}
}

// delistCourses marks listings delisted, drops them from the public catalogue state, and returns
// the visibility changes (public listings fall back to link or private).
func (s *Service) delistCourses(ctx context.Context, tx pgx.Tx, ids []string, action string, takedown bool) ([]visibilityChange, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if _, err := tx.Exec(ctx, `UPDATE marketplace_listings SET delisted_at=COALESCE(delisted_at,now()) WHERE course_id=ANY($1::uuid[])`, ids); err != nil {
		return nil, err
	}
	state := "delisted"
	if takedown {
		state = "taken_down"
		if _, err := tx.Exec(ctx, `UPDATE publication_links SET revoked_at=now() WHERE course_id=ANY($1::uuid[]) AND revoked_at IS NULL`, ids); err != nil {
			return nil, err
		}
	}
	rows, err := tx.Query(ctx, `WITH old AS (SELECT id,visibility,learner_id FROM courses WHERE id=ANY($1::uuid[]) FOR UPDATE)
 UPDATE courses c SET moderation_state=CASE WHEN c.moderation_state='taken_down' THEN 'taken_down' ELSE $2 END,moderation_action_id=$3::uuid,
  visibility=CASE WHEN EXISTS (SELECT 1 FROM publication_links WHERE course_id=c.id AND revoked_at IS NULL) THEN 'link'::course_visibility ELSE 'private'::course_visibility END
 FROM old WHERE c.id=old.id RETURNING c.id::text,old.learner_id::text,old.visibility::text,c.visibility::text`, ids, state, action)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []visibilityChange
	for rows.Next() {
		var c visibilityChange
		if err = rows.Scan(&c.course, &c.owner, &c.from, &c.to); err != nil {
			return nil, err
		}
		if c.from != c.to {
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

func (s *Service) ownedListings(ctx context.Context, tx pgx.Tx, owner string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT course_id::text FROM marketplace_listings WHERE owner_id=$1 AND delisted_at IS NULL ORDER BY course_id`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// apply performs the effect of an action. Existing copies are touched only by a severe takedown.
func (s *Service) apply(ctx context.Context, tx pgx.Tx, kind, id string, t target, in ActionInput, res ActionResult) ([]visibilityChange, int, error) {
	var changes []visibilityChange
	blocked := 0
	ban := func() error {
		_, err := tx.Exec(ctx, `INSERT INTO publishing_bans(learner_id,action_id) VALUES($1,$2) ON CONFLICT (learner_id) DO NOTHING`, t.author, res.ID)
		return err
	}
	switch kind {
	case "course":
		switch in.Action {
		case "dismiss", "warn":
			if _, err := tx.Exec(ctx, `UPDATE marketplace_listings SET hidden_at=NULL WHERE course_id=$1`, id); err != nil {
				return nil, 0, err
			}
		case "delist", "takedown":
			var err error
			if changes, err = s.delistCourses(ctx, tx, []string{id}, res.ID, in.Action == "takedown"); err != nil {
				return nil, 0, err
			}
			if res.Severe {
				tag, err := tx.Exec(ctx, `UPDATE course_copies SET blocked_at=now(),blocked_action_id=$2::uuid WHERE source_course_id=$1 AND blocked_at IS NULL`, id, res.ID)
				if err != nil {
					return nil, 0, err
				}
				blocked = int(tag.RowsAffected())
				// Durable before the takedown commits, so a restore of an older dump cannot lift the block.
				if s.ledger != nil {
					if err = s.ledger.RecordBlock(ctx, LedgerRef(id), LedgerRef(res.ID)); err != nil {
						return nil, 0, err
					}
				}
			}
		case "ban_publishing":
			if err := ban(); err != nil {
				return nil, 0, err
			}
			ids, err := s.ownedListings(ctx, tx, t.author)
			if err != nil {
				return nil, 0, err
			}
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
			if changes, err = s.delistCourses(ctx, tx, ids, res.ID, false); err != nil {
				return nil, 0, err
			}
		}
	case "publication":
		switch in.Action {
		case "dismiss":
			if _, err := tx.Exec(ctx, `UPDATE publication_drafts SET verdict='pass' WHERE id=$1`, t.draft); err != nil {
				return nil, 0, err
			}
		default:
			if _, err := tx.Exec(ctx, `UPDATE publication_drafts SET verdict='block' WHERE id=$1`, t.draft); err != nil {
				return nil, 0, err
			}
			if in.Action == "ban_publishing" {
				if err := ban(); err != nil {
					return nil, 0, err
				}
			}
		}
	case "review":
		if in.Action == "delist" || in.Action == "takedown" {
			if _, err := tx.Exec(ctx, `UPDATE course_reviews SET verdict='block' WHERE id=$1`, id); err != nil {
				return nil, 0, err
			}
		}
	case "user":
		if in.Action == "ban_publishing" {
			if err := ban(); err != nil {
				return nil, 0, err
			}
			ids, err := s.ownedListings(ctx, tx, t.author)
			if err != nil {
				return nil, 0, err
			}
			if changes, err = s.delistCourses(ctx, tx, ids, res.ID, false); err != nil {
				return nil, 0, err
			}
		}
	}
	return changes, blocked, nil
}

// Notices lists the actions taken against the author's content, newest first, with their one appeal.
func (s *Service) Notices(ctx context.Context, author string) ([]Notice, error) {
	out := []Notice{}
	if !validID(author) {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT a.id::text,a.action,a.reason_code,a.reason,a.target_type,COALESCE(a.course_id::text,''),COALESCE(c.title,''),a.severe,a.created_at,ap.status,ap.decided_at
 FROM moderation_actions a LEFT JOIN courses c ON c.id=a.course_id LEFT JOIN moderation_appeals ap ON ap.action_id=a.id
 WHERE a.author_id=$1 AND a.action<>'dismiss' ORDER BY a.created_at DESC,a.id LIMIT 100`, author)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var n Notice
		var status *string
		var decided *time.Time
		if err = rows.Scan(&n.ID, &n.Action, &n.ReasonCode, &n.Reason, &n.TargetType, &n.CourseID, &n.Title, &n.Severe, &n.CreatedAt, &status, &decided); err != nil {
			return nil, err
		}
		n.CanAppeal = status == nil
		if status != nil {
			n.Appeal = &AppealState{Status: *status, DecidedAt: decided}
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// FileAppeal records the author's single appeal of an action. The unique key on the action makes
// the rule race-safe: of two simultaneous appeals exactly one is created.
func (s *Service) FileAppeal(ctx context.Context, author, actionID, text string) (string, error) {
	if !validID(author) || !validID(actionID) {
		return "", ErrCourseUnavailable
	}
	text = strings.TrimSpace(text)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var course string
	if err = tx.QueryRow(ctx, `SELECT COALESCE(course_id::text,'') FROM moderation_actions WHERE id=$1 AND author_id=$2 AND action<>'dismiss' FOR SHARE`, actionID, author).Scan(&course); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrCourseUnavailable
		}
		return "", err
	}
	if text == "" || !utf8.ValidString(text) || strings.ContainsRune(text, 0) || utf8.RuneCountInString(text) > s.cfg.AppealMaxChars {
		return "", ErrInvalidModeration
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO moderation_appeals(action_id,author_id,text) VALUES($1,$2,$3) ON CONFLICT (action_id) DO NOTHING RETURNING id::text`, actionID, author, text).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrAppealUsed
	}
	if err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	props := map[string]any{"action_id": actionID, "outcome": "filed"}
	if course != "" {
		props["course_id"] = course
	}
	s.emit(author, "moderation_appeal", props)
	return id, nil
}

// DecideAppeal lets a moderator other than the acting one uphold or overturn an action. Overturning
// reverses what the action did, except the share link, which the owner creates again.
func (s *Service) DecideAppeal(ctx context.Context, actor, appealID, decision, reason string) error {
	if err := s.requireModerator(ctx, actor); err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	if !validID(appealID) || (decision != "uphold" && decision != "overturn") || reason == "" || !utf8.ValidString(reason) ||
		strings.ContainsRune(reason, 0) || utf8.RuneCountInString(reason) > s.cfg.ModerationReasonMaxChars {
		return ErrInvalidModeration
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status, actionID, kind, targetID, course, draft, author, moderator, prior string
	var wasActive, severe bool
	err = tx.QueryRow(ctx, `SELECT ap.status,a.id::text,a.target_type,a.target_id::text,COALESCE(a.course_id::text,''),COALESCE(a.draft_id::text,''),COALESCE(a.author_id::text,''),COALESCE(a.moderator_id::text,''),a.listing_was_active,COALESCE(a.prior_verdict,''),a.severe
 FROM moderation_appeals ap JOIN moderation_actions a ON a.id=ap.action_id WHERE ap.id=$1 FOR UPDATE OF ap`, appealID).
		Scan(&status, &actionID, &kind, &targetID, &course, &draft, &author, &moderator, &wasActive, &prior, &severe)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCourseUnavailable
	}
	if err != nil {
		return err
	}
	if status != "open" {
		return ErrAppealUsed
	}
	if moderator == actor {
		return ErrSameModerator
	}
	if author == actor {
		return ErrSelfModeration
	}
	outcome := "upheld"
	var changes []visibilityChange
	if decision == "overturn" {
		outcome = "overturned"
		if changes, err = s.restore(ctx, tx, actionID, kind, targetID, course, draft, prior, wasActive); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE moderation_appeals SET status=$2,decided_by=$3,decision_reason=$4,decided_at=now() WHERE id=$1`, appealID, outcome, actor, reason); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if decision == "overturn" {
		// After commit: a missing lift marker only means a restore re-blocks, the safe direction.
		if severe && kind == "course" && s.ledger != nil {
			if lerr := s.ledger.LiftBlock(ctx, LedgerRef(actionID)); lerr != nil {
				slog.ErrorContext(ctx, "moderation block lift was not recorded", "action_id", actionID)
			}
		}
		s.invalidateCatalogue(ctx)
		if kind == "review" && course != "" {
			s.refreshListingStats(ctx, course)
		}
	}
	props := map[string]any{"action_id": actionID, "outcome": outcome}
	if course != "" {
		props["course_id"] = course
	}
	s.emit(actor, "moderation_appeal", props)
	for _, c := range changes {
		s.emit(c.owner, "course_visibility_changed", map[string]any{"course_id": c.course, "from": c.from, "to": c.to, "cause": "moderation"})
	}
	return nil
}

func (s *Service) restore(ctx context.Context, tx pgx.Tx, actionID, kind, targetID, course, draft, prior string, wasActive bool) ([]visibilityChange, error) {
	var changes []visibilityChange
	if _, err := tx.Exec(ctx, `DELETE FROM publishing_bans WHERE action_id=$1`, actionID); err != nil {
		return nil, err
	}
	switch kind {
	case "course":
		if _, err := tx.Exec(ctx, `UPDATE course_copies SET blocked_at=NULL,blocked_action_id=NULL WHERE blocked_action_id=$1`, actionID); err != nil {
			return nil, err
		}
		rows, err := tx.Query(ctx, `SELECT id::text,learner_id::text,visibility::text FROM courses WHERE moderation_action_id=$1 FOR UPDATE`, actionID)
		if err != nil {
			return nil, err
		}
		type row struct{ id, owner, vis string }
		var held []row
		for rows.Next() {
			var r row
			if err = rows.Scan(&r.id, &r.owner, &r.vis); err != nil {
				rows.Close()
				return nil, err
			}
			held = append(held, r)
		}
		rows.Close()
		for _, r := range held {
			to := r.vis
			if wasActive && r.id == course {
				to = "public"
				if _, err = tx.Exec(ctx, `UPDATE marketplace_listings SET delisted_at=NULL,hidden_at=NULL WHERE course_id=$1`, r.id); err != nil {
					return nil, err
				}
			}
			if _, err = tx.Exec(ctx, `UPDATE courses SET moderation_state='none',moderation_action_id=NULL,visibility=$2::course_visibility WHERE id=$1`, r.id, to); err != nil {
				return nil, err
			}
			if to != r.vis {
				changes = append(changes, visibilityChange{course: r.id, owner: r.owner, from: r.vis, to: to})
			}
		}
	case "publication":
		if draft != "" {
			if _, err := tx.Exec(ctx, `UPDATE publication_drafts SET verdict='pass' WHERE id=$1`, draft); err != nil {
				return nil, err
			}
		}
	case "review":
		if prior != "" {
			if _, err := tx.Exec(ctx, `UPDATE course_reviews SET verdict=$2 WHERE id=$1`, targetID, prior); err != nil {
				return nil, err
			}
		}
	}
	return changes, nil
}

// purgeModeration deletes the audit log (and, by cascade, its appeals) and resolved queue rows past
// the retention window. The append-only trigger allows deletes only inside this transaction flag.
func (s *Service) purgeModeration(ctx context.Context) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT set_config('duolify.audit_purge','on',true)`); err != nil {
		return 0, err
	}
	var n int64
	for _, q := range []string{
		`DELETE FROM moderation_actions WHERE created_at < now()-make_interval(days => $1)`,
		`DELETE FROM moderation_queue_items WHERE status='resolved' AND opened_at < now()-make_interval(days => $1)`,
	} {
		tag, err := tx.Exec(ctx, q, s.cfg.ModerationRetentionDays)
		if err != nil {
			return 0, err
		}
		n += tag.RowsAffected()
	}
	return n, tx.Commit(ctx)
}
