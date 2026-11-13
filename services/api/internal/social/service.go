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
	"strings"
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
	err := s.pool.QueryRow(ctx, `SELECT version,reserved,profanity FROM username_config WHERE id`).Scan(&c.Version, &c.Reserved, &c.Profanity)
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
	out, err := s.claim(ctx, learnerID, name)
	var rejection *Rejection
	switch {
	case errors.As(err, &rejection):
		s.emit(learnerID, "username_rejected", map[string]any{"reason_code": rejection.Code})
	case err == nil:
		s.emit(learnerID, "username_set", map[string]any{"username_length": utf8.RuneCountInString(out.Username)})
	}
	return out, err
}

func (s *Service) claim(ctx context.Context, learnerID, name string) (Identity, error) {
	if r := Validate(name); r != nil {
		return Identity{}, r
	}
	cfg, err := s.config(ctx)
	if err != nil {
		return Identity{}, err
	}
	if r := cfg.Check(name); r != nil {
		return Identity{}, r
	}
	display, canonical := Normalize(name), Canonical(name)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Identity{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var owner string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM learners WHERE id=$1::uuid FOR UPDATE`, learnerID).Scan(&owner); err != nil {
		return Identity{}, err
	}
	var has bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM learner_usernames WHERE learner_id=$1::uuid)`, learnerID).Scan(&has); err != nil {
		return Identity{}, err
	}
	if has {
		return Identity{}, &Rejection{Code: CodeAlreadySet}
	}
	if _, err = tx.Exec(ctx, `DELETE FROM username_holds WHERE canonical=$1 AND held_until<=now()`, canonical); err != nil {
		return Identity{}, err
	}
	var holder string
	switch err = tx.QueryRow(ctx, `SELECT previous_owner_ref FROM username_holds WHERE canonical=$1`, canonical).Scan(&holder); {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return Identity{}, err
	case holder != ownerRef(learnerID):
		return Identity{}, &Rejection{Code: CodeTaken}
	default:
		if _, err = tx.Exec(ctx, `DELETE FROM username_holds WHERE canonical=$1`, canonical); err != nil {
			return Identity{}, err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO learner_usernames(learner_id,username) VALUES($1::uuid,$2)`, learnerID, display); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" {
			return Identity{}, &Rejection{Code: CodeTaken}
		}
		return Identity{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO learner_social_settings(learner_id) VALUES($1::uuid) ON CONFLICT DO NOTHING`, learnerID); err != nil {
		return Identity{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Identity{}, err
	}
	return s.own(ctx, learnerID)
}

func (s *Service) row(ctx context.Context, where string, arg any) (identityRow, error) {
	var r identityRow
	err := s.pool.QueryRow(ctx, `SELECT u.learner_id::text,u.username,l.display_name,COALESCE(ss.show_streak_to_friends,true)
 FROM learner_usernames u JOIN learners l ON l.id=u.learner_id LEFT JOIN learner_social_settings ss ON ss.learner_id=u.learner_id WHERE `+where, arg).
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

// Profile returns the public projection by exact username, or pgx.ErrNoRows.
func (s *Service) Profile(ctx context.Context, username string) (Identity, error) {
	r, err := s.row(ctx, "u.canonical=$1", Canonical(username))
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
	case r.URL.Path == "/v1/social/username" && r.Method == http.MethodPut:
		var in struct {
			Username string `json:"username"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		var extra any
		if err := d.Decode(&in); err != nil || !errors.Is(d.Decode(&extra), io.EOF) {
			rej := &Rejection{Code: CodeInvalid}
			write(w, rej.Status(), map[string]string{"error": rej.Error(), "code": rej.Code})
			return
		}
		id, err := s.Claim(r.Context(), actor, in.Username)
		var rej *Rejection
		switch {
		case errors.As(err, &rej):
			write(w, rej.Status(), map[string]string{"error": rej.Error(), "code": rej.Code})
		case err != nil:
			unavailable(err)
		default:
			write(w, 200, response(&id))
		}
	case strings.HasPrefix(r.URL.Path, "/v1/social/profiles/") && r.Method == http.MethodGet:
		id, err := s.Profile(r.Context(), strings.TrimPrefix(r.URL.Path, "/v1/social/profiles/"))
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
