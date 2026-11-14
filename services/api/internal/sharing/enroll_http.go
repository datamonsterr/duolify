package sharing

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
)

// Enroller is what the enroll routes need from the service.
type Enroller interface {
	Enroll(ctx context.Context, viewer, token string) (Enrollment, error)
	Existing(ctx context.Context, viewer, token string) (EnrollmentState, error)
}

// EnrollHandler serves the signed-in enroll routes. The actor always comes from the session.
type EnrollHandler struct{ e Enroller }

// NewEnrollHandler wires the enroll routes.
func NewEnrollHandler(e Enroller) *EnrollHandler { return &EnrollHandler{e: e} }

func (h *EnrollHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	actor := accounts.LearnerID(r.Context())
	if actor == "" {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	token := r.PathValue("token")
	if r.Method == http.MethodGet {
		state, err := h.e.Existing(r.Context(), actor, token)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		write(w, http.StatusOK, state)
		return
	}
	got, err := h.e.Enroll(r.Context(), actor, token)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	status := http.StatusOK
	if got.Created {
		status = http.StatusCreated
	}
	write(w, status, got)
}

func (h *EnrollHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrLinkNotFound):
		write(w, http.StatusNotFound, map[string]string{"error": "link_not_found"})
	case errors.Is(err, ErrRateLimited):
		w.Header().Set("Retry-After", "3600")
		write(w, http.StatusTooManyRequests, map[string]string{"error": "rate_limited"})
	default:
		slog.ErrorContext(r.Context(), "enroll request failed", "error", err)
		write(w, http.StatusServiceUnavailable, map[string]string{"error": "sharing_unavailable"})
	}
}
