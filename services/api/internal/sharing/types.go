// Package sharing builds immutable, screened publication drafts (E15-01). Nothing here makes a
// Course visible to anyone but its owner.
package sharing

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"time"
)

// Verdicts. pending_screening is the fail-closed state.
const (
	VerdictPass      = "pass"
	VerdictReview    = "needs_review"
	VerdictBlock     = "block"
	VerdictPending   = "pending_screening"
	PolicyVersion    = "course-v1"
	KindPersonalData = "personal_data"
	KindSecret       = "secret"
)

var (
	// ErrUnavailable is the neutral owner-only refusal: unknown, foreign or deleted.
	ErrUnavailable = errors.New("draft_unavailable")
	// ErrOutOfAllowlist rejects a snapshot carrying any field outside the allowlist.
	ErrOutOfAllowlist = errors.New("snapshot_rejected")
	// ErrAdultRequired means the owner is below the configured publisher age.
	ErrAdultRequired = errors.New("adult_required")
	// ErrVisibilityUnavailable means no public visibility exists yet (E15-01).
	ErrVisibilityUnavailable = errors.New("visibility_unavailable")
)

// Config holds every policy-like value; defaults are conservative (ADR 0012, E15-12 open).
type Config struct {
	MinPublisherAge  int
	IncludeNarration bool
	SampleMaxLessons int
	SampleMaxBytes   int
	MaxInputTokens   uint32
	MaxOutputTokens  uint32
	TimeoutMs        uint32
	MaxAttempts      uint32
	MaxCostNanos     int64
	RetentionDays    int
}

// DefaultConfig is the conservative v1 policy.
func DefaultConfig() Config {
	return Config{MinPublisherAge: 18, SampleMaxLessons: 12, SampleMaxBytes: 120000, MaxInputTokens: 60000, MaxOutputTokens: 300, TimeoutMs: 120000, MaxAttempts: 2, MaxCostNanos: 250_000_000, RetentionDays: 365}
}

// ConfigFromEnv overlays SHARING_* variables on the defaults; invalid values keep the default.
func ConfigFromEnv(getenv func(string) string) Config {
	c := DefaultConfig()
	if getenv == nil {
		getenv = os.Getenv
	}
	if n, err := strconv.Atoi(getenv("SHARING_MIN_PUBLISHER_AGE")); err == nil && n >= 13 && n <= 99 {
		c.MinPublisherAge = n
	}
	if v := getenv("SHARING_INCLUDE_NARRATION"); v == "true" {
		c.IncludeNarration = true
	}
	if n, err := strconv.ParseInt(getenv("SHARING_SCREEN_MAX_COST_NANOS"), 10, 64); err == nil && n > 0 && n <= c.MaxCostNanos {
		c.MaxCostNanos = n
	}
	return c
}

// Rates are per-token USD nanos from the active Pricing config (never hard-coded here).
type Rates struct{ Input, Output, CacheWrite, CacheRead uint64 }

// Finding is a scanner or classifier result: a closed kind and a position, never matched text.
type Finding struct {
	Kind     string `json:"kind"`
	Location string `json:"location"`
}

// Snapshot is the allowlisted, immutable publication content.
type Snapshot struct {
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Units       []SnapshotUnit `json:"units"`
}

// SnapshotUnit is one Course unit and its Lessons.
type SnapshotUnit struct {
	Title   string           `json:"title"`
	Lessons []SnapshotLesson `json:"lessons"`
}

// SnapshotLesson is one Lesson's allowlisted content.
type SnapshotLesson struct {
	Title         string          `json:"title"`
	Goal          string          `json:"goal"`
	Generated     bool            `json:"generated"`
	Slides        []SnapshotSlide `json:"slides,omitempty"`
	Questions     []SnapshotQ     `json:"questions,omitempty"`
	RetryPool     []RetryTopic    `json:"retry_pool,omitempty"`
	NarrationRefs []string        `json:"narration_refs,omitempty"`
}

// SnapshotSlide is one generated Motion HTML Slide.
type SnapshotSlide struct {
	ID        string `json:"id"`
	HTML      string `json:"html"`
	Narration string `json:"narration"`
}

// SnapshotQ is one Lesson test question.
type SnapshotQ struct {
	Topic       string   `json:"topic"`
	Prompt      string   `json:"prompt"`
	Choices     []string `json:"choices"`
	AnswerIndex int      `json:"answer_index"`
	Explanation string   `json:"explanation"`
}

// RetryTopic is the private Retry variant pool for one topic.
type RetryTopic struct {
	Topic    string          `json:"topic"`
	Variants json.RawMessage `json:"variants"`
}

// Draft is what the owner sees. Snapshot text is not returned, only its outline.
type Draft struct {
	ID             string        `json:"id"`
	CourseID       string        `json:"course_id"`
	Number         int           `json:"number"`
	ContentHash    string        `json:"content_hash"`
	Verdict        string        `json:"verdict"`
	ReasonCodes    []string      `json:"reason_codes"`
	Findings       []Finding     `json:"findings"`
	Outline        []OutlineUnit `json:"outline"`
	LessonCount    int           `json:"lesson_count"`
	SampledLessons int           `json:"sampled_lessons"`
	NarrationIncl  bool          `json:"narration_included"`
	CreatedAt      time.Time     `json:"created_at"`
}

// OutlineUnit is the owner-facing outline of a unit.
type OutlineUnit struct {
	Title   string          `json:"title"`
	Lessons []OutlineLesson `json:"lessons"`
}

// OutlineLesson is a Lesson title in the outline.
type OutlineLesson struct {
	Title string `json:"title"`
}
