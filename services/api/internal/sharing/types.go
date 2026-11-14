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
	// ErrPublishNotAllowed is returned when the latest draft verdict is not pass.
	ErrPublishNotAllowed = errors.New("publish_not_allowed")
	// ErrLinkNotFound is the neutral 404 for an invalid, revoked, or block-hidden token.
	ErrLinkNotFound = errors.New("link_not_found")
)

// Config holds every policy-like value; defaults are conservative (ADR 0012, E15-12 open).
type Config struct {
	MinPublisherAge int
	// MaxEnrollmentsPerDay caps new Course copies per account per rolling day (E15-03).
	MaxEnrollmentsPerDay int
	// CopySlideMs is the playback time given to a reused slide that carries no duration.
	CopySlideMs      int
	IncludeNarration bool
	SampleMaxLessons int
	SampleMaxBytes   int
	MaxInputTokens   uint32
	MaxOutputTokens  uint32
	TimeoutMs        uint32
	MaxAttempts      uint32
	MaxCostNanos     int64
	RetentionDays    int
	// E15-02: public preview domain and deep-link scheme.
	PublicDomain             string
	AppScheme                string
	PreviewRateLimitPerIP    int
	PreviewRateWindowSeconds int
	// TrustedProxyHops is how many reverse proxies append to X-Forwarded-For (shared with auth).
	TrustedProxyHops int
	// E15-07 Marketplace. The release flag is OFF until moderation and takedown ship and legal
	// approves (ADR 0012); every cap below is configuration.
	MarketplaceEnabled             bool
	MarketplaceMinAccountAgeDays   int
	MarketplaceMinLessons          int
	MarketplaceMaxTags             int
	MarketplaceMaxListingsPerOwner int
	MarketplacePageSize            int
	MarketplaceMaxPageSize         int
	MarketplaceMaxQueryLength      int
	// MarketplaceRatingMinCount is the Bayesian minimum count C; the prior is the mean used
	// when no listed course has a rating yet.
	MarketplaceRatingMinCount  int
	MarketplaceRatingPrior     float64
	MarketplaceCacheTTLSeconds int
	// E15-06 Course invites: sends per sender per rolling day, days until a pending invite expires,
	// days a final invite is kept, and push attempts before an adapter failure is final.
	InviteSendsPerDay int
	InviteExpiryDays  int
	InvitePurgeDays   int
	PushMaxAttempts   int
	// E15-05 Republish: versions per Course per rolling day and the owner changelog length.
	MaxRepublishesPerDay int
	MaxChangelogLen      int
}

// DefaultConfig is the conservative v1 policy.
func DefaultConfig() Config {
	return Config{
		MinPublisherAge: 18, MaxEnrollmentsPerDay: 30, CopySlideMs: 8000, SampleMaxLessons: 12, SampleMaxBytes: 120000,
		MaxInputTokens: 60000, MaxOutputTokens: 300, TimeoutMs: 120000, MaxAttempts: 2,
		MaxCostNanos: 250_000_000, RetentionDays: 365,
		PublicDomain: "localhost", AppScheme: "duolify",
		PreviewRateLimitPerIP: 60, PreviewRateWindowSeconds: 60,
		MarketplaceMinAccountAgeDays: 3, MarketplaceMinLessons: 3, MarketplaceMaxTags: 5, MarketplaceMaxListingsPerOwner: 20,
		MarketplacePageSize: 20, MarketplaceMaxPageSize: 50, MarketplaceMaxQueryLength: 100,
		MarketplaceRatingMinCount: 10, MarketplaceRatingPrior: 3.5, MarketplaceCacheTTLSeconds: 60,
		InviteSendsPerDay: 20, InviteExpiryDays: 30, InvitePurgeDays: 90, PushMaxAttempts: 3,
		MaxRepublishesPerDay: 3, MaxChangelogLen: 500,
	}
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
	if n, err := strconv.Atoi(getenv("SHARING_MAX_ENROLLMENTS_PER_DAY")); err == nil && n > 0 {
		c.MaxEnrollmentsPerDay = n
	}
	if n, err := strconv.Atoi(getenv("SHARING_COPY_SLIDE_MS")); err == nil && n >= 1000 && n <= 120000 {
		c.CopySlideMs = n
	}
	if v := getenv("SHARING_INCLUDE_NARRATION"); v == "true" {
		c.IncludeNarration = true
	}
	if n, err := strconv.ParseInt(getenv("SHARING_SCREEN_MAX_COST_NANOS"), 10, 64); err == nil && n > 0 && n <= c.MaxCostNanos {
		c.MaxCostNanos = n
	}
	if v := getenv("SHARING_PUBLIC_DOMAIN"); v != "" {
		c.PublicDomain = v
	}
	if v := getenv("SHARING_APP_SCHEME"); v != "" {
		c.AppScheme = v
	}
	if n, err := strconv.Atoi(getenv("SHARING_PREVIEW_RATE_LIMIT_PER_IP")); err == nil && n > 0 {
		c.PreviewRateLimitPerIP = n
	}
	if n, err := strconv.Atoi(getenv("SHARING_PREVIEW_RATE_WINDOW_SECONDS")); err == nil && n > 0 {
		c.PreviewRateWindowSeconds = n
	}
	if n, err := strconv.Atoi(getenv("AUTH_TRUSTED_PROXY_HOPS")); err == nil && n >= 0 && n <= 32 {
		c.TrustedProxyHops = n
	}
	// Only the exact word "true" turns the Marketplace on.
	c.MarketplaceEnabled = getenv("MARKETPLACE_ENABLED") == "true"
	positive := func(key string, dst *int, lo, hi int) {
		if n, err := strconv.Atoi(getenv(key)); err == nil && n >= lo && n <= hi {
			*dst = n
		}
	}
	positive("MARKETPLACE_MIN_ACCOUNT_AGE_DAYS", &c.MarketplaceMinAccountAgeDays, 0, 3650)
	positive("MARKETPLACE_MIN_LESSONS", &c.MarketplaceMinLessons, 1, 1000)
	positive("MARKETPLACE_MAX_TAGS", &c.MarketplaceMaxTags, 0, 10)
	positive("MARKETPLACE_MAX_LISTINGS_PER_OWNER", &c.MarketplaceMaxListingsPerOwner, 1, 1000)
	positive("MARKETPLACE_MAX_PAGE_SIZE", &c.MarketplaceMaxPageSize, 1, 100)
	positive("MARKETPLACE_PAGE_SIZE", &c.MarketplacePageSize, 1, 100)
	positive("MARKETPLACE_MAX_QUERY_LENGTH", &c.MarketplaceMaxQueryLength, 1, 200)
	positive("MARKETPLACE_RATING_MIN_COUNT", &c.MarketplaceRatingMinCount, 1, 10000)
	positive("MARKETPLACE_CACHE_TTL_SECONDS", &c.MarketplaceCacheTTLSeconds, 1, 3600)
	positive("SHARING_INVITES_PER_DAY", &c.InviteSendsPerDay, 1, 1000)
	positive("SHARING_INVITE_EXPIRY_DAYS", &c.InviteExpiryDays, 1, 365)
	positive("SHARING_INVITE_PURGE_DAYS", &c.InvitePurgeDays, 1, 3650)
	positive("SHARING_PUSH_MAX_ATTEMPTS", &c.PushMaxAttempts, 1, 10)
	positive("SHARING_MAX_REPUBLISHES_PER_DAY", &c.MaxRepublishesPerDay, 1, 100)
	positive("SHARING_MAX_CHANGELOG_LENGTH", &c.MaxChangelogLen, 1, 1000)
	if f, err := strconv.ParseFloat(getenv("MARKETPLACE_RATING_PRIOR"), 64); err == nil && f >= 1 && f <= 5 {
		c.MarketplaceRatingPrior = f
	}
	if c.MarketplacePageSize > c.MarketplaceMaxPageSize {
		c.MarketplacePageSize = c.MarketplaceMaxPageSize
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
	// Key is the stable lesson key (E15-05): it survives retitles and reordering.
	Key           string          `json:"key,omitempty"`
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
	Title     string `json:"title"`
	Generated bool   `json:"generated"`
}

// CreatorInfo is the allowlisted public identity of a Course owner.
type CreatorInfo struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url,omitempty"`
}

// PublicSlide is the allowlisted HTML only slide returned on a public preview.
// No narration, no questions.
type PublicSlide struct {
	ID   string `json:"id"`
	HTML string `json:"html"`
}

// PublicOutlineUnit is the preview outline: title + lesson titles, no content.
type PublicOutlineUnit struct {
	Title   string                `json:"title"`
	Lessons []PublicOutlineLesson `json:"lessons"`
}

// PublicOutlineLesson is a lesson entry in the public outline.
type PublicOutlineLesson struct {
	Title     string `json:"title"`
	Generated bool   `json:"generated"`
}

// PreviewPayload is the allowlisted public preview of a published Course.
// NO questions, answer_index, explanation, retry_pool, narration_refs or narration audio.
type PreviewPayload struct {
	CourseID             string              `json:"course_id"`
	Title                string              `json:"title"`
	Description          string              `json:"description"`
	Version              int                 `json:"version"`
	LessonCount          int                 `json:"lesson_count"`
	GeneratedLessonCount int                 `json:"generated_lesson_count"`
	Creator              CreatorInfo         `json:"creator"`
	Outline              []PublicOutlineUnit `json:"outline"`
	SampleSlides         []PublicSlide       `json:"sample_slides"`
	EnrollmentCount      int                 `json:"enrollment_count"`
}

// BuildPreviewPayload constructs a PreviewPayload from a Snapshot + Draft + creator identity.
// It enforces the preview allowlist at the type level: forbidden fields simply do not exist.
func BuildPreviewPayload(snap Snapshot, draft Draft, creator CreatorInfo, enrollmentCount int) PreviewPayload {
	var generatedCount int
	var sample []PublicSlide
	outline := make([]PublicOutlineUnit, 0, len(snap.Units))
	for _, u := range snap.Units {
		ou := PublicOutlineUnit{Title: u.Title, Lessons: make([]PublicOutlineLesson, 0, len(u.Lessons))}
		for _, l := range u.Lessons {
			ou.Lessons = append(ou.Lessons, PublicOutlineLesson{Title: l.Title, Generated: l.Generated})
			if l.Generated {
				generatedCount++
				// Collect up to 3 sample slides from the first generated lesson that has slides.
				if len(sample) < 3 && len(l.Slides) > 0 {
					for _, sl := range l.Slides {
						if len(sample) >= 3 {
							break
						}
						sample = append(sample, PublicSlide{ID: sl.ID, HTML: sl.HTML})
					}
				}
			}
		}
		outline = append(outline, ou)
	}
	return PreviewPayload{
		CourseID:             draft.CourseID,
		Title:                snap.Title,
		Description:          snap.Description,
		Version:              draft.Number,
		LessonCount:          draft.LessonCount,
		GeneratedLessonCount: generatedCount,
		Creator:              creator,
		Outline:              outline,
		SampleSlides:         sample,
		EnrollmentCount:      enrollmentCount,
	}
}
