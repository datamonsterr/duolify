package sharing

import (
	"context"
	"errors"
	"time"
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
	errNotImplemented = errors.New("not implemented")
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

func (s *Service) IsModerator(ctx context.Context, actor string) (bool, error) {
	return false, errNotImplemented
}

func (s *Service) ModerationView(ctx context.Context, actor string, limit int) (ModerationView, error) {
	return ModerationView{}, errNotImplemented
}

func (s *Service) ModerateItem(ctx context.Context, actor, itemID string, in ActionInput) (ActionResult, error) {
	return ActionResult{}, errNotImplemented
}

func (s *Service) Notices(ctx context.Context, author string) ([]Notice, error) {
	return nil, errNotImplemented
}

func (s *Service) FileAppeal(ctx context.Context, author, actionID, text string) (string, error) {
	return "", errNotImplemented
}

func (s *Service) DecideAppeal(ctx context.Context, actor, appealID, decision, reason string) error {
	return errNotImplemented
}

// ActionAllowed is the action matrix per target type.
func ActionAllowed(targetType, action string) bool { return false }

// ValidateModeration checks the matrix, the closed reason code and the reason text bounds.
func ValidateModeration(targetType string, in ActionInput, cfg Config) error {
	return errNotImplemented
}

// IsSevere is true for a takedown whose reason code is configured as severe.
func IsSevere(action, code string, cfg Config) bool { return false }

// DueAt is the first-response deadline of a queue item.
func DueAt(opened time.Time, cfg Config) time.Time { return opened }

// IsOverdue reports an item still open past its first-response target.
func IsOverdue(opened, now time.Time, cfg Config) bool { return false }

// HeldSeverity is the queue severity of a held publication verdict.
func HeldSeverity(verdict string, cfg Config) int { return 0 }
