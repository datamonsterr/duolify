package sharing

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
)

// Handler serves the owner-only publication draft routes; the router protects them with the
// session so the actor always comes from it.
type Handler struct{ s *Service }

// NewHandler serves the owner-only draft routes for an authenticated actor.
func NewHandler(s *Service) *Handler { return &Handler{s: s} }

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrUnavailable):
		write(w, http.StatusNotFound, map[string]string{"error": "draft_unavailable"})
	case errors.Is(err, ErrOutOfAllowlist):
		write(w, http.StatusUnprocessableEntity, map[string]string{"error": "snapshot_rejected"})
	case errors.Is(err, ErrAdultRequired):
		write(w, http.StatusForbidden, map[string]string{"error": "adult_required"})
	case errors.Is(err, ErrVisibilityUnavailable):
		write(w, http.StatusConflict, map[string]string{"error": "visibility_unavailable"})
	default:
		slog.ErrorContext(r.Context(), "publication draft request failed", "error", err)
		write(w, http.StatusServiceUnavailable, map[string]string{"error": "sharing_unavailable"})
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	actor := accounts.LearnerID(r.Context())
	if actor == "" {
		write(w, http.StatusUnauthorized, map[string]string{"error": "Sign in required."})
		return
	}
	course, draft := r.PathValue("id"), r.PathValue("draftId")
	var (
		d      Draft
		err    error
		status = http.StatusOK
	)
	switch {
	case r.Method == http.MethodPost && draft == "" && r.URL.Path == "/v1/courses/"+course+"/publication-drafts":
		d, err = h.s.BuildDraft(r.Context(), actor, course)
		status = http.StatusCreated
	case r.Method == http.MethodGet && draft != "":
		d, err = h.s.Get(r.Context(), actor, course, draft)
	case r.Method == http.MethodPost && draft != "":
		d, err = h.s.Rescreen(r.Context(), actor, course, draft)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/courses/"+course+"/publish":
		h.fail(w, r, h.s.Publish(r.Context(), actor, course))
		return
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	write(w, status, d)
}
