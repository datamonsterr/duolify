package sharing

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"

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
	case errors.Is(err, ErrCharacterConfirmation), errors.Is(err, ErrCharacterRelease):
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, ErrUnavailable):
		write(w, http.StatusNotFound, map[string]string{"error": "draft_unavailable"})
	case errors.Is(err, ErrOutOfAllowlist):
		write(w, http.StatusUnprocessableEntity, map[string]string{"error": "snapshot_rejected"})
	case errors.Is(err, ErrAdultRequired):
		write(w, http.StatusForbidden, map[string]string{"error": "adult_required"})
	case errors.Is(err, ErrBirthDateRequired):
		write(w, http.StatusConflict, map[string]string{"error": "birth_date_required"})
	case errors.Is(err, ErrVisibilityUnavailable):
		write(w, http.StatusConflict, map[string]string{"error": "visibility_unavailable"})
	case errors.Is(err, ErrPublishNotAllowed):
		write(w, http.StatusConflict, map[string]string{"error": "publish_not_allowed"})
	case errors.Is(err, ErrLinkNotFound):
		write(w, http.StatusNotFound, map[string]string{"error": "link_not_found"})
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
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1/moderation/character-snapshots/"):
		var body struct {
			Reason string `json:"reason_code"`
		}
		if !decodeBody(w, r, &body, false) {
			return
		}
		if body.Reason != "unsafe" && body.Reason != "legal" {
			write(w, 400, map[string]string{"error": "invalid_reason"})
			return
		}
		err = h.s.SuppressCharacter(r.Context(), actor, r.PathValue("snapshotId"), body.Reason)
		if errors.Is(err, ErrNotModerator) {
			write(w, 403, map[string]string{"error": "not_moderator"})
			return
		}
		if err != nil {
			h.fail(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/character-export"):
		disclosure, e := h.s.CharacterDisclosure(r.Context(), actor, course)
		if e != nil {
			h.fail(w, r, e)
			return
		}
		write(w, http.StatusOK, disclosure)
		return
	case r.Method == http.MethodPost && draft == "" && r.URL.Path == "/v1/courses/"+course+"/publication-drafts":
		var body struct {
			Confirmed bool `json:"character_export_confirmed"`
		}
		if !decodeBody(w, r, &body, true) {
			return
		}
		d, err = h.s.BuildDraft(WithCharacterExportConfirmation(r.Context(), body.Confirmed), actor, course)
		status = http.StatusCreated
	case r.Method == http.MethodGet && draft != "":
		d, err = h.s.Get(r.Context(), actor, course, draft)
	case r.Method == http.MethodPost && draft != "":
		d, err = h.s.Rescreen(r.Context(), actor, course, draft)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/courses/"+course+"/publish":
		h.fail(w, r, h.s.Publish(r.Context(), actor, course))
		return
	case r.Method == http.MethodGet && r.URL.Path == "/v1/courses/"+course+"/share-link":
		st, serr := h.s.LinkStatus(r.Context(), actor, course)
		if serr != nil {
			h.fail(w, r, serr)
			return
		}
		write(w, http.StatusOK, st)
		return
	case r.Method == http.MethodPost && r.URL.Path == "/v1/courses/"+course+"/share-link":
		// PublishLink: create/publish a share link for a course with verdict=pass.
		token, terr := h.s.PublishLink(r.Context(), actor, course)
		if terr != nil {
			h.fail(w, r, terr)
			return
		}
		write(w, http.StatusCreated, h.s.linkBody(token))
		return
	case r.Method == http.MethodDelete && r.URL.Path == "/v1/courses/"+course+"/share-link":
		// RevokeLink: revoke the active share link.
		if rerr := h.s.RevokeLink(r.Context(), actor, course); rerr != nil {
			h.fail(w, r, rerr)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	case r.Method == http.MethodPost && r.URL.Path == "/v1/courses/"+course+"/unpublish":
		// Unpublish: private, link revoked, delisted, enrollment stopped; existing copies stay.
		if uerr := h.s.Unpublish(r.Context(), actor, course); uerr != nil {
			h.fail(w, r, uerr)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	case r.Method == http.MethodPost && r.URL.Path == "/v1/courses/"+course+"/share-link/rotate":
		// RotateLink: generate a new token, revoke the old one.
		token, rerr := h.s.RotateLink(r.Context(), actor, course)
		if rerr != nil {
			h.fail(w, r, rerr)
			return
		}
		write(w, http.StatusOK, h.s.linkBody(token))
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

// PreviewHandler serves the public preview endpoint (anonymous allowed).
// It is NOT protected by auth middleware — the actor is optional.
type PreviewHandler struct{ s *Service }

// NewPreviewHandler returns the anonymous preview handler.
func NewPreviewHandler(s *Service) *PreviewHandler { return &PreviewHandler{s: s} }

// WithTurnstile configures proof checking for public share previews.
func (s *Service) WithTurnstile(gate *accounts.Turnstile) *Service {
	c := *s
	c.turnstile = gate
	return &c
}

// VerifyPreviewProof is the Go challenge seam for a preview read. The caller
// first applies the preview rate limit and uses the same proof only once.
func (s *Service) VerifyPreviewProof(w http.ResponseWriter, r *http.Request) bool {
	return s.turnstile.Require(w, r, r.Header.Get(accounts.TurnstileHeader), s.clientIP(r), accounts.TurnstileSharePreview, accounts.LearnerID(r.Context()))
}

func (h *PreviewHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	token := r.PathValue("token")
	// Set noindex headers on every response, including refusals.
	w.Header().Set("X-Robots-Tag", "noindex")
	w.Header().Set("Cache-Control", "no-store")
	if err := h.s.AllowPreview(r.Context(), h.s.clientIP(r)); err != nil {
		if errors.Is(err, ErrRateLimited) {
			w.Header().Set("Retry-After", strconv.Itoa(h.s.cfg.PreviewRateWindowSeconds))
			write(w, http.StatusTooManyRequests, map[string]string{"error": "rate_limited"})
			return
		}
		slog.ErrorContext(r.Context(), "public preview limiter failed", "error", err)
		write(w, http.StatusServiceUnavailable, map[string]string{"error": "sharing_unavailable"})
		return
	}
	if !h.s.VerifyPreviewProof(w, r) {
		return
	}

	viewerID := accounts.LearnerID(r.Context()) // may be ""
	payload, err := h.s.GetPreview(r.Context(), token, viewerID)
	if err != nil {
		if errors.Is(err, ErrLinkNotFound) {
			write(w, http.StatusNotFound, map[string]string{"error": "link_not_found"})
		} else {
			slog.ErrorContext(r.Context(), "public preview failed", "error", err)
			write(w, http.StatusServiceUnavailable, map[string]string{"error": "sharing_unavailable"})
		}
		return
	}
	write(w, http.StatusOK, payload)
}

// linkBody is the one-time response that carries the plaintext token.
func (s *Service) linkBody(token string) map[string]string {
	return map[string]string{
		"token":       token,
		"preview_url": "https://" + s.cfg.PublicDomain + "/c/" + token,
		"deep_link":   s.cfg.AppScheme + "://share/" + token,
	}
}

// clientIP honours X-Forwarded-For only for the configured number of trusted proxy hops.
func (s *Service) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if s.cfg.TrustedProxyHops <= 0 {
		return host
	}
	chain := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	if i := len(chain) - s.cfg.TrustedProxyHops; i >= 0 {
		if ip := net.ParseIP(strings.TrimSpace(chain[i])); ip != nil {
			return ip.String()
		}
	}
	return host
}
