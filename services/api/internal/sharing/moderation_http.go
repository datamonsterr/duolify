package sharing

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
)

// ModerationHandler serves the moderator surface and the author's notices. The actor is always the
// session; a person without the moderator grant gets the same neutral 404 as an unknown route.
type ModerationHandler struct{ s *Service }

// NewModerationHandler returns the handler for the /v1/moderation routes.
func NewModerationHandler(s *Service) ModerationHandler { return ModerationHandler{s: s} }

func moderationFail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotModerator), errors.Is(err, ErrCourseUnavailable):
		write(w, http.StatusNotFound, map[string]string{"error": "not_found"})
	case errors.Is(err, ErrInvalidModeration):
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid_moderation"})
	case errors.Is(err, ErrSameModerator), errors.Is(err, ErrSelfModeration):
		write(w, http.StatusForbidden, map[string]string{"error": "not_allowed"})
	case errors.Is(err, ErrAppealUsed):
		write(w, http.StatusConflict, map[string]string{"error": "appeal_used"})
	default:
		slog.ErrorContext(r.Context(), "moderation request failed", "error", err)
		write(w, http.StatusServiceUnavailable, map[string]string{"error": "moderation_unavailable"})
	}
}

func decodeModeration(w http.ResponseWriter, r *http.Request, dst any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	d.DisallowUnknownFields()
	if d.Decode(dst) != nil || d.Decode(&struct{}{}) != io.EOF {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid_moderation"})
		return false
	}
	return true
}

func (h ModerationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	actor := accounts.LearnerID(r.Context())
	if actor == "" {
		write(w, http.StatusUnauthorized, map[string]string{"error": "sign_in_required"})
		return
	}
	switch r.Pattern {
	case "GET /v1/moderation/queue":
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit == 0 {
			limit = 50
		}
		view, err := h.s.ModerationView(r.Context(), actor, limit)
		if err != nil {
			moderationFail(w, r, err)
			return
		}
		write(w, http.StatusOK, view)
	case "POST /v1/moderation/items/{id}/actions":
		var in struct {
			Action     string `json:"action"`
			ReasonCode string `json:"reason_code"`
			Reason     string `json:"reason"`
		}
		if !decodeModeration(w, r, &in) {
			return
		}
		res, err := h.s.ModerateItem(r.Context(), actor, r.PathValue("id"), ActionInput{Action: in.Action, ReasonCode: in.ReasonCode, Reason: in.Reason})
		if err != nil {
			moderationFail(w, r, err)
			return
		}
		write(w, http.StatusCreated, res)
	case "POST /v1/moderation/appeals/{id}/decision":
		var in struct {
			Decision string `json:"decision"`
			Reason   string `json:"reason"`
		}
		if !decodeModeration(w, r, &in) {
			return
		}
		if err := h.s.DecideAppeal(r.Context(), actor, r.PathValue("id"), in.Decision, in.Reason); err != nil {
			moderationFail(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case "GET /v1/moderation/release-readiness":
		report, err := h.s.ReleaseReadiness(r.Context(), actor)
		if err != nil {
			moderationFail(w, r, err)
			return
		}
		write(w, http.StatusOK, report)
	case "GET /v1/moderation/notices":
		items, err := h.s.Notices(r.Context(), actor)
		if err != nil {
			moderationFail(w, r, err)
			return
		}
		write(w, http.StatusOK, items)
	case "POST /v1/moderation/notices/{id}/appeal":
		var in struct {
			Text string `json:"text"`
		}
		if !decodeModeration(w, r, &in) {
			return
		}
		id, err := h.s.FileAppeal(r.Context(), actor, r.PathValue("id"), in.Text)
		if err != nil {
			moderationFail(w, r, err)
			return
		}
		write(w, http.StatusCreated, map[string]string{"id": id})
	default:
		write(w, http.StatusNotFound, map[string]string{"error": "unknown_route"})
	}
}
