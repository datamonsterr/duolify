package accounts

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/jackc/pgx/v5/pgtype"
)

// ProfileOptions configures current context. Configure before serving requests.
type ProfileOptions struct {
	TextLimit int
	Events    analytics.Events
}

// ProfileOptionsFromEnv validates limits before the server starts.
func ProfileOptionsFromEnv(getenv func(string) string) (ProfileOptions, error) {
	limit := 2000
	if raw := getenv("PROFILE_TEXT_LIMIT"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 16384 {
			return ProfileOptions{}, errors.New("PROFILE_TEXT_LIMIT must be between 1 and 16384")
		}
	}
	return ProfileOptions{TextLimit: limit}, nil
}

// ConfigureProfile sets the profile policy and the existing analytics transport.
func (s *Service) ConfigureProfile(options ProfileOptions) {
	if options.TextLimit < 1 || options.TextLimit > 16384 {
		options.TextLimit = 2000
	}
	s.profile = options
}

// LearnerContext contains only current optional context, never authentication data.
type LearnerContext struct {
	LearnerID       string `json:"learner_id"`
	Goal            string `json:"goal"`
	Topic           string `json:"topic"`
	Background      string `json:"background"`
	Level           string `json:"level"`
	Locale          string `json:"locale"`
	TimeZone        string `json:"time_zone"`
	DailyMinutes    *int   `json:"daily_minutes"`
	DailyXP         *int   `json:"daily_xp"`
	ReminderEnabled *bool  `json:"reminder_enabled"`
	ReminderTime    string `json:"reminder_time"`
	SetupCompleted  bool   `json:"setup_completed"`
	TextLimit       int    `json:"text_limit"`
}

var reminderPattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// ValidateContextPatch returns values and field errors without reflecting learner text.
// Omitted fields are preserved; null is permitted only for optional numbers/boolean.
func ValidateContextPatch(fields map[string]json.RawMessage, limit int) (map[string]any, map[string]string) {
	values := map[string]any{}
	failures := map[string]string{}
	for name, raw := range fields {
		invalid := false
		switch name {
		case "goal", "topic", "background", "level", "locale", "time_zone", "reminder_time":
			var value string
			if string(raw) == "null" || json.Unmarshal(raw, &value) != nil {
				invalid = true
				break
			}
			switch name {
			case "goal", "topic", "background":
				invalid = !utf8.ValidString(value) || utf8.RuneCountInString(value) > limit
			case "level":
				invalid = value != "" && value != "beginner" && value != "intermediate" && value != "advanced" && value != "expert"
			case "locale":
				invalid = value != "" && (len(value) > 64 || !localePattern.MatchString(value))
			case "time_zone":
				if value != "" {
					_, err := time.LoadLocation(value)
					invalid = err != nil || value == "Local" || len(value) > 64
				}
			case "reminder_time":
				invalid = value != "" && !reminderPattern.MatchString(value)
			}
			values[name] = value
		case "daily_minutes", "daily_xp":
			var value *int
			invalid = json.Unmarshal(raw, &value) != nil
			maxValue := 1440
			if name == "daily_xp" {
				maxValue = 10000
			}
			if value != nil && (*value < 1 || *value > maxValue) {
				invalid = true
			}
			values[name] = value
		case "reminder_enabled":
			var value *bool
			invalid = json.Unmarshal(raw, &value) != nil
			values[name] = value
		case "narration", "captions", "ui_sounds":
			var value bool
			invalid = json.Unmarshal(raw, &value) != nil
			values[name] = value
		case "appearance":
			var value string
			invalid = json.Unmarshal(raw, &value) != nil || (value != "system" && value != "light" && value != "dark")
			values[name] = value
		case "motion":
			var value string
			invalid = json.Unmarshal(raw, &value) != nil || (value != "system" && value != "reduce")
			values[name] = value
		case "haptics":
			var value string
			invalid = json.Unmarshal(raw, &value) != nil || (value != "system" && value != "on" && value != "off")
			values[name] = value
		case "narration_speed", "text_size":
			var value float64
			invalid = json.Unmarshal(raw, &value) != nil
			if name == "narration_speed" {
				invalid = invalid || value < 0.5 || value > 2.0
			}
			if name == "text_size" {
				invalid = invalid || value < 0.8 || value > 1.5
			}
			values[name] = value
		default:
			invalid = true
		}
		if invalid {
			failures[name] = "Enter a valid value within the configured limits."
		}
	}
	return values, failures
}
func (s *Service) registerProfile() {
	s.mux.Handle("GET /v1/me/profile", s.Protect(http.HandlerFunc(s.readContext)))
	s.mux.Handle("GET /v1/me/profile/export", s.Protect(http.HandlerFunc(s.readContext)))
	s.mux.Handle("POST /v1/me/profile", s.Protect(http.HandlerFunc(s.saveContext)))
	s.mux.Handle("GET /v1/me/settings", s.Protect(http.HandlerFunc(s.readSettings)))
	s.mux.Handle("POST /v1/me/settings", s.Protect(http.HandlerFunc(s.saveSettings)))
	s.mux.Handle("POST /v1/me/setup-chat", s.Protect(http.HandlerFunc(s.handleSetupChat)))
}
func contextID(r *http.Request) pgtype.UUID {
	var id pgtype.UUID
	// Protect already validates the authenticated UUID.
	_ = id.Scan(r.Context().Value(learnerKey{}).(string))
	return id
}
func (s *Service) readContext(w http.ResponseWriter, r *http.Request) {
	raw, err := db.New(s.pool).GetLearnerContext(r.Context(), contextID(r))
	var value LearnerContext
	if err == nil {
		err = json.Unmarshal(raw, &value)
	}
	if err != nil {
		respond(w, 503, map[string]string{"error": "Could not read your profile. Please retry."})
		return
	}
	value.TextLimit = s.profile.TextLimit
	if r.URL.Path == "/v1/me/profile/export" {
		w.Header().Set("Content-Disposition", `attachment; filename="learner-profile.json"`)
	}
	respond(w, 200, value)
}
func (s *Service) saveContext(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Confirmed bool                       `json:"confirmed"`
		Action    string                     `json:"action"`
		Fields    map[string]json.RawMessage `json:"fields"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&in)
	var extra any
	if err != nil || !errors.Is(decoder.Decode(&extra), io.EOF) || !in.Confirmed || (in.Action != "save" && in.Action != "skip") {
		respond(w, 400, map[string]string{"error": "Explicit profile confirmation or Skip is required."})
		return
	}
	for name := range in.Fields {
		if !profileContextField(name) {
			respond(w, 400, map[string]string{"error": "Check the highlighted fields."})
			return
		}
	}
	patch, failures := ValidateContextPatch(in.Fields, s.profile.TextLimit)
	if in.Action == "save" && len(failures) > 0 {
		respond(w, 400, map[string]any{"error": "Check the highlighted fields.", "fields": failures})
		return
	}
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		respond(w, 503, map[string]string{"error": "Could not save profile. Please retry."})
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	raw, err := queries.GetLearnerContext(ctx, contextID(r))
	var before LearnerContext
	if err == nil {
		err = json.Unmarshal(raw, &before)
	}
	var current map[string]any
	if err == nil {
		err = json.Unmarshal(raw, &current)
	}
	var changed []string
	if err == nil && in.Action == "save" {
		// Normalize numbers/pointers through JSON before effective-change comparison.
		encoded, marshalErr := json.Marshal(patch)
		err = marshalErr
		var normalized map[string]any
		if err == nil {
			err = json.Unmarshal(encoded, &normalized)
		}
		for name, value := range normalized {
			if !reflect.DeepEqual(current[name], value) {
				changed = append(changed, name)
			}
			current[name] = value
		}
	}
	if err == nil {
		current["setup_completed"] = true
	}
	var updated []byte
	if err == nil {
		updated, err = json.Marshal(current)
	}
	if err == nil {
		err = queries.SaveLearnerContext(ctx, db.SaveLearnerContextParams{Context: updated, LearnerID: contextID(r)})
	}
	var optOut bool
	if err == nil {
		err = tx.QueryRow(ctx, "SELECT analytics_opt_out FROM learner_profiles WHERE learner_id=$1", contextID(r)).Scan(&optOut)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		respond(w, 503, map[string]string{"error": "Could not save profile. Please retry."})
		return
	}
	sort.Strings(changed)
	if !optOut && s.profile.Events != nil {
		id := before.LearnerID
		if len(changed) > 0 {
			_ = s.profile.Events.Capture(id, "learner_profile_updated", map[string]any{"fields_changed": changed, "source": "api", "platform": "web", "app_version": "0.1.0"})
		}
		if !before.SetupCompleted {
			_ = s.profile.Events.Capture(id, "onboarding_completed", map[string]any{"source": "api", "platform": "web", "app_version": "0.1.0"})
		}
	}
	var value LearnerContext
	if err = json.Unmarshal(updated, &value); err != nil {
		respond(w, 503, map[string]string{"error": "Could not read your profile. Please retry."})
		return
	}
	value.TextLimit = s.profile.TextLimit
	respond(w, 200, value)
}
