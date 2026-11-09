package accounts

import (
	"context"
	"encoding/json"
	"net/http"
	"unicode/utf8"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
)

// SetupChatEngine abstracts the engine SetupChat method.
type SetupChatEngine interface {
	SetupChat(ctx context.Context, in *enginev1.SetupChatRequest) (*enginev1.SetupChatResponse, error)
}

// PricingCatalogue provides immutable pricing action checks.
type PricingCatalogue interface {
	HasAction(name string) bool
}

// SetupChatOptions wires engine, pricing and analytics.
type SetupChatOptions struct {
	Engine  SetupChatEngine
	Pricing PricingCatalogue
	Events  analytics.Events
}

// ConfigureSetupChat enables guided onboarding setup chat.
func (s *Service) ConfigureSetupChat(opts SetupChatOptions) {
	s.setupChat = opts
}

func (s *Service) handleSetupChat(w http.ResponseWriter, r *http.Request) {
	if s.setupChat.Engine == nil || s.setupChat.Pricing == nil {
		respond(w, http.StatusServiceUnavailable, map[string]string{"error": "Setup chat is currently unavailable. Use the form instead."})
		return
	}
	if !s.setupChat.Pricing.HasAction("setup_chat") {
		respond(w, http.StatusServiceUnavailable, map[string]string{"error": "Setup chat is currently disabled. Use the form instead."})
		return
	}

	var in struct {
		CurrentStep string          `json:"current_step"`
		Answer      string          `json:"answer"`
		Skipped     bool            `json:"skipped"`
		IsFollowUp  bool            `json:"is_follow_up"`
		Draft       *LearnerContext `json:"draft"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	textLimit := s.profile.TextLimit
	if textLimit <= 0 {
		textLimit = 2000
	}
	if err := decoder.Decode(&in); err != nil || (!in.Skipped && utf8.RuneCountInString(in.Answer) > textLimit) {
		respond(w, http.StatusBadRequest, map[string]string{"error": "Invalid setup chat answer or length limit exceeded."})
		return
	}

	validSteps := map[string]bool{
		"goal": true, "level": true, "daily_time": true, "reminders": true, "time_zone": true, "confirm": true, "": true,
	}
	if !validSteps[in.CurrentStep] {
		respond(w, http.StatusBadRequest, map[string]string{"error": "Invalid setup chat step."})
		return
	}

	ctx := r.Context()
	var current LearnerContext
	if s.pool != nil {
		raw, err := db.New(s.pool).GetLearnerContext(ctx, contextID(r))
		if err == nil {
			_ = json.Unmarshal(raw, &current)
		}
	}
	if in.Draft != nil {
		if in.Draft.Goal != "" {
			current.Goal = in.Draft.Goal
		}
		if in.Draft.Topic != "" {
			current.Topic = in.Draft.Topic
		}
		if in.Draft.Background != "" {
			current.Background = in.Draft.Background
		}
		if in.Draft.Level != "" {
			current.Level = in.Draft.Level
		}
		if in.Draft.DailyMinutes != nil {
			current.DailyMinutes = in.Draft.DailyMinutes
		}
		if in.Draft.DailyXP != nil {
			current.DailyXP = in.Draft.DailyXP
		}
		if in.Draft.ReminderEnabled != nil {
			current.ReminderEnabled = in.Draft.ReminderEnabled
		}
		if in.Draft.ReminderTime != "" {
			current.ReminderTime = in.Draft.ReminderTime
		}
		if in.Draft.TimeZone != "" {
			current.TimeZone = in.Draft.TimeZone
		}
		if in.Draft.Locale != "" {
			current.Locale = in.Draft.Locale
		}
	}

	var dailyMin uint32
	if current.DailyMinutes != nil && *current.DailyMinutes > 0 && *current.DailyMinutes <= 1440 {
		dailyMin = uint32(*current.DailyMinutes)
	}
	var dailyXP uint32
	if current.DailyXP != nil && *current.DailyXP > 0 && *current.DailyXP <= 100000 {
		dailyXP = uint32(*current.DailyXP)
	}
	var reminderEnabled bool
	if current.ReminderEnabled != nil {
		reminderEnabled = *current.ReminderEnabled
	}

	req := &enginev1.SetupChatRequest{
		Profile: &enginev1.LearnerProfile{
			LearnerId:       contextID(r).String(),
			Locale:          current.Locale,
			Background:      current.Background,
			Goals:           current.Goal,
			Level:           current.Level,
			Topic:           current.Topic,
			DailyMinutes:    dailyMin,
			DailyXp:         dailyXP,
			ReminderEnabled: reminderEnabled,
			ReminderTime:    current.ReminderTime,
			TimeZone:        current.TimeZone,
		},
		CurrentStep: in.CurrentStep,
		Answer:      in.Answer,
		Skipped:     in.Skipped,
		IsFollowUp:  in.IsFollowUp,
	}

	resp, err := s.setupChat.Engine.SetupChat(ctx, req)
	if err != nil {
		respond(w, http.StatusServiceUnavailable, map[string]string{"error": "The setup chat isn't responding. Your answers so far are kept in the form."})
		return
	}

	// PostHog analytics: $ai_generation without prompt/answer content
	var optOut bool
	learnerID, _ := r.Context().Value(learnerKey{}).(string)
	if s.pool != nil {
		_ = s.pool.QueryRow(ctx, "SELECT analytics_opt_out FROM learner_profiles WHERE learner_id=$1", contextID(r)).Scan(&optOut)
	}
	if !optOut && s.setupChat.Events != nil && learnerID != "" {
		_ = s.setupChat.Events.Capture(learnerID, "$ai_generation", map[string]any{
			"provider":      resp.GetUsage().GetProvider(),
			"model":         resp.GetUsage().GetModel(),
			"input_tokens":  resp.GetUsage().GetInputTokens(),
			"output_tokens": resp.GetUsage().GetOutputTokens(),
			"latency_ms":    resp.GetUsage().GetLatencyMs(),
			"task":          "setup_chat",
			"fallback_used": false,
			"source":        "api",
			"platform":      "web",
			"app_version":   "0.1.0",
		})
	}

	quickReplies := make([]map[string]string, 0, len(resp.GetQuickReplies()))
	for _, q := range resp.GetQuickReplies() {
		quickReplies = append(quickReplies, map[string]string{
			"id":    q.GetId(),
			"label": q.GetLabel(),
		})
	}

	updated := resp.GetUpdatedProfile()
	updatedMap := map[string]any{
		"topic":            updated.GetTopic(),
		"goal":             updated.GetGoals(),
		"level":            updated.GetLevel(),
		"background":       updated.GetBackground(),
		"daily_minutes":    nil,
		"daily_xp":         nil,
		"reminder_enabled": nil,
		"reminder_time":    updated.GetReminderTime(),
		"time_zone":        updated.GetTimeZone(),
		"locale":           updated.GetLocale(),
	}
	if updated.GetDailyMinutes() > 0 {
		dm := int(updated.GetDailyMinutes())
		updatedMap["daily_minutes"] = dm
	}
	if updated.GetDailyXp() > 0 {
		dx := int(updated.GetDailyXp())
		updatedMap["daily_xp"] = dx
	}
	if updated.GetReminderTime() != "" || updated.GetReminderEnabled() {
		re := updated.GetReminderEnabled()
		updatedMap["reminder_enabled"] = re
	}

	respond(w, http.StatusOK, map[string]any{
		"message":         resp.GetMessage(),
		"quick_replies":   quickReplies,
		"updated_profile": updatedMap,
		"next_step":       resp.GetNextStep(),
		"is_follow_up":    resp.GetIsFollowUp(),
		"is_complete":     resp.GetIsComplete(),
	})
}
