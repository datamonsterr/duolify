package chat

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/google/uuid"
)

const edgeChatPath = "/internal/edge/chat"

// EdgeOptions connects the Edge transport to the existing API authorities.
type EdgeOptions struct {
	Secret        string
	Tutor         *Service
	SessionActive func(context.Context, string, string) (bool, error)
	GuidedSetup   http.Handler
	GuidedBrief   http.Handler
}

// Edge authorizes Worker operations and delegates them to the existing chat handlers.
type Edge struct{ opts EdgeOptions }

// NewEdge constructs the service boundary using the existing chat and session authorities.
func NewEdge(opts EdgeOptions) *Edge { return &Edge{opts: opts} }

type edgeScopeKey struct{}

func scopeFromContext(ctx context.Context) (edgeScope, bool) {
	scope, ok := ctx.Value(edgeScopeKey{}).(edgeScope)
	return scope, ok
}

func canonicalUUID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id.String() == value && id != uuid.Nil
}

func (scope edgeScope) guided() bool {
	return scope.Surface == "setup" || scope.Surface == "course_brief"
}

func (scope edgeScope) canReadTurn(thread string, createdAt time.Time) bool {
	if scope.ThreadID != "" {
		return thread == scope.ThreadID
	}
	if thread == "" {
		return true
	}
	// An initial channel can recover the first thread by its turn key before it knows the thread ID.
	issuedAt := time.Unix(scope.ExpiresAt, 0).Add(-channelTTL)
	return !createdAt.Before(issuedAt)
}

// MountRoutes registers authenticated grant issuance and the scoped service routes.
func (e *Edge) MountRoutes(mux *http.ServeMux, protect func(http.Handler) http.Handler, allowedOrigin func(string) bool) {
	if len(e.opts.Secret) < 32 || e.opts.Tutor == nil || e.opts.SessionActive == nil {
		return
	}
	mux.Handle("POST /v1/courses/{id}/tutor/edge-channel", protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.issue(w, r, allowedOrigin, r.PathValue("id"), false)
	})))
	mux.Handle("POST /v1/me/guided-chat/edge-channel", protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.issue(w, r, allowedOrigin, "", true)
	})))
	for _, route := range []struct {
		method, path, op string
	}{
		{"POST", "/authorize", "authorize"},
		{"POST", "/quote", "quote"},
		{"POST", "/messages", "messages"},
		{"GET", "/thread", "thread"},
		{"GET", "/turns/{key}", "turn"},
		{"POST", "/turns/{key}/stop", "stop"},
		{"POST", "/guided-turn", "guided"},
		{"POST", "/fallback", "fallback"},
	} {
		mux.HandleFunc(route.method+" "+edgeChatPath+route.path, func(w http.ResponseWriter, r *http.Request) {
			e.serveService(w, r, allowedOrigin, route.op)
		})
	}
}

func (e *Edge) issue(w http.ResponseWriter, r *http.Request, allowedOrigin func(string) bool, course string, guided bool) {
	w.Header().Set("Cache-Control", "no-store")
	origin := r.Header.Get("Origin")
	if origin == "" || !allowedOrigin(origin) {
		fail(w, http.StatusForbidden, "origin_not_allowed")
		return
	}
	scope := edgeScope{
		LearnerID: accounts.LearnerID(r.Context()),
		SessionID: accounts.SessionID(r.Context()),
		Origin:    origin,
		CourseID:  course,
		ExpiresAt: time.Now().Add(channelTTL).Unix(),
	}
	if guided {
		var in struct {
			Surface string `json:"surface"`
		}
		if !decode(w, r, &in) {
			return
		}
		scope.Surface = in.Surface
		if !scope.guided() || e.guidedHandler(scope) == nil {
			fail(w, http.StatusNotFound, "not_found")
			return
		}
	} else {
		var in struct {
			ThreadID string `json:"thread_id"`
		}
		if !decode(w, r, &in) {
			return
		}
		if !canonicalUUID(course) || !e.opts.Tutor.ownsCourse(r.Context(), scope.LearnerID, course) {
			fail(w, http.StatusNotFound, "not_found")
			return
		}
		scope.ThreadID = in.ThreadID
		if scope.ThreadID == "" {
			var err error
			scope.ThreadID, err = e.opts.Tutor.latestThread(r.Context(), scope.LearnerID, course)
			if err != nil {
				fail(w, http.StatusServiceUnavailable, "unavailable")
				return
			}
		}
	}
	if status := e.validateScope(r.Context(), &scope, allowedOrigin); status != http.StatusOK {
		edgeFailure(w, status)
		return
	}
	token, err := signEdge(e.opts.Secret, edgeChannelDomain, scope)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	respond(w, http.StatusOK, map[string]any{"token": token, "expires_at": scope.ExpiresAt})
}

func (e *Edge) validateScope(ctx context.Context, scope *edgeScope, allowedOrigin func(string) bool) int {
	now := time.Now()
	if scope == nil || !canonicalUUID(scope.LearnerID) || !canonicalUUID(scope.SessionID) ||
		scope.ExpiresAt <= now.Unix() || scope.ExpiresAt > now.Add(channelTTL).Unix() ||
		scope.Origin == "" || !allowedOrigin(scope.Origin) {
		return http.StatusUnauthorized
	}
	active, err := e.opts.SessionActive(ctx, scope.LearnerID, scope.SessionID)
	if err != nil {
		return http.StatusServiceUnavailable
	}
	if !active {
		return http.StatusUnauthorized
	}
	if scope.guided() {
		if scope.CourseID != "" || scope.ThreadID != "" || e.guidedHandler(*scope) == nil {
			return http.StatusNotFound
		}
		return http.StatusOK
	}
	if (scope.Surface != "" && scope.Surface != "tutor") || !canonicalUUID(scope.CourseID) ||
		!e.opts.Tutor.ownsCourse(ctx, scope.LearnerID, scope.CourseID) {
		return http.StatusNotFound
	}
	if scope.ThreadID != "" && (!canonicalUUID(scope.ThreadID) ||
		!e.opts.Tutor.ownsThread(ctx, scope.LearnerID, scope.CourseID, scope.ThreadID)) {
		return http.StatusNotFound
	}
	return http.StatusOK
}

func edgeFailure(w http.ResponseWriter, status int) {
	code := http.StatusText(status)
	switch status {
	case http.StatusUnauthorized:
		code = "unauthorized"
	case http.StatusNotFound:
		code = "not_found"
	case http.StatusServiceUnavailable:
		code = "unavailable"
	}
	fail(w, status, code)
}

func (e *Edge) serveService(w http.ResponseWriter, r *http.Request, allowedOrigin func(string) bool, op string) {
	w.Header().Set("Cache-Control", "no-store")
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		status := http.StatusBadRequest
		if errors.As(err, new(*http.MaxBytesError)) {
			status = http.StatusRequestEntityTooLarge
		}
		fail(w, status, "invalid_request")
		return
	}
	claims, err := verifyEdgeService(e.opts.Secret, r, body, time.Now())
	if err != nil {
		edgeFailure(w, http.StatusUnauthorized)
		return
	}
	if op == "authorize" {
		e.authorize(w, r, allowedOrigin, claims, body)
		return
	}
	if status := e.validateScope(r.Context(), claims.Scope, allowedOrigin); status != http.StatusOK {
		edgeFailure(w, status)
		return
	}
	scope := *claims.Scope
	ctx := analytics.WithChatTransport(accounts.WithLearnerID(r.Context(), scope.LearnerID), claims.Transport)
	ctx = context.WithValue(ctx, edgeScopeKey{}, scope)
	r = r.WithContext(ctx)
	r.Body = io.NopCloser(bytes.NewReader(body))
	if op == "fallback" {
		e.fallback(w, r, scope)
		return
	}
	if scope.guided() {
		if op != "guided" {
			edgeFailure(w, http.StatusNotFound)
			return
		}
		handler := e.guidedHandler(scope)
		if scope.Surface == "course_brief" {
			r.URL.Path = "/v1/course-brief/chat"
		}
		handler.ServeHTTP(w, r)
		return
	}
	e.serveTutor(w, r, scope, op)
}

func (e *Edge) authorize(w http.ResponseWriter, r *http.Request, allowedOrigin func(string) bool, claims edgeServiceClaims, body []byte) {
	var in struct {
		Token  string `json:"token"`
		Origin string `json:"origin"`
	}
	var scope edgeScope
	if claims.Scope != nil || strictEdgeJSON(body, &in) != nil ||
		verifyEdge(e.opts.Secret, edgeChannelDomain, in.Token, &scope) != nil || in.Origin != scope.Origin {
		edgeFailure(w, http.StatusUnauthorized)
		return
	}
	if status := e.validateScope(r.Context(), &scope, allowedOrigin); status != http.StatusOK {
		edgeFailure(w, status)
		return
	}
	respond(w, http.StatusOK, scope)
}

func (e *Edge) guidedHandler(scope edgeScope) http.Handler {
	switch scope.Surface {
	case "setup":
		return e.opts.GuidedSetup
	case "course_brief":
		return e.opts.GuidedBrief
	}
	return nil
}

func (e *Edge) serveTutor(w http.ResponseWriter, r *http.Request, scope edgeScope, op string) {
	tutor := e.opts.Tutor
	owner, course, key := scope.LearnerID, scope.CourseID, r.PathValue("key")
	switch op {
	case "quote":
		tutor.quote(w, r, owner, course)
	case "messages":
		tutor.send(w, r, owner, course)
	case "thread":
		if scope.ThreadID != "" {
			tutor.threadMessages(w, r, owner, scope.ThreadID)
		} else {
			respond(w, http.StatusOK, map[string]any{"thread": nil, "messages": []messageDTO{}})
		}
	case "turn":
		tutor.turn(w, r, owner, course, key)
	case "stop":
		tutor.stop(w, r, owner, course, key)
	default:
		edgeFailure(w, http.StatusNotFound)
	}
}

func (e *Edge) fallback(w http.ResponseWriter, r *http.Request, scope edgeScope) {
	var in struct {
		From   string `json:"from_transport"`
		To     string `json:"to_transport"`
		Reason string `json:"reason_code"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !analytics.ValidChatFallback(in.From, in.To, in.Reason) {
		fail(w, http.StatusBadRequest, "invalid_request")
		return
	}
	surface := "tutor"
	if scope.guided() {
		surface = "guided"
	}
	e.opts.Tutor.emit(r.Context(), scope.LearnerID, "chat_transport_fallback", map[string]any{
		"surface": surface, "from_transport": in.From, "to_transport": in.To, "reason_code": in.Reason,
	})
	w.WriteHeader(http.StatusNoContent)
}
