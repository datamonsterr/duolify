package courses

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
)

// PreparePreview resolves intent and context before the financial boundary. Client
// topic/profile/bounds are never accepted, and quote consent binds one revision.
func (s *Service) PreparePreview(ctx context.Context, owner, revision, quoteID string) (*enginev1.PreviewCourseRequest, error) {
	if s.opts.Pool == nil || revision == "" || quoteID == "" {
		return nil, errors.New("confirmed brief and quote required")
	}
	q, err := pricing.NewQuotes(s.opts.Pool).Get(ctx, owner, quoteID)
	if err != nil || q.Action != "preview" || !q.Accepted || q.Expired || q.Input.BriefRevision != revision {
		return nil, errors.New("fresh accepted brief quote required")
	}
	var latest string
	if s.opts.Pool.QueryRow(ctx, "SELECT id FROM course_brief_revisions WHERE learner_id::text=$1 AND expires_at>now() ORDER BY created_at DESC,id DESC LIMIT 1", owner).Scan(&latest) != nil || latest != revision {
		return nil, errors.New("brief confirmation superseded")
	}
	row, err := db.New(s.opts.Pool).GetCourseBrief(ctx, db.GetCourseBriefParams{ID: revision, LearnerID: owner})
	if err != nil {
		return nil, err
	}
	var brief Brief
	if json.Unmarshal(row.Brief, &brief) != nil || brief.Validate(true) != nil || brief.Hash() != row.ContentHash {
		return nil, errors.New("invalid brief")
	}
	learner := &enginev1.LearnerProfile{LearnerId: owner}
	err = s.opts.Pool.QueryRow(ctx, `SELECT locale,background,goal,level,topic,time_zone,COALESCE(daily_minutes,0),COALESCE(daily_xp,0),COALESCE(reminder_enabled,false),reminder_time FROM learner_profiles WHERE learner_id::text=$1`, owner).Scan(&learner.Locale, &learner.Background, &learner.Goals, &learner.Level, &learner.Topic, &learner.TimeZone, &learner.DailyMinutes, &learner.DailyXp, &learner.ReminderEnabled, &learner.ReminderTime)
	if err != nil {
		return nil, err
	}
	if brief.Level != "" {
		learner.Level = brief.Level
	}
	if brief.Language != "" {
		learner.Locale = brief.Language
	}
	return &enginev1.PreviewCourseRequest{Topic: brief.Topic, Brief: brief.proto(), BriefRevision: revision, BriefHash: row.ContentHash, Learner: learner}, nil
}

var unsafeSlide = regexp.MustCompile(`(?i)(https?:|//|javascript:|data:|<\s*(iframe|object|embed|base|form)\b|\b(fetch|websocket|eventsource|xmlhttprequest|sendbeacon|importscripts)\b|@import|&(?:#|[a-z]+;)|\\)`)
var slideRoot = regexp.MustCompile(`(?i)\bid\s*=\s*["']root["']`)

// ValidatePreview independently checks the delivered contract before charging.
func ValidatePreview(r *enginev1.PreviewCourseResponse) error {
	invalid := errors.New("invalid preview")
	text := func(s string) bool { return utf8.ValidString(s) && strings.TrimSpace(s) != "" && len(s) <= 4000 }
	if r == nil || r.Unavailable || !text(r.GetSampleOutline().GetTitle()) || r.SampleOutline.CourseId != "" || len(r.SampleOutline.Units) == 0 || len(r.SampleOutline.Units) > 10 || len(r.ExampleSlides) == 0 || len(r.ExampleSlides) > 5 {
		return invalid
	}
	for _, unit := range r.SampleOutline.Units {
		if !text(unit.GetTitle()) || len(unit.GetLessons()) == 0 || len(unit.GetLessons()) > 20 {
			return invalid
		}
		for _, lesson := range unit.Lessons {
			if !text(lesson.GetTitle()) || !text(lesson.GetGoal()) {
				return invalid
			}
		}
	}
	for _, slide := range r.ExampleSlides {
		if len(slide.GetHtml()) > 256*1024 || !slideRoot.MatchString(slide.GetHtml()) || unsafeSlide.MatchString(slide.GetHtml()) || !text(slide.GetNarration()) || slide.DurationMs == 0 || slide.DurationMs > 60000 {
			return invalid
		}
	}
	return nil
}

// PreviewEvent uses only public identifiers, lengths and fixed reason codes.
func (s *Service) PreviewEvent(ctx context.Context, owner, event string, request *enginev1.PreviewCourseRequest, previewID string, durationMS int64) {
	if s.opts.Pool == nil || s.opts.Events == nil {
		return
	}
	var optedOut bool
	if s.opts.Pool.QueryRow(ctx, "SELECT analytics_opt_out FROM learner_profiles WHERE learner_id::text=$1", owner).Scan(&optedOut) != nil || optedOut {
		return
	}
	props := map[string]any{"source": "api", "platform": "web", "app_version": "0.1.0", "stage": "preview", "preview_id": previewID, "duration_ms": durationMS}
	if request != nil {
		props["topic_length"] = utf8.RuneCountInString(request.Topic)
		props["has_profile"] = request.Learner != nil && (request.Learner.Background != "" || request.Learner.Goals != "" || request.Learner.Level != "")
	}
	if event == "course_generation_failed" {
		props["error_code"] = "generation_unavailable"
	}
	_ = s.opts.Events.Capture(owner, event, props)
}
