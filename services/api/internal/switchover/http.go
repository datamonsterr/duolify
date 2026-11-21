package switchover

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/jackc/pgx/v5/pgxpool"
)

const adminPrefix = "/admin/v1/switchover/"

// Middleware must be the outermost handler. It stamps every request so the gate can tell in-flight
// work from new work, lets operator endpoints through a closed gate, and in read-only mode answers
// writes with 503 and Retry-After while reads and SSE streams continue.
func (g *Gate) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, adminPrefix) {
			next.ServeHTTP(w, r.WithContext(Bypass(r.Context())))
			return
		}
		if g.State() == StateReadOnly {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
			default:
				w.Header().Set("Retry-After", "5")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "read-only maintenance window; retry shortly"})
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(g.Admit(r.Context())))
	})
}

// OperatorOnly authorizes the caller as a pricing administrator with a fresh sign-in (the project's operator grant).
// The router must run accounts.Protect first so the request carries a validated session.
func OperatorOnly(pool *pgxpool.Pool) func(*http.Request) (bool, error) {
	return func(r *http.Request) (bool, error) {
		actor := accounts.LearnerID(r.Context())
		if actor == "" {
			return false, nil
		}
		return pricing.HasAdminAccess(r.Context(), pool, actor, accounts.SessionID(r.Context()))
	}
}

// Handler serves /admin/v1/switchover/{switch,open,readonly,resume} (POST) and status (GET). Every
// route calls authorize first; a nil authorize denies everyone.
func (g *Gate) Handler(authorize func(*http.Request) (bool, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reply := func(code int, body any) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(body)
		}
		if authorize == nil {
			reply(http.StatusForbidden, map[string]string{"error": "operator access required"})
			return
		}
		ok, err := authorize(r)
		if err != nil {
			reply(http.StatusServiceUnavailable, map[string]string{"error": "operator authorization unavailable"})
			return
		}
		if !ok {
			reply(http.StatusForbidden, map[string]string{"error": "operator access required"})
			return
		}
		action := strings.TrimPrefix(r.URL.Path, adminPrefix)
		var run func(context.Context) error
		switch {
		case action == "status" && r.Method == http.MethodGet:
		case r.Method != http.MethodPost:
			reply(http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		case action == "switch":
			run = g.Switch
		case action == "open":
			run = g.Open
		case action == "readonly":
			run = g.EnterReadOnly
		case action == "resume":
			run = g.LeaveReadOnly
		default:
			reply(http.StatusNotFound, map[string]string{"error": "unknown switchover action"})
			return
		}
		if run != nil {
			// An operator disconnecting must not abort a cutover half-way; each action bounds itself.
			if err = run(context.WithoutCancel(r.Context())); err != nil {
				code := http.StatusInternalServerError
				if errors.Is(err, ErrState) {
					code = http.StatusConflict
				}
				reply(code, map[string]any{"error": err.Error(), "state": g.State(), "switched": g.Switched()})
				return
			}
		}
		reply(http.StatusOK, map[string]any{"state": g.State(), "switched": g.Switched()})
	})
}

// BearerToken authorizes callers that present the shared operator token; an empty token denies everyone.
// The worker process has no accounts, so its loopback operations listener uses this instead of OperatorOnly.
func BearerToken(token string) func(*http.Request) (bool, error) {
	return func(r *http.Request) (bool, error) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		return ok && token != "" && subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1, nil
	}
}
