package social

import (
	"errors"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// serveFriends handles friend, request and link routes; false means no route matched.
func (s *Service) serveFriends(w http.ResponseWriter, r *http.Request, actor string, unavailable func(error)) bool {
	fail := func(err error) {
		var rej *Rejection
		if errors.As(err, &rej) {
			writeRejection(w, rej)
			return
		}
		unavailable(err)
	}
	id := func(raw string) (string, bool) {
		parsed, err := uuid.Parse(raw)
		return parsed.String(), err == nil
	}
	path := r.URL.Path
	switch {
	case path == "/v1/social/friend-requests" && r.Method == http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 2048)
		username, token, err := parseSendBody(r.Body)
		if err != nil {
			writeRejection(w, &Rejection{Code: CodeRequestUnavailable})
			return true
		}
		res, err := s.SendRequest(r.Context(), actor, username, token)
		if err != nil {
			fail(err)
			return true
		}
		write(w, 200, res)
	case path == "/v1/social/friend-requests" && r.Method == http.MethodGet:
		in, out, err := s.Requests(r.Context(), actor)
		if err != nil {
			fail(err)
			return true
		}
		write(w, 200, map[string]any{"incoming": in, "outgoing": out})
	case strings.HasPrefix(path, "/v1/social/friend-requests/"):
		parts := strings.Split(strings.TrimPrefix(path, "/v1/social/friend-requests/"), "/")
		requestID, ok := id(parts[0])
		switch {
		case len(parts) == 1 && r.Method == http.MethodDelete:
			if ok {
				if err := s.Cancel(r.Context(), actor, requestID); err != nil {
					fail(err)
					return true
				}
			}
			write(w, 200, Result{Status: "cancelled"})
		case len(parts) == 2 && r.Method == http.MethodPost && (parts[1] == "accept" || parts[1] == "decline"):
			if !ok {
				writeRejection(w, &Rejection{Code: CodeRequestUnavailable})
				return true
			}
			act := s.Accept
			if parts[1] == "decline" {
				act = s.Decline
			}
			res, err := act(r.Context(), actor, requestID)
			if err != nil {
				fail(err)
				return true
			}
			write(w, 200, res)
		default:
			return false
		}
	case path == "/v1/social/friends" && r.Method == http.MethodGet:
		list, err := s.Friends(r.Context(), actor)
		if err != nil {
			fail(err)
			return true
		}
		write(w, 200, map[string]any{"friends": list})
	case strings.HasPrefix(path, "/v1/social/friends/") && r.Method == http.MethodDelete:
		friendshipID, ok := id(strings.TrimPrefix(path, "/v1/social/friends/"))
		if !ok {
			write(w, 200, Result{Status: statusRemoved})
			return true
		}
		res, err := s.Remove(r.Context(), actor, friendshipID)
		if err != nil {
			fail(err)
			return true
		}
		write(w, 200, res)
	case path == "/v1/social/friend-link" && r.Method == http.MethodGet:
		st, err := s.LinkStatus(r.Context(), actor)
		if err != nil {
			fail(err)
			return true
		}
		write(w, 200, st)
	case path == "/v1/social/friend-link" && r.Method == http.MethodPost:
		token, expires, err := s.CreateLink(r.Context(), actor)
		if err != nil {
			fail(err)
			return true
		}
		write(w, 200, map[string]any{"token": token, "expires_at": expires.UTC(), "valid_days": int(math.Round(time.Until(expires).Hours() / 24))})
	case path == "/v1/social/friend-link" && r.Method == http.MethodDelete:
		if err := s.RevokeLink(r.Context(), actor); err != nil {
			fail(err)
			return true
		}
		write(w, 200, LinkState{})
	case path == "/v1/social/friend-link/resolve" && r.Method == http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 2048)
		username, token, err := parseSendBody(r.Body)
		if err != nil || username != "" {
			writeRejection(w, &Rejection{Code: CodeInviteUnavailable})
			return true
		}
		who, err := s.ResolveLink(r.Context(), actor, token)
		if err != nil {
			fail(err)
			return true
		}
		write(w, 200, who)
	default:
		return false
	}
	return true
}
