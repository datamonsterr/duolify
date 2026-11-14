package sharing

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
)

// Catalogue is what the public Marketplace routes need from the service.
type Catalogue interface {
	MarketplaceEnabled() bool
	AllowPublicRead(r *http.Request) error
	RetryAfterSeconds() int
	Browse(ctx context.Context, viewer string, q BrowseQuery) (Page, error)
	MarketCourse(ctx context.Context, viewer, id string) (MarketCoursePage, error)
	EnrollMarketplace(ctx context.Context, viewer, id string) (Enrollment, error)
}

// AllowPublicRead counts one anonymous catalogue read for the caller's address.
func (s *Service) AllowPublicRead(r *http.Request) error {
	return s.allow(r.Context(), "marketplace-read:"+s.clientIP(r))
}

// RetryAfterSeconds is the limiter window.
func (s *Service) RetryAfterSeconds() int { return s.cfg.PreviewRateWindowSeconds }

// MarketHandler serves browse and the Course page anonymously (the session, when present, only
// adds the block filter) and enrollment for a signed-in actor. The actor is never read from a body.
type MarketHandler struct{ c Catalogue }

// NewMarketHandler wires the public Marketplace routes.
func NewMarketHandler(c Catalogue) *MarketHandler { return &MarketHandler{c: c} }

func marketFail(w http.ResponseWriter, r *http.Request, retryAfter int, err error) {
	switch {
	case errors.Is(err, ErrMarketplaceDisabled):
		write(w, http.StatusNotFound, map[string]string{"error": "marketplace_unavailable"})
	case errors.Is(err, ErrCourseUnavailable):
		write(w, http.StatusNotFound, map[string]string{"error": "course_unavailable"})
	case errors.Is(err, ErrInvalidCursor):
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid_cursor"})
	case errors.Is(err, ErrInvalidQuery):
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid_query"})
	case errors.Is(err, ErrRateLimited):
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		write(w, http.StatusTooManyRequests, map[string]string{"error": "rate_limited"})
	default:
		// The request (and so the search text) is deliberately not logged.
		slog.ErrorContext(r.Context(), "marketplace request failed", "error", err)
		write(w, http.StatusServiceUnavailable, map[string]string{"error": "sharing_unavailable"})
	}
}

func (h *MarketHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Robots-Tag", "noindex")
	w.Header().Set("Cache-Control", "no-store")
	retry := h.c.RetryAfterSeconds()
	if !h.c.MarketplaceEnabled() {
		marketFail(w, r, retry, ErrMarketplaceDisabled)
		return
	}
	id := r.PathValue("id")
	if id != "" && !validID(id) {
		marketFail(w, r, retry, ErrCourseUnavailable)
		return
	}
	viewer := accounts.LearnerID(r.Context())
	if r.Method == http.MethodPost {
		if viewer == "" {
			write(w, http.StatusUnauthorized, map[string]string{"error": "Sign in required."})
			return
		}
		e, err := h.c.EnrollMarketplace(r.Context(), viewer, id)
		if err != nil {
			marketFail(w, r, retry, err)
			return
		}
		status := http.StatusOK
		if e.Created {
			status = http.StatusCreated
		}
		write(w, status, e)
		return
	}
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	if err := h.c.AllowPublicRead(r); err != nil {
		marketFail(w, r, retry, err)
		return
	}
	if id != "" {
		p, err := h.c.MarketCourse(r.Context(), viewer, id)
		if err != nil {
			marketFail(w, r, retry, err)
			return
		}
		write(w, http.StatusOK, p)
		return
	}
	v := r.URL.Query()
	q := BrowseQuery{Q: v.Get("q"), Category: v.Get("category"), Language: v.Get("language"), Sort: v.Get("sort"), Cursor: v.Get("cursor")}
	if raw := v.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			marketFail(w, r, retry, ErrInvalidQuery)
			return
		}
		q.Limit = n
	}
	page, err := h.c.Browse(r.Context(), viewer, q)
	if err != nil {
		marketFail(w, r, retry, err)
		return
	}
	write(w, http.StatusOK, page)
}

// Lister is what the owner listing routes need from the service.
type Lister interface {
	MarketplaceEnabled() bool
	ListCourse(ctx context.Context, owner, courseID string, in ListingInput) (Listing, error)
	DelistCourse(ctx context.Context, owner, courseID string) error
	ListingStatus(ctx context.Context, owner, courseID string) (Listing, error)
}

// ListingHandler serves the owner-only list, delist and status routes behind the session.
type ListingHandler struct{ l Lister }

// NewListingHandler wires the owner listing routes.
func NewListingHandler(l Lister) *ListingHandler { return &ListingHandler{l: l} }

func (h *ListingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.l.MarketplaceEnabled() {
		marketFail(w, r, 0, ErrMarketplaceDisabled)
		return
	}
	owner := accounts.LearnerID(r.Context())
	if owner == "" {
		write(w, http.StatusUnauthorized, map[string]string{"error": "Sign in required."})
		return
	}
	course := r.PathValue("id")
	var (
		l   Listing
		err error
	)
	switch r.Method {
	case http.MethodGet:
		l, err = h.l.ListingStatus(r.Context(), owner, course)
	case http.MethodPut:
		var in ListingInput
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
			return
		}
		l, err = h.l.ListCourse(r.Context(), owner, course, in)
	case http.MethodDelete:
		if err = h.l.DelistCourse(r.Context(), owner, course); err == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	default:
		http.NotFound(w, r)
		return
	}
	var ne *NotEligibleError
	switch {
	case err == nil:
		write(w, http.StatusOK, l)
	case errors.As(err, &ne):
		write(w, http.StatusUnprocessableEntity, map[string]any{"error": "not_eligible", "reasons": ne.Reasons})
	case errors.Is(err, ErrUnavailable):
		write(w, http.StatusNotFound, map[string]string{"error": "course_unavailable"})
	case errors.Is(err, ErrAdultRequired):
		write(w, http.StatusForbidden, map[string]string{"error": "adult_required"})
	case errors.Is(err, ErrPublishNotAllowed):
		write(w, http.StatusConflict, map[string]string{"error": "publish_not_allowed"})
	default:
		marketFail(w, r, 0, err)
	}
}
