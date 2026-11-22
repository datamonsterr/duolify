package accounts

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// OnboardingProgress is separate from optional learner-context confirmation.
// Only closed enums are persisted; replay never changes other profile fields.
type OnboardingProgress struct {
	WelcomeState string `json:"welcome_state"`
	TourState    string `json:"tour_state"`
}

func (s *Service) registerOnboarding() {
	s.mux.Handle("GET /v1/me/onboarding", s.Protect(http.HandlerFunc(s.readOnboarding)))
	s.mux.Handle("PATCH /v1/me/onboarding", s.Protect(http.HandlerFunc(s.saveOnboarding)))
}

func (s *Service) readOnboarding(w http.ResponseWriter, r *http.Request) {
	var value OnboardingProgress
	err := s.pool.QueryRow(r.Context(), `SELECT welcome_state,tour_state FROM learner_profiles WHERE learner_id=$1::uuid`, contextID(r)).Scan(&value.WelcomeState, &value.TourState)
	if err != nil {
		respond(w, 503, map[string]string{"error": "Could not read onboarding progress."})
		return
	}
	respond(w, 200, value)
}

func (s *Service) saveOnboarding(w http.ResponseWriter, r *http.Request) {
	// Strings intentionally reject null, pending and empty fields. Omitted fields
	// preserve state; an atomic UPDATE merges simultaneous device acknowledgments.
	var fields map[string]json.RawMessage
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&fields); err != nil || len(fields) == 0 {
		respond(w, 400, map[string]string{"error": "Choose a valid onboarding state."})
		return
	}
	var extra any
	if !errors.Is(decoder.Decode(&extra), io.EOF) {
		respond(w, 400, map[string]string{"error": "Invalid onboarding request."})
		return
	}
	var welcome, tour string
	for name, raw := range fields {
		var state string
		if json.Unmarshal(raw, &state) != nil || (state != "skipped" && state != "completed") || (name != "welcome_state" && name != "tour_state") {
			respond(w, 400, map[string]string{"error": "Choose a valid onboarding state."})
			return
		}
		if name == "welcome_state" {
			welcome = state
		} else {
			tour = state
		}
	}
	var value OnboardingProgress
	err := s.pool.QueryRow(r.Context(), `UPDATE learner_profiles SET
 welcome_state=CASE WHEN welcome_state='completed' OR $2='completed' THEN 'completed' WHEN $2='skipped' THEN 'skipped' ELSE welcome_state END,
 tour_state=CASE WHEN tour_state='completed' OR $3='completed' THEN 'completed' WHEN $3='skipped' THEN 'skipped' ELSE tour_state END
 WHERE learner_id=$1::uuid RETURNING welcome_state,tour_state`, contextID(r), welcome, tour).Scan(&value.WelcomeState, &value.TourState)
	if err != nil {
		respond(w, 503, map[string]string{"error": "Could not save onboarding progress."})
		return
	}
	respond(w, 200, value)
}
