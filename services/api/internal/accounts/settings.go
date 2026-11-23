package accounts

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"sort"

	"github.com/datamonsterr/duolify/services/api/internal/store/db"
)

var settingsFields = map[string]bool{
	"appearance": true, "motion": true, "narration": true, "narration_speed": true,
	"captions": true, "text_size": true, "ui_sounds": true, "haptics": true,
	"locale": true, "time_zone": true, "daily_minutes": true, "daily_xp": true,
	"reminder_enabled": true, "reminder_time": true, "analytics_opt_out": true,
	"fullscreen_on_rotate": true, "autoplay": true, "auto_send_after_dictation": true,
	"mascot": true,
}

// LearnerSettings contains only non-content preferences returned by the settings API.
type LearnerSettings struct {
	LearnerID       string  `json:"learner_id"`
	Locale          string  `json:"locale"`
	TimeZone        string  `json:"time_zone"`
	DailyMinutes    *int    `json:"daily_minutes"`
	DailyXP         *int    `json:"daily_xp"`
	ReminderEnabled *bool   `json:"reminder_enabled"`
	ReminderTime    string  `json:"reminder_time"`
	AnalyticsOptOut bool    `json:"analytics_opt_out"`
	Appearance      string  `json:"appearance"`
	Motion          string  `json:"motion"`
	Narration       bool    `json:"narration"`
	NarrationSpeed  float64 `json:"narration_speed"`
	Captions        bool    `json:"captions"`
	TextSize        float64 `json:"text_size"`
	UISounds        bool    `json:"ui_sounds"`
	Haptics         string  `json:"haptics"`

	FullscreenOnRotate     bool `json:"fullscreen_on_rotate"`
	Autoplay               bool `json:"autoplay"`
	AutoSendAfterDictation bool `json:"auto_send_after_dictation"`

	// Mascot is the guide preference (RigPartSpec v1); partial updates merge, see mascot.go.
	Mascot json.RawMessage `json:"mascot"`
}

func profileContextField(name string) bool {
	switch name {
	case "goal", "topic", "background", "level", "locale", "time_zone", "daily_minutes", "daily_xp", "reminder_enabled", "reminder_time":
		return true
	default:
		return false
	}
}

// ValidateSettingsPatch validates an allowlisted settings update without reflecting private input.
func ValidateSettingsPatch(fields map[string]json.RawMessage, limit int) (map[string]any, map[string]string) {
	values := map[string]any{}
	failures := map[string]string{}
	ordinary := make(map[string]json.RawMessage, len(fields))
	for name, raw := range fields {
		if !settingsFields[name] {
			failures[name] = "Enter a valid setting."
			continue
		}
		if name == "analytics_opt_out" {
			var value bool
			if err := json.Unmarshal(raw, &value); err != nil {
				failures[name] = "Enter a valid setting."
			} else {
				values[name] = value
			}
			continue
		}
		if name == "mascot" {
			if patch, ok := ParseMascotPatch(raw); ok {
				values[name] = patch
			} else {
				failures[name] = "Enter a valid setting."
			}
			continue
		}
		ordinary[name] = raw
	}
	valid, invalid := ValidateContextPatch(ordinary, limit)
	for name, value := range valid {
		values[name] = value
	}
	for name, message := range invalid {
		failures[name] = message
	}
	return values, failures
}

func (s *Service) readSettings(w http.ResponseWriter, r *http.Request) {
	raw, err := db.New(s.pool).GetLearnerSettings(r.Context(), contextID(r))
	var value LearnerSettings
	if err == nil {
		err = json.Unmarshal(raw, &value)
	}
	if err != nil {
		respond(w, 503, map[string]string{"error": "Could not read settings. Please retry."})
		return
	}
	value.Mascot = ResolveMascot(r.Context(), s.pool, contextID(r), value.Mascot)
	respond(w, 200, value)
}

func (s *Service) saveSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Confirmed bool                       `json:"confirmed"`
		Fields    map[string]json.RawMessage `json:"fields"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil || !in.Confirmed {
		respond(w, 400, map[string]string{"error": "Explicit settings confirmation is required."})
		return
	}
	var extra any
	if !errors.Is(decoder.Decode(&extra), io.EOF) {
		respond(w, 400, map[string]string{"error": "Invalid settings request."})
		return
	}
	for name := range in.Fields {
		if !settingsFields[name] {
			respond(w, 400, map[string]string{"error": "Check the highlighted settings."})
			return
		}
	}
	patch, failures := ValidateSettingsPatch(in.Fields, s.profile.TextLimit)
	if len(failures) != 0 {
		respond(w, 400, map[string]any{"error": "Check the highlighted settings.", "fields": failures})
		return
	}
	mascotPatch, hasMascot := patch["mascot"].(MascotPatch)
	delete(patch, "mascot")
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		respond(w, 503, map[string]string{"error": "Could not save settings. Please retry."})
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	raw, err := queries.GetLearnerSettings(ctx, contextID(r))
	var before LearnerSettings
	var current map[string]any
	if err == nil {
		err = json.Unmarshal(raw, &before)
	}
	if err == nil {
		err = json.Unmarshal(raw, &current)
	}
	changed := []string{}
	if err == nil {
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
	if err == nil && hasMascot {
		stored, _ := json.Marshal(current["mascot"])
		merged, ok := MergeMascot(stored, mascotPatch)
		if ok && merged.Mode == "character" && !MascotCharacterUsable(ctx, tx, contextID(r), *merged.CharacterID) {
			ok = false
		}
		if !ok {
			respond(w, 400, map[string]any{"error": "Check the highlighted settings.", "fields": map[string]string{"mascot": "Enter a valid setting."}})
			return
		}
		encoded, _ := json.Marshal(merged)
		var next any
		_ = json.Unmarshal(encoded, &next)
		if !reflect.DeepEqual(current["mascot"], next) {
			changed = append(changed, "mascot")
		}
		current["mascot"] = next
	}
	updated, marshalErr := json.Marshal(current)
	if err == nil {
		err = marshalErr
	}
	if err == nil {
		err = queries.SaveLearnerSettings(ctx, db.SaveLearnerSettingsParams{Context: updated, LearnerID: contextID(r)})
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		respond(w, 503, map[string]string{"error": "Could not save settings. Please retry."})
		return
	}
	sort.Strings(changed)
	var after LearnerSettings
	if err = json.Unmarshal(updated, &after); err != nil {
		respond(w, 503, map[string]string{"error": "Could not read saved settings. Please retry."})
		return
	}
	// Opt-out suppresses every event, including the transition itself. Opting
	// back in may emit only after the persisted state allows capture again.
	if !after.AnalyticsOptOut && s.profile.Events != nil {
		if len(changed) > 0 {
			_ = s.profile.Events.Capture(after.LearnerID, "learner_profile_updated", map[string]any{"fields_changed": changed, "source": "api", "platform": "web", "app_version": "0.1.0"})
		}
		if before.AnalyticsOptOut != after.AnalyticsOptOut {
			_ = s.profile.Events.Capture(after.LearnerID, "analytics_opt_out_changed", map[string]any{"enabled": true, "source": "api", "platform": "web", "app_version": "0.1.0"})
		}
	}
	after.Mascot = ResolveMascot(ctx, s.pool, contextID(r), after.Mascot)
	respond(w, 200, after)
}
