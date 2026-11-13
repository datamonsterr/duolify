package social

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Service claims usernames and serves the minimal public identity.
type Service struct {
	pool    *pgxpool.Pool
	events  analytics.Events
	streaks StreakSource
}

// New creates the service behind the central fail-closed consent gate.
func New(pool *pgxpool.Pool, events analytics.Events) *Service {
	if events != nil {
		events = analytics.WithConsent(events, analytics.DatabaseConsent(pool))
	}
	return &Service{pool: pool, events: events}
}

// WithStreaks attaches the streak source used by the privacy-aware projection.
func (s *Service) WithStreaks(source StreakSource) *Service { s.streaks = source; return s }

func (s *Service) config(ctx context.Context) (Config, error) {
	var c Config
	err := s.pool.QueryRow(ctx, `SELECT version,reserved,profanity,hold_days,rename_cooldown_days,search_max_results,search_per_minute,name_attempts_per_hour FROM username_config WHERE id`).
		Scan(&c.Version, &c.Reserved, &c.Profanity, &c.HoldDays, &c.RenameCooldownDays, &c.SearchMax, &c.SearchPerMinute, &c.AttemptsPerHour)
	return c, err
}

func ownerRef(learnerID string) string {
	sum := sha256.Sum256([]byte(learnerID))
	return hex.EncodeToString(sum[:])
}

func (s *Service) emit(learnerID, event string, props map[string]any) {
	if s.events == nil {
		return
	}
	props["source"], props["platform"], props["app_version"] = "api", "web", "0.1.0"
	if err := s.events.Capture(learnerID, event, props); err != nil {
		slog.Error("social analytics delivery failed", "error", err)
	}
}

// Claim gives a learner their first username. Exactly one concurrent claimant of a
// canonical name wins: the Postgres unique index decides, never application code.
func (s *Service) Claim(ctx context.Context, learnerID, name string) (Identity, error) {
	out, err := s.change(ctx, learnerID, name, false)
	if err == nil {
		s.emit(learnerID, "username_set", map[string]any{"username_length": utf8.RuneCountInString(out.Username)})
	}
	return out.Identity, err
}

// Rename changes a learner's username: once per cooldown, the old name held for its
// previous owner. References elsewhere use the learner ID and are untouched.
func (s *Service) Rename(ctx context.Context, learnerID, name string) (Identity, error) {
	out, err := s.change(ctx, learnerID, name, true)
	if err == nil && out.changed {
		s.emit(learnerID, "username_changed", map[string]any{"username_length": utf8.RuneCountInString(out.Username)})
	}
	return out.Identity, err
}

// change rate-limits the attempt first, so a throttled learner learns nothing about
// any name, then reports reason codes for refusals that the catalogue allows.
func (s *Service) change(ctx context.Context, learnerID, name string, rename bool) (renamed, error) {
	cfg, err := s.config(ctx)
	if err != nil {
		return renamed{}, err
	}
	if rej, err := s.allow(ctx, learnerID, bucketAttempt, cfg.AttemptsPerHour, attemptWindow); err != nil || rej != nil {
		if rej != nil {
			return renamed{}, rej
		}
		return renamed{}, err
	}
	out, err := s.apply(ctx, cfg, learnerID, name, rename)
	var rejection *Rejection
	if errors.As(err, &rejection) {
		switch rejection.Code {
		case CodeInvalid, CodeTaken, CodeReserved, CodeNotAllowed, CodeAlreadySet:
			s.emit(learnerID, "username_rejected", map[string]any{"reason_code": rejection.Code})
		}
	}
	return out, err
}

type renamed struct {
	Identity
	changed bool
}

func (s *Service) apply(ctx context.Context, cfg Config, learnerID, name string, rename bool) (renamed, error) {
	if r := Validate(name); r != nil {
		return renamed{}, r
	}
	if r := cfg.Check(name); r != nil {
		return renamed{}, r
	}
	display, canonical := Normalize(name), Canonical(name)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return renamed{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var owner string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM learners WHERE id=$1::uuid FOR UPDATE`, learnerID).Scan(&owner); err != nil {
		return renamed{}, err
	}
	var current string
	var coolingDown bool
	var cooldownEnds *time.Time
	has := true
	err = tx.QueryRow(ctx, `SELECT username, COALESCE(changed_at+make_interval(days=>$2)>now(),false), changed_at+make_interval(days=>$2) FROM learner_usernames WHERE learner_id=$1::uuid`, learnerID, cfg.RenameCooldownDays).
		Scan(&current, &coolingDown, &cooldownEnds)
	if errors.Is(err, pgx.ErrNoRows) {
		has = false
	} else if err != nil {
		return renamed{}, err
	}
	switch {
	case !rename && has:
		return renamed{}, &Rejection{Code: CodeAlreadySet}
	case rename && !has:
		return renamed{}, &Rejection{Code: CodeNotSet}
	case rename && current == display:
		id, err := s.own(ctx, learnerID)
		return renamed{Identity: id}, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM username_holds WHERE canonical=$1 AND held_until<=now()`, canonical); err != nil {
		return renamed{}, err
	}
	var holder string
	reclaim := false
	switch err = tx.QueryRow(ctx, `SELECT previous_owner_ref FROM username_holds WHERE canonical=$1`, canonical).Scan(&holder); {
	case errors.Is(err, pgx.ErrNoRows):
		holder = ""
	case err != nil:
		return renamed{}, err
	default:
		reclaim = holder == ownerRef(learnerID)
	}
	// The previous owner may take their own held name back without waiting; they
	// start a fresh cooldown. Everyone else sees the cooldown before any name check.
	if rename && coolingDown && !reclaim {
		return renamed{}, &Rejection{Code: CodeCooldown, NextChangeAt: *cooldownEnds}
	}
	if holder != "" {
		if !reclaim {
			return renamed{}, &Rejection{Code: CodeTaken}
		}
		if _, err = tx.Exec(ctx, `DELETE FROM username_holds WHERE canonical=$1`, canonical); err != nil {
			return renamed{}, err
		}
	}
	if rename {
		_, err = tx.Exec(ctx, `UPDATE learner_usernames SET username=$2, changed_at=now() WHERE learner_id=$1::uuid`, learnerID, display)
	} else {
		_, err = tx.Exec(ctx, `INSERT INTO learner_usernames(learner_id,username) VALUES($1::uuid,$2)`, learnerID, display)
	}
	if err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" {
			return renamed{}, &Rejection{Code: CodeTaken}
		}
		return renamed{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO learner_social_settings(learner_id) VALUES($1::uuid) ON CONFLICT DO NOTHING`, learnerID); err != nil {
		return renamed{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return renamed{}, err
	}
	id, err := s.own(ctx, learnerID)
	return renamed{Identity: id, changed: true}, err
}

func (s *Service) row(ctx context.Context, where string, args ...any) (identityRow, error) {
	var r identityRow
	err := s.pool.QueryRow(ctx, `SELECT u.learner_id::text,u.username,l.display_name,COALESCE(ss.show_streak_to_friends,true)
 FROM learner_usernames u JOIN learners l ON l.id=u.learner_id LEFT JOIN learner_social_settings ss ON ss.learner_id=u.learner_id WHERE `+where, args...).
		Scan(&r.LearnerID, &r.Username, &r.DisplayName, &r.ShowStreakToFriends)
	return r, err
}

func (s *Service) own(ctx context.Context, learnerID string) (Identity, error) {
	r, err := s.row(ctx, "u.learner_id=$1::uuid", learnerID)
	if err != nil {
		return Identity{}, err
	}
	out := r.Project(s.streaks, true)
	return out, nil
}

// Profile returns the public projection by exact username, or pgx.ErrNoRows. It
// follows the exact-search visibility rule, so a hidden learner looks nonexistent.
func (s *Service) Profile(ctx context.Context, viewerID, username string) (Identity, error) {
	r, err := s.row(ctx, "u.canonical=$2 AND (u.learner_id=$1::uuid OR "+exactVisible+")"+blockedPairClause, viewerID, Canonical(username))
	if err != nil {
		return Identity{}, err
	}
	return r.Project(s.streaks, false), nil
}

type identityResponse struct {
	Username *Identity `json:"username"`
	Rules    struct {
		Min int `json:"min"`
		Max int `json:"max"`
	} `json:"rules"`
}

func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("social response failed", "error", err)
	}
}

func writeRejection(w http.ResponseWriter, rej *Rejection) {
	body := map[string]any{"error": rej.Error(), "code": rej.Code}
	switch rej.Code {
	case CodeCooldown:
		body["next_change_at"] = rej.NextChangeAt.UTC().Format(time.RFC3339)
	case CodeRateLimited:
		w.Header().Set("Retry-After", strconv.Itoa(rej.RetryAfterSeconds()))
	}
	write(w, rej.Status(), body)
}

func response(id *Identity) identityResponse {
	r := identityResponse{Username: id}
	r.Rules.Min, r.Rules.Max = MinLength, MaxLength
	return r
}

// ServeHTTP serves /v1/social/*. The actor always comes from the session.
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	actor := accounts.LearnerID(r.Context())
	if actor == "" {
		write(w, 401, map[string]string{"error": "Sign in required."})
		return
	}
	unavailable := func(err error) {
		slog.ErrorContext(r.Context(), "social request failed", "error", err)
		write(w, 503, map[string]string{"error": "Social is unavailable. Retry."})
	}
	switch {
	case r.URL.Path == "/v1/social/identity" && r.Method == http.MethodGet:
		id, err := s.own(r.Context(), actor)
		if errors.Is(err, pgx.ErrNoRows) {
			write(w, 200, response(nil))
		} else if err != nil {
			unavailable(err)
		} else {
			write(w, 200, response(&id))
		}
	case r.URL.Path == "/v1/social/username" && (r.Method == http.MethodPut || r.Method == http.MethodPatch):
		var in struct {
			Username string `json:"username"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		var extra any
		if err := d.Decode(&in); err != nil || !errors.Is(d.Decode(&extra), io.EOF) {
			writeRejection(w, &Rejection{Code: CodeInvalid})
			return
		}
		change := s.Claim
		if r.Method == http.MethodPatch {
			change = s.Rename
		}
		id, err := change(r.Context(), actor, in.Username)
		var rej *Rejection
		switch {
		case errors.As(err, &rej):
			writeRejection(w, rej)
		case err != nil:
			unavailable(err)
		default:
			write(w, 200, response(&id))
		}
	case r.URL.Path == "/v1/social/search" && r.Method == http.MethodGet:
		found, err := s.Search(r.Context(), actor, r.URL.Query().Get("q"))
		var rej *Rejection
		switch {
		case errors.As(err, &rej):
			writeRejection(w, rej)
		case err != nil:
			unavailable(err)
		default:
			write(w, 200, map[string]any{"results": found})
		}
	case r.URL.Path == "/v1/social/settings" && r.Method == http.MethodGet:
		cur, err := s.Settings(r.Context(), actor)
		if err != nil {
			unavailable(err)
			return
		}
		write(w, 200, cur)
	case r.URL.Path == "/v1/social/settings" && r.Method == http.MethodPatch:
		patch, err := ParseSettingsPatch(r.Body)
		if err != nil {
			write(w, 422, map[string]string{"error": "Those privacy settings aren't valid.", "code": "settings_invalid"})
			return
		}
		cur, err := s.UpdateSettings(r.Context(), actor, patch)
		if err != nil {
			unavailable(err)
			return
		}
		write(w, 200, cur)
	case strings.HasPrefix(r.URL.Path, "/v1/social/profiles/") && r.Method == http.MethodGet:
		id, err := s.Profile(r.Context(), actor, strings.TrimPrefix(r.URL.Path, "/v1/social/profiles/"))
		if errors.Is(err, pgx.ErrNoRows) {
			write(w, 404, map[string]string{"error": "Nobody found with that username."})
		} else if err != nil {
			unavailable(err)
		} else {
			write(w, 200, id)
		}
	default:
		write(w, 404, map[string]string{"error": "Not found."})
	}
}
