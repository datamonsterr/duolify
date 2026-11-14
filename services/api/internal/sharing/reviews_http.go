package sharing

import (
	"encoding/json"
	"errors"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"io"
	"net/http"
)

// ReviewHandler uses only session identity. Unknown courses and foreign reviews are neutral.
type ReviewHandler struct{ s *Service }

func (h ReviewHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	if !h.s.MarketplaceEnabled() {
		marketFail(w, r, 0, ErrMarketplaceDisabled)
		return
	}
	actor, course := accounts.LearnerID(r.Context()), r.PathValue("id")
	if r.Method == http.MethodGet {
		if err := h.s.AllowPublicRead(r); err != nil {
			marketFail(w, r, h.s.RetryAfterSeconds(), err)
			return
		}
		p, err := h.s.Reviews(r.Context(), actor, course)
		if err != nil {
			marketFail(w, r, 0, err)
			return
		}
		write(w, http.StatusOK, p)
		return
	}
	if actor == "" {
		write(w, http.StatusUnauthorized, map[string]string{"error": "sign_in_required"})
		return
	}
	var err error
	if r.Method == http.MethodDelete {
		err = h.s.DeleteReview(r.Context(), actor, course)
		if err == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	} else {
		var in struct {
			Stars int    `json:"stars"`
			Text  string `json:"text"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
		d.DisallowUnknownFields()
		if d.Decode(&in) != nil || d.Decode(&struct{}{}) != io.EOF {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid_review"})
			return
		}
		var review Review
		review, err = h.s.SaveReview(r.Context(), actor, course, in.Stars, in.Text)
		if err == nil {
			write(w, http.StatusOK, review)
			return
		}
	}
	var gate *ReviewGateError
	switch {
	case errors.As(err, &gate):
		write(w, http.StatusUnprocessableEntity, map[string]any{"error": "review_locked", "lessons_needed": gate.Needed})
	case errors.Is(err, ErrInvalidReview):
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid_review"})
	case errors.Is(err, ErrSelfReview):
		write(w, http.StatusForbidden, map[string]string{"error": "self_review"})
	default:
		marketFail(w, r, h.s.RetryAfterSeconds(), err)
	}
}
