package sharing

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrInvalidReport rejects unknown reasons, targets, malformed ids and over-long or invalid text.
	ErrInvalidReport = errors.New("invalid_report")
	// ErrSelfReport refuses reporting yourself or your own content.
	ErrSelfReport = errors.New("self_report")
)

// ReportInput names the target; a user target is a public username, so no learner id is exposed.
type ReportInput struct{ TargetType, TargetID, Reason, Text string }

// ReportResult tells only the reporter whether the report merged into an earlier open one.
type ReportResult struct {
	Merged bool `json:"merged"`
}

// QueueItem is the moderation read model: severity first, then age.
type QueueItem struct {
	ID          string
	TargetType  string
	TargetID    string
	Severity    int
	ReportCount int
	OpenedAt    time.Time
}

// Report is implemented in the next step.
func (s *Service) Report(context.Context, string, ReportInput) (ReportResult, error) {
	return ReportResult{}, ErrCourseUnavailable
}

// ModerationQueue lists open items by severity then age.
func (s *Service) ModerationQueue(context.Context, int) ([]QueueItem, error) { return nil, nil }

// ValidateReport is implemented in the next step.
func ValidateReport(ReportInput, Config) error { return nil }

// QueueSeverity is implemented in the next step.
func QueueSeverity([]string, Config) int { return 0 }

// ShouldHide is implemented in the next step.
func ShouldHide(int, Config) bool { return false }
