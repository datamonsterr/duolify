package courses

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/jackc/pgx/v5"
)

// Outline stores order and parenthood through ordered containment, never indexes as IDs.
type Outline []OutlineUnit

// OutlineUnit keeps its identity through moves and title changes.
type OutlineUnit struct {
	ID      string          `json:"id"`
	Title   string          `json:"title"`
	Lessons []OutlineLesson `json:"lessons"`
}

// OutlineLesson is a content-free Lesson stub, with learner-editable goal.
type OutlineLesson struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Goal  string `json:"goal"`
}

var outlineID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

// Validate allows empty intermediate containers, but confirmation needs complete units.
func (o Outline) Validate(confirm bool) error {
	invalid := errors.New("invalid outline: check IDs, nonempty titles/goals and size limits")
	if o == nil || len(o) > 10 || (confirm && len(o) == 0) {
		return invalid
	}
	seen := map[string]bool{}
	identity := func(id string) bool {
		if !outlineID.MatchString(id) || seen[id] {
			return false
		}
		seen[id] = true
		return true
	}
	text := func(v string, limit int) bool {
		return utf8.ValidString(v) && strings.TrimSpace(v) != "" && !strings.ContainsRune(v, 0) && utf8.RuneCountInString(v) <= limit
	}
	for _, u := range o {
		if !identity(u.ID) || !text(u.Title, 500) || u.Lessons == nil || len(u.Lessons) > 20 || (confirm && len(u.Lessons) == 0) {
			return invalid
		}
		for _, l := range u.Lessons {
			if !identity(l.ID) || !text(l.Title, 500) || !text(l.Goal, 4000) {
				return invalid
			}
		}
	}
	return nil
}

// Hash identifies the exact canonical content (including IDs and order).
func (o Outline) Hash() string {
	raw, _ := json.Marshal(o)
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

// OutlineSnapshot is one pre-edit state; the same history accepts future applied proposals.
type OutlineSnapshot struct {
	Value   Outline `json:"value"`
	FocusID string  `json:"focusId"`
}

// ConfirmedOutline is the exact revision handoff to creation/proposal consumers.
type ConfirmedOutline struct {
	DraftID     string  `json:"draft_id"`
	Revision    int64   `json:"revision"`
	ContentHash string  `json:"content_hash"`
	Outline     Outline `json:"outline"`
}

// OutlineDraft is an owned mutable head with bounded persistent undo and confirmation.
type OutlineDraft struct {
	ID            string            `json:"id"`
	BriefRevision string            `json:"brief_revision"`
	Revision      int64             `json:"revision"`
	Outline       Outline           `json:"outline"`
	History       []OutlineSnapshot `json:"history"`
	Confirmed     *ConfirmedOutline `json:"confirmed"`
}

type outlineInput struct {
	BriefRevision    string            `json:"brief_revision"`
	ExpectedRevision int64             `json:"expected_revision"`
	Outline          Outline           `json:"outline"`
	History          []OutlineSnapshot `json:"history"`
}

func (in outlineInput) validate() error {
	if err := in.Outline.Validate(false); err != nil {
		return err
	}
	if len(in.History) > 50 {
		return errors.New("undo history exceeds 50 edits")
	}
	for _, h := range in.History {
		if err := h.Value.Validate(false); err != nil {
			return err
		}
		if h.FocusID != "" && !outlineID.MatchString(h.FocusID) {
			return errors.New("invalid focus ID")
		}
	}
	return nil
}

func decodeOutline(w http.ResponseWriter, r *http.Request, out any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return false
	}
	var extra any
	return d.Decode(&extra) == io.EOF
}

func outlineError(w http.ResponseWriter, code int) {
	respond(w, code, map[string]string{"error": "Outline unavailable. Your edits are kept; check titles and goals, then retry."})
}

// ServeOutlineHTTP runs behind authentication and has no Engine or Credits dependency.
func (s *Service) ServeOutlineHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	owner := accounts.LearnerID(r.Context())
	if owner == "" {
		outlineError(w, 401)
		return
	}
	if s.opts.Pool == nil {
		outlineError(w, 503)
		return
	}
	tail := strings.TrimPrefix(r.URL.Path, "/v1/outline-drafts")
	if tail == "" && r.Method == "POST" {
		s.createOutline(w, r, owner)
		return
	}
	if tail == "" && r.Method == "GET" {
		var id string
		err := s.opts.Pool.QueryRow(r.Context(), `SELECT d.id::text FROM outline_drafts d JOIN course_brief_revisions b ON b.id=d.brief_revision WHERE d.learner_id::text=$1 AND d.brief_revision::text=$2 AND NOT d.is_recovery AND b.expires_at>now() ORDER BY d.updated_at DESC,d.id DESC LIMIT 1`, owner, r.URL.Query().Get("brief_revision")).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			respond(w, 200, map[string]any{"draft": nil})
			return
		}
		if err != nil {
			outlineError(w, 503)
			return
		}
		d, err := s.readOutline(r.Context(), owner, id)
		if err != nil {
			outlineError(w, 404)
			return
		}
		respond(w, 200, map[string]any{"draft": d})
		return
	}
	parts := strings.Split(strings.Trim(tail, "/"), "/")
	if len(parts) > 2 || parts[0] == "" {
		outlineError(w, 404)
		return
	}
	id := parts[0]
	if len(parts) == 2 {
		switch {
		case parts[1] == "confirm" && r.Method == "POST":
			s.confirmOutline(w, r, owner, id)
		case parts[1] == "confirmed" && r.Method == "GET":
			rev, err := strconv.ParseInt(r.URL.Query().Get("expected_revision"), 10, 64)
			if err != nil {
				outlineError(w, 400)
				return
			}
			c, err := s.ResolveConfirmed(r.Context(), owner, id, rev)
			if err != nil {
				outlineError(w, 409)
				return
			}
			respond(w, 200, c)
		default:
			outlineError(w, 404)
		}
		return
	}
	switch r.Method {
	case "GET":
		d, err := s.readOutline(r.Context(), owner, id)
		if err != nil {
			outlineError(w, 404)
			return
		}
		respond(w, 200, d)
	case "PUT":
		s.saveOutline(w, r, owner, id)
	case "DELETE":
		tag, err := s.opts.Pool.Exec(r.Context(), "DELETE FROM outline_drafts WHERE id::text=$1 AND learner_id::text=$2", id, owner)
		if err != nil {
			outlineError(w, 503)
			return
		}
		if tag.RowsAffected() != 1 {
			outlineError(w, 404)
			return
		}
		w.WriteHeader(204)
	default:
		outlineError(w, 404)
	}
}

const outlineColumns = `d.id::text,d.brief_revision::text,d.revision,d.outline,d.history,d.confirmed`

func scanOutline(row pgx.Row) (OutlineDraft, error) {
	var d OutlineDraft
	var raw, history, confirmed []byte
	err := row.Scan(&d.ID, &d.BriefRevision, &d.Revision, &raw, &history, &confirmed)
	if err != nil {
		return d, err
	}
	if err = json.Unmarshal(raw, &d.Outline); err != nil {
		return d, err
	}
	if err = json.Unmarshal(history, &d.History); err != nil {
		return d, err
	}
	if len(confirmed) > 0 {
		err = json.Unmarshal(confirmed, &d.Confirmed)
	}
	return d, err
}

func (s *Service) readOutline(ctx context.Context, owner, id string) (OutlineDraft, error) {
	return scanOutline(s.opts.Pool.QueryRow(ctx, `SELECT `+outlineColumns+` FROM outline_drafts d JOIN course_brief_revisions b ON b.id=d.brief_revision WHERE d.id::text=$1 AND d.learner_id::text=$2 AND b.expires_at>now()`, id, owner))
}

func (s *Service) createOutline(w http.ResponseWriter, r *http.Request, owner string) {
	var in outlineInput
	if !decodeOutline(w, r, &in) || in.validate() != nil || in.ExpectedRevision != 0 {
		outlineError(w, 400)
		return
	}
	if !s.limit(r.Context(), "outline-write:"+owner, 120, time.Hour) {
		outlineError(w, 429)
		return
	}
	raw, _ := json.Marshal(in.Outline)
	if in.History == nil {
		in.History = []OutlineSnapshot{}
	}
	history, _ := json.Marshal(in.History)
	d, err := scanOutline(s.opts.Pool.QueryRow(r.Context(), `INSERT INTO outline_drafts AS d(learner_id,brief_revision,outline,history) SELECT learner_id,id,$3,$4 FROM course_brief_revisions WHERE id::text=$1 AND learner_id::text=$2 AND expires_at>now() RETURNING `+outlineColumns, in.BriefRevision, owner, raw, history))
	if err != nil {
		outlineError(w, 404)
		return
	}
	respond(w, 201, d)
}

func (s *Service) saveOutline(w http.ResponseWriter, r *http.Request, owner, id string) {
	var in outlineInput
	if !decodeOutline(w, r, &in) || in.validate() != nil || in.ExpectedRevision < 1 || in.BriefRevision != "" {
		outlineError(w, 400)
		return
	}
	if !s.limit(r.Context(), "outline-write:"+owner, 120, time.Hour) {
		outlineError(w, 429)
		return
	}
	ctx := r.Context()
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		outlineError(w, 503)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := scanOutline(tx.QueryRow(ctx, `SELECT `+outlineColumns+` FROM outline_drafts d JOIN course_brief_revisions b ON b.id=d.brief_revision WHERE d.id::text=$1 AND d.learner_id::text=$2 AND b.expires_at>now() FOR UPDATE OF d`, id, owner))
	if err != nil {
		outlineError(w, 404)
		return
	}
	raw, _ := json.Marshal(in.Outline)
	if in.History == nil {
		in.History = []OutlineSnapshot{}
	}
	history, _ := json.Marshal(in.History)
	if current.Revision != in.ExpectedRevision {
		recovery, err := scanOutline(tx.QueryRow(ctx, `INSERT INTO outline_drafts AS d(learner_id,brief_revision,outline,history,is_recovery) VALUES($1::uuid,$2::uuid,$3,$4,true) RETURNING `+outlineColumns, owner, current.BriefRevision, raw, history))
		if err != nil {
			outlineError(w, 503)
			return
		}
		if tx.Commit(ctx) != nil {
			outlineError(w, 503)
			return
		}
		respond(w, 409, map[string]any{"error": "A newer outline is saved. Both drafts are kept; choose which to continue.", "current": current, "recovery": recovery})
		return
	}
	d, err := scanOutline(tx.QueryRow(ctx, `UPDATE outline_drafts AS d SET outline=$3,history=$4,revision=revision+1,confirmed=NULL,is_recovery=false,updated_at=now() WHERE id::text=$1 AND learner_id::text=$2 RETURNING `+outlineColumns, id, owner, raw, history))
	if err != nil {
		outlineError(w, 503)
		return
	}
	if tx.Commit(ctx) != nil {
		outlineError(w, 503)
		return
	}
	respond(w, 200, d)
}

func (s *Service) confirmOutline(w http.ResponseWriter, r *http.Request, owner, id string) {
	var in struct {
		ExpectedRevision int64 `json:"expected_revision"`
	}
	if !decodeOutline(w, r, &in) || in.ExpectedRevision < 1 {
		outlineError(w, 400)
		return
	}
	ctx := r.Context()
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		outlineError(w, 503)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	d, err := scanOutline(tx.QueryRow(ctx, `SELECT `+outlineColumns+` FROM outline_drafts d JOIN course_brief_revisions b ON b.id=d.brief_revision WHERE d.id::text=$1 AND d.learner_id::text=$2 AND b.expires_at>now() FOR UPDATE OF d`, id, owner))
	if err != nil {
		outlineError(w, 404)
		return
	}
	if d.Revision != in.ExpectedRevision {
		respond(w, 409, map[string]any{"error": "A newer outline is saved. Reload it before confirming.", "current": d})
		return
	}
	if d.Outline.Validate(true) != nil {
		outlineError(w, 400)
		return
	}
	c := ConfirmedOutline{DraftID: d.ID, Revision: d.Revision, ContentHash: d.Outline.Hash(), Outline: d.Outline}
	raw, _ := json.Marshal(c)
	if _, err = tx.Exec(ctx, `UPDATE outline_drafts SET confirmed=$2 WHERE id::text=$1`, id, raw); err != nil {
		outlineError(w, 503)
		return
	}
	if tx.Commit(ctx) != nil {
		outlineError(w, 503)
		return
	}
	respond(w, 200, c)
}

// ResolveConfirmed rejects a changed head; future consumers must use this exact revision.
func (s *Service) ResolveConfirmed(ctx context.Context, owner, id string, expected int64) (ConfirmedOutline, error) {
	d, err := s.readOutline(ctx, owner, id)
	if err != nil {
		return ConfirmedOutline{}, err
	}
	if d.Confirmed == nil || d.Revision != expected || d.Confirmed.Revision != expected || d.Confirmed.ContentHash != d.Outline.Hash() {
		return ConfirmedOutline{}, errors.New("confirmed outline revision required")
	}
	return *d.Confirmed, nil
}
