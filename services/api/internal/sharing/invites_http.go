package sharing

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
)

// InviteAPI is what the invite routes need from the service.
type InviteAPI interface {
	SendInvite(ctx context.Context, actor, courseID string, in InviteInput) (Invite, error)
	SentInvites(ctx context.Context, actor, courseID string) ([]SentInvite, error)
	ReceivedInvites(ctx context.Context, actor string) ([]ReceivedInvite, int, error)
	MarkInvitesRead(ctx context.Context, actor string) error
	AcceptInvite(ctx context.Context, actor, id string) (InviteOutcome, error)
	DeclineInvite(ctx context.Context, actor, id string) (InviteOutcome, error)
	RevokeInvite(ctx context.Context, actor, id string) (InviteOutcome, error)
}

// InvitePatterns are the routes the invite handler serves.
var InvitePatterns = []string{
	"POST /v1/courses/{id}/invites", "GET /v1/courses/{id}/invites",
	"GET /v1/invites/received", "POST /v1/invites/read",
	"POST /v1/invites/{id}/accept", "POST /v1/invites/{id}/decline", "POST /v1/invites/{id}/revoke",
}

// InviteHandler serves the invite routes. The actor always comes from the session, never the body.
type InviteHandler struct{ api InviteAPI }

// NewInviteHandler wires the invite routes.
func NewInviteHandler(api InviteAPI) *InviteHandler { return &InviteHandler{api: api} }

func (h *InviteHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	actor := accounts.LearnerID(r.Context())
	if actor == "" {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	ctx, id := r.Context(), r.PathValue("id")
	switch r.Pattern {
	case "POST /v1/courses/{id}/invites":
		in, err := ParseInviteBody(r.Body)
		if err != nil {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid_invite"})
			return
		}
		inv, err := h.api.SendInvite(ctx, actor, id, in)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		write(w, http.StatusCreated, inv)
	case "GET /v1/courses/{id}/invites":
		list, err := h.api.SentInvites(ctx, actor, id)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		write(w, http.StatusOK, map[string]any{"invites": list})
	case "GET /v1/invites/received":
		list, unread, err := h.api.ReceivedInvites(ctx, actor)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		write(w, http.StatusOK, map[string]any{"invites": list, "unread": unread})
	case "POST /v1/invites/read":
		if err := h.api.MarkInvitesRead(ctx, actor); err != nil {
			h.fail(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		var out InviteOutcome
		var err error
		switch r.Pattern {
		case "POST /v1/invites/{id}/accept":
			out, err = h.api.AcceptInvite(ctx, actor, id)
		case "POST /v1/invites/{id}/decline":
			out, err = h.api.DeclineInvite(ctx, actor, id)
		case "POST /v1/invites/{id}/revoke":
			out, err = h.api.RevokeInvite(ctx, actor, id)
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			h.fail(w, r, err)
			return
		}
		write(w, http.StatusOK, out)
	}
}

func (h *InviteHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrInviteUnavailable):
		write(w, http.StatusNotFound, map[string]string{"error": "invite_unavailable"})
	case errors.Is(err, ErrCourseUnavailable):
		write(w, http.StatusNotFound, map[string]string{"error": "course_unavailable"})
	case errors.Is(err, ErrCourseNotShared):
		write(w, http.StatusConflict, map[string]string{"error": "course_not_shared"})
	case errors.Is(err, ErrInviteDuplicate):
		write(w, http.StatusConflict, map[string]string{"error": "invite_duplicate"})
	case errors.Is(err, ErrInviteRateLimited), errors.Is(err, ErrRateLimited):
		code := "invite_rate_limited"
		if errors.Is(err, ErrRateLimited) {
			code = "rate_limited"
		}
		write(w, http.StatusTooManyRequests, map[string]string{"error": code})
	default:
		slog.ErrorContext(r.Context(), "invite request failed", "error", err)
		write(w, http.StatusServiceUnavailable, map[string]string{"error": "sharing_unavailable"})
	}
}
