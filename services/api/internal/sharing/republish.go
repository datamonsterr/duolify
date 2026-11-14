package sharing

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// E15-05 skeleton: signatures only; behaviour follows the failing tests.

var (
	// ErrNotPublished means the Course has no live passing version to republish over.
	ErrNotPublished = errors.New("not_published")
	// ErrRepublishLimit means the Course used its republishes for the rolling day.
	ErrRepublishLimit = errors.New("republish_limit")
	// ErrUpdateUnavailable is the one neutral refusal for a copy update: unknown, foreign, forked,
	// already current or stale.
	ErrUpdateUnavailable = errors.New("update_unavailable")
	// ErrChangelogTooLong rejects a changelog above the configured length.
	ErrChangelogTooLong = errors.New("changelog_too_long")
)

// Version is one numbered publication in the owner's version list.
type Version struct {
	Number      int       `json:"number"`
	Verdict     string    `json:"verdict"`
	Live        bool      `json:"live"`
	LessonCount int       `json:"lesson_count"`
	Changelog   string    `json:"changelog"`
	CreatedAt   time.Time `json:"created_at"`
}

// VersionList is the owner's versions with the remaining republish budget.
type VersionList struct {
	Versions             []Version `json:"versions"`
	RepublishesRemaining int       `json:"republishes_remaining"`
	Limit                int       `json:"limit"`
}

// ChangeSummary counts what differs between two versions, matched by lesson key.
type ChangeSummary struct {
	Added    int `json:"added"`
	Removed  int `json:"removed"`
	Retitled int `json:"retitled"`
}

// ChangelogEntry is what a learner sees for one newer version.
type ChangelogEntry struct {
	Version int    `json:"version"`
	Text    string `json:"text"`
	ChangeSummary
}

// UpdateStatus tells a copy's learner whether an update exists.
type UpdateStatus struct {
	UpdateAvailable bool             `json:"update_available"`
	Forked          bool             `json:"forked"`
	CurrentVersion  int              `json:"current_version"`
	LatestVersion   int              `json:"latest_version"`
	LockVersion     int              `json:"lock_version"`
	Changelog       []ChangelogEntry `json:"changelog"`
	Completed       int              `json:"completed_lessons"`
	ArchivedCount   int              `json:"archived_count"`
}

// UpdateResult counts what an update did.
type UpdateResult struct {
	Version     int `json:"version"`
	LockVersion int `json:"lock_version"`
	Kept        int `json:"kept"`
	Added       int `json:"added"`
	Archived    int `json:"archived"`
}

// ArchivedLesson is a lesson removed upstream that stays in the learner's history.
type ArchivedLesson struct {
	Title      string    `json:"title"`
	ArchivedAt time.Time `json:"archived_at"`
	Version    int       `json:"version"`
	Completed  bool      `json:"completed"`
}

// HeldLesson is one live lesson of a copy as the planner sees it.
type HeldLesson struct {
	StubID, Key, Title, Status  string
	Reused, HasContent, HasTest bool
}

// Planned actions for a lesson in the new layout.
const (
	ActionKeep    = "keep"
	ActionReplace = "replace"
	ActionAdd     = "add"
)

// PlannedLesson is one lesson of the copy after the update.
type PlannedLesson struct {
	Key, Title, Goal, StubID, Action, Status string
	Source                                   SnapshotLesson
}

// PlannedUnit is one unit after the update.
type PlannedUnit struct {
	Title   string
	Lessons []PlannedLesson
}

// UpdatePlan is the pure description of an update.
type UpdatePlan struct {
	Units                 []PlannedUnit
	Archive               []string
	Kept, Added, Archived int
}

// LessonKey is the deterministic stable key of an owner lesson stub.
func LessonKey(stubID string) string { return "" }

// KeyOrLegacy returns a snapshot lesson's key, or a position-derived one for pre-E15-05 versions.
func KeyOrLegacy(l SnapshotLesson, unit, lesson int) string { return "" }

// DiffSnapshots counts additions, removals and retitles between two versions by lesson key.
func DiffSnapshots(oldSnap, newSnap Snapshot) ChangeSummary { return ChangeSummary{} }

// PlanUpdate maps a copy's lessons onto the new version by key.
func PlanUpdate(have []HeldLesson, next Snapshot, origin string) UpdatePlan { return UpdatePlan{} }

// CleanChangelog trims the owner text and rejects one above max characters.
func CleanChangelog(text string, max int) (string, error) { return "", ErrChangelogTooLong }

func (s *Service) Republish(ctx context.Context, owner, courseID, changelog string) (Draft, error) {
	return Draft{}, ErrNotPublished
}

func (s *Service) Versions(ctx context.Context, owner, courseID string) (VersionList, error) {
	return VersionList{}, ErrUnavailable
}

func (s *Service) UpdateStatus(ctx context.Context, learner, copyID string) (UpdateStatus, error) {
	return UpdateStatus{}, ErrUpdateUnavailable
}

func (s *Service) ApplyUpdate(ctx context.Context, learner, copyID string, lockVersion, toVersion int) (UpdateResult, error) {
	return UpdateResult{}, ErrUpdateUnavailable
}

func (s *Service) ArchivedLessons(ctx context.Context, learner, copyID string) ([]ArchivedLesson, error) {
	return nil, ErrUpdateUnavailable
}

// RetainedObjectPrefixes lists published-object prefixes still pinned by a copy or referenced by a copy lesson.
func (s *Service) RetainedObjectPrefixes(ctx context.Context) ([]string, error) { return nil, nil }

// Updater is what the update routes need from the service.
type Updater interface {
	Republish(ctx context.Context, owner, courseID, changelog string) (Draft, error)
	Versions(ctx context.Context, owner, courseID string) (VersionList, error)
	UpdateStatus(ctx context.Context, learner, copyID string) (UpdateStatus, error)
	ApplyUpdate(ctx context.Context, learner, copyID string, lockVersion, toVersion int) (UpdateResult, error)
	ArchivedLessons(ctx context.Context, learner, copyID string) ([]ArchivedLesson, error)
}

// UpdateHandler serves the republish and copy update routes.
type UpdateHandler struct{ u Updater }

// NewUpdateHandler wires the routes.
func NewUpdateHandler(u Updater) *UpdateHandler { return &UpdateHandler{u: u} }

func (h *UpdateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }

// UpdatePatterns are the routes the update handler serves.
var UpdatePatterns = []string{
	"POST /v1/courses/{id}/republish", "GET /v1/courses/{id}/versions",
	"GET /v1/courses/{id}/update", "POST /v1/courses/{id}/update", "GET /v1/courses/{id}/archived-lessons",
}
