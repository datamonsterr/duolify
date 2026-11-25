//go:build integration && edge_conformance

package sharing_test

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
	"github.com/jackc/pgx/v5/pgxpool"
)

type edgeFixtureAccounts struct{ blocked string }

func (edgeFixtureAccounts) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	http.NotFound(w, r)
}

func (edgeFixtureAccounts) Protect(next http.Handler) http.Handler { return next }

func (a edgeFixtureAccounts) Optional(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, _ := r.Cookie("fixture_session")
		if (cookie != nil && cookie.Value == "blocked") || r.Header.Get("Authorization") == "Bearer fixture-blocked" {
			r = r.WithContext(accounts.WithLearnerID(r.Context(), a.blocked))
		}
		next.ServeHTTP(w, r)
	})
}

func TestEdgePublicRead(t *testing.T) {
	f := newSharingFixture(t)
	const secret = "test-only-edge-origin-secret"
	r := startRedis(t)
	s := marketService(t, f, &capture{}, r, func(c *sharing.Config) {
		c.MarketplaceCacheTTLSeconds = 2
		c.EdgeOriginSecret = secret
		c.PreviewRateLimitPerIP = 8
	})
	owner := marketOwner(t, f, "edge-owner")
	course := marketCourse(t, f, owner, "Astronomy edge fixture", "A public outline", 3)
	listCourse(t, s, owner, course, "science", "en", "moon")
	token, err := s.PublishLink(t.Context(), owner, course)
	if err != nil {
		t.Fatal(err)
	}
	second := marketCourse(t, f, owner, "Geology edge fixture", "Another public outline", 3)
	listCourse(t, s, owner, second, "science", "en")
	revocable, err := s.PublishLink(t.Context(), owner, second)
	if err != nil {
		t.Fatal(err)
	}
	privateCourse := marketCourse(t, f, owner, "Private edge fixture", "A private outline", 3)
	moderatedOwner := marketOwner(t, f, "edge-moderated-owner")
	moderated := marketCourse(t, f, moderatedOwner, "moderationfixture", "A reported outline", 3)
	listCourse(t, s, moderatedOwner, moderated, "tech", "en")
	blocked := newLearner(t, f, "Edge blocked viewer")
	seedBlockedLearner(t, f, owner, blocked)
	hyperdriveModerated := marketCourse(t, f, moderatedOwner, "hyperdrivemoderationfixture", "Another reported outline", 3)
	listCourse(t, s, moderatedOwner, hyperdriveModerated, "tech", "en")
	for i, id := range []string{course, second, moderated, hyperdriveModerated} {
		_, err = f.DB.Pool.Exec(t.Context(), "UPDATE marketplace_listings SET published_at = timestamptz '2026-11-20 19:20:21.123456+00' + $2::int * interval '1 microsecond' WHERE course_id = $1", id, i)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.DB.Pool.Exec(t.Context(), "UPDATE marketplace_listing_stats SET rating_sum = $2::bigint, rating_count = 1, popular_score = 4 - $2::bigint, enrollments_30d = 1 WHERE course_id = $1", id, i+1)
		if err != nil {
			t.Fatal(err)
		}
	}
	readerDSN := provisionEdgeReader(t, f.DB.Pool)
	reporterIDs := reporters(t, f, 3)
	t.Setenv("API_ALLOWED_ORIGINS", "https://one.example.test,https://two.example.test")
	auth := edgeFixtureAccounts{blocked: blocked}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	api := httpapi.New(httpapi.Dependencies{Sharing: s, Accounts: auth}, logger)
	disabled, _ := enrollService(t, f, &capture{}, nil)
	off := httpapi.New(httpapi.Dependencies{Sharing: disabled, Accounts: auth}, logger)
	var marketOff atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("POST /__test/market-off", func(w http.ResponseWriter, _ *http.Request) {
		marketOff.Store(true)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /__test/market-on", func(w http.ResponseWriter, _ *http.Request) {
		marketOff.Store(false)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /__test/rotate", func(w http.ResponseWriter, r *http.Request) {
		token, err := s.RotateLink(r.Context(), owner, second)
		if err != nil {
			t.Error(err)
			http.Error(w, "rotate fixture link", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err = json.NewEncoder(w).Encode(map[string]string{"token": token}); err != nil {
			t.Error(err)
		}
	})
	mux.HandleFunc("POST /__test/revoke", func(w http.ResponseWriter, r *http.Request) {
		if err := s.RevokeLink(r.Context(), owner, second); err != nil {
			t.Error(err)
			http.Error(w, "revoke fixture link", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	hide := func(id string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			for _, reporterID := range reporterIDs {
				if _, err := s.Report(r.Context(), reporterID, sharing.ReportInput{TargetType: "course", TargetID: id, Reason: "unsafe"}); err != nil {
					t.Errorf("report seeded course: %v", err)
					http.Error(w, "hide fixture listing", http.StatusInternalServerError)
					return
				}
			}
			w.WriteHeader(http.StatusNoContent)
		}
	}
	mux.HandleFunc("POST /__test/hide", hide(moderated))
	mux.HandleFunc("POST /__test/hide-hyperdrive", hide(hyperdriveModerated))
	var calls atomic.Uint64
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Keep the deliberately low rate-limit case independent of conformance probes.
		r.RemoteAddr = fmt.Sprintf("192.0.2.%d:4191", 100+calls.Add(1)%100)
		if marketOff.Load() {
			off.ServeHTTP(w, r)
			return
		}
		api.ServeHTTP(w, r)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	fixture, err := json.Marshal(map[string]string{
		"course": course, "privateCourse": privateCourse, "moderated": moderated, "share": token, "revocable": revocable,
		"hyperdriveModerated": hyperdriveModerated,
	})
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"exec", "vitest", "run"}
	if path := os.Getenv("EDGE_TEST_PATH"); path != "" {
		args = append(args, path)
	}
	cmd := exec.CommandContext(t.Context(), "pnpm", args...)
	cmd.Dir = "../../../../edge"
	cmd.Env = append(os.Environ(), "EDGE_FIXTURE="+string(fixture),
		"EDGE_FIXTURE_ORIGIN="+server.URL, "EDGE_FIXTURE_SECRET="+secret,
		"CLOUDFLARE_HYPERDRIVE_LOCAL_CONNECTION_STRING_MARKETPLACE_DB="+readerDSN)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
}

// The test applies the same administrator runbook used by a deployment, then gives the
// isolated test role a synthetic password for the local workerd Hyperdrive connection.
func provisionEdgeReader(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	sql, err := os.ReadFile("../../../../infra/edge/provision-marketplace-reader.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(t.Context(), string(sql)); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(t.Context(), "ALTER ROLE duolify_edge_ro LOGIN PASSWORD 'synthetic-only'"); err != nil {
		t.Fatal(err)
	}
	dsn := *sharingURL
	dsn.Path = "/" + pool.Config().ConnConfig.Database
	dsn.User = url.UserPassword("duolify_edge_ro", "synthetic-only")
	return dsn.String()
}
