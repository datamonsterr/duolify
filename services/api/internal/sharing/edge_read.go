package sharing

import (
	"crypto/subtle"
	"net/http"
	"strconv"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
)

const (
	edgeSecretHeader   = "X-Duolify-Edge-Secret" // #nosec G101 -- header name only.
	edgeClientIPHeader = "X-Duolify-Edge-Client-IP"
	edgeCacheHeader    = "X-Duolify-Edge-Cache"
	edgeReadAtHeader   = "X-Duolify-Edge-Read-At"
	edgeTTLHeader      = "X-Duolify-Edge-TTL"
)

func (s *Service) trustedEdge(r *http.Request) bool {
	return s != nil && s.cfg.EdgeOriginSecret != "" &&
		subtle.ConstantTimeCompare([]byte(s.cfg.EdgeOriginSecret), []byte(r.Header.Get(edgeSecretHeader))) == 1
}

func (s *Service) edgeMarketplaceTTL(r *http.Request, viewer string) int {
	if !s.trustedEdge(r) || r.Header.Get(edgeCacheHeader) != "1" ||
		viewer != "" || len(r.Header.Values("Cookie")) != 0 || len(r.Header.Values("Authorization")) != 0 {
		return 0
	}
	return s.cfg.MarketplaceCacheTTLSeconds
}

func edgeReadFreshness(w http.ResponseWriter, readAt time.Time, ttl int) {
	if ttl > 0 {
		w.Header().Set(edgeReadAtHeader, strconv.FormatInt(readAt.UnixMilli(), 10))
		w.Header().Set(edgeTTLHeader, strconv.Itoa(ttl))
	}
}

type edgeReadStatus struct {
	http.ResponseWriter
	status int
}

func (w *edgeReadStatus) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// Signed-in reads travel through the existing origin authorization and consent boundary.
// Anonymous requests have only operational request counts.
func (s *Service) withEdgeReadAnalytics(surface string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		viewer := accounts.LearnerID(r.Context())
		if viewer == "" || !s.trustedEdge(r) || r.Method != http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		status := &edgeReadStatus{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(status, r)
		outcome := "unavailable"
		switch status.status {
		case http.StatusOK:
			outcome = "served"
		case http.StatusNotFound:
			outcome = "not_found"
		}
		s.emit(viewer, "edge_public_read", map[string]any{
			"surface": surface, "read_path": "go_origin", "cache_status": "bypass", "outcome": outcome,
		})
	})
}
