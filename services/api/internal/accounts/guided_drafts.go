package accounts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// Guided chat drafts: owner-only resume state for scripted chats. The server never learns the
// script; it only bounds the shape and size of what is stored.
const (
	// MaxGuidedDraftBytes caps the compact answers JSON (16 KB).
	MaxGuidedDraftBytes = 16384
	// GuidedDraftIdleDays is the idle retention before the worker purges a draft.
	GuidedDraftIdleDays = 30
	maxGuidedDrafts     = 50
	maxGuidedAnswers    = 32
)

var (
	guidedScriptID = regexp.MustCompile(`^[a-z]+(\.[a-z_]+)+$`)
	guidedStepID   = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// GuidedDraft is the wire shape of GET and PUT.
type GuidedDraft struct {
	ScriptVersion int             `json:"script_version"`
	StepID        string          `json:"step_id"`
	Answers       json.RawMessage `json:"answers"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

func (s *Service) registerGuidedDrafts() {
	s.mux.Handle("GET /v1/me/guided-drafts/{script_id}", s.Protect(http.HandlerFunc(s.readGuidedDraft)))
	s.mux.Handle("PUT /v1/me/guided-drafts/{script_id}", s.Protect(http.HandlerFunc(s.saveGuidedDraft)))
	s.mux.Handle("DELETE /v1/me/guided-drafts/{script_id}", s.Protect(http.HandlerFunc(s.deleteGuidedDraft)))
}

func etag(t time.Time) string { return `"` + t.UTC().Format(time.RFC3339Nano) + `"` }

func scriptID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("script_id")
	if len(id) > 64 || !guidedScriptID.MatchString(id) {
		respond(w, http.StatusNotFound, map[string]string{"error": "Draft not found."})
		return "", false
	}
	return id, true
}

func (s *Service) loadGuidedDraft(ctx context.Context, learner any, script string) (GuidedDraft, error) {
	var d GuidedDraft
	var answers []byte
	err := s.pool.QueryRow(ctx, `SELECT script_version, step_id, answers, updated_at FROM guided_chat_drafts
 WHERE learner_id=$1 AND script_id=$2 AND expires_at > now()`, learner, script).Scan(&d.ScriptVersion, &d.StepID, &answers, &d.UpdatedAt)
	d.Answers = answers
	return d, err
}

func (s *Service) readGuidedDraft(w http.ResponseWriter, r *http.Request) {
	script, ok := scriptID(w, r)
	if !ok {
		return
	}
	d, err := s.loadGuidedDraft(r.Context(), contextID(r), script)
	if errors.Is(err, pgx.ErrNoRows) {
		respond(w, http.StatusNotFound, map[string]string{"error": "Draft not found."})
		return
	}
	if err != nil {
		respond(w, http.StatusServiceUnavailable, map[string]string{"error": "Could not read the draft."})
		return
	}
	w.Header().Set("ETag", etag(d.UpdatedAt))
	respond(w, http.StatusOK, d)
}

// validGuidedAnswers accepts an object of at most 32 step ids, each holding {value, skipped?}
// where value is null, a string, a number, a boolean or an array of strings.
func validGuidedAnswers(raw json.RawMessage) bool {
	var answers map[string]struct {
		Value   json.RawMessage `json:"value"`
		Skipped *bool           `json:"skipped"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&answers); err != nil || answers == nil || len(answers) > maxGuidedAnswers {
		return false
	}
	for id, a := range answers {
		if !guidedStepID.MatchString(id) || len(id) > 64 || len(a.Value) == 0 {
			return false
		}
		var v any
		if json.Unmarshal(a.Value, &v) != nil {
			return false
		}
		switch x := v.(type) {
		case nil, bool, float64:
		case string:
			if !utf8.ValidString(x) {
				return false
			}
		case []any:
			for _, item := range x {
				if _, isString := item.(string); !isString {
					return false
				}
			}
		default:
			return false
		}
	}
	return true
}

func (s *Service) saveGuidedDraft(w http.ResponseWriter, r *http.Request) {
	script, ok := scriptID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxGuidedDraftBytes+4096)
	var in struct {
		ScriptVersion int             `json:"script_version"`
		StepID        string          `json:"step_id"`
		Answers       json.RawMessage `json:"answers"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var extra any
	if err := dec.Decode(&in); err != nil || !errors.Is(dec.Decode(&extra), io.EOF) {
		respond(w, http.StatusBadRequest, map[string]string{"error": "Invalid draft."})
		return
	}
	var compact bytes.Buffer
	if json.Compact(&compact, in.Answers) != nil || compact.Len() > MaxGuidedDraftBytes {
		respond(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "That draft is too large."})
		return
	}
	if in.ScriptVersion < 1 || len(in.StepID) > 64 || (in.StepID != "" && !guidedStepID.MatchString(in.StepID)) || !validGuidedAnswers(compact.Bytes()) {
		respond(w, http.StatusBadRequest, map[string]string{"error": "Invalid draft."})
		return
	}
	learner := contextID(r)
	ifMatch := r.Header.Get("If-Match")
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		respond(w, http.StatusServiceUnavailable, map[string]string{"error": "Could not save the draft."})
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	// Serialise writers per Learner and script so the If-Match check and the write are atomic.
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1::text || ':' || $2, 0))`, learner, script); err != nil {
		respond(w, http.StatusServiceUnavailable, map[string]string{"error": "Could not save the draft."})
		return
	}
	var current GuidedDraft
	var currentAnswers []byte
	err = tx.QueryRow(r.Context(), `SELECT script_version, step_id, answers, updated_at FROM guided_chat_drafts
 WHERE learner_id=$1 AND script_id=$2 AND expires_at > now() FOR UPDATE`, learner, script).Scan(&current.ScriptVersion, &current.StepID, &currentAnswers, &current.UpdatedAt)
	exists := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		respond(w, http.StatusServiceUnavailable, map[string]string{"error": "Could not save the draft."})
		return
	}
	// Absent row: only an empty If-Match may create. Present row: If-Match must be its ETag.
	if (exists && ifMatch != etag(current.UpdatedAt)) || (!exists && ifMatch != "") {
		if exists {
			current.Answers = currentAnswers
			w.Header().Set("ETag", etag(current.UpdatedAt))
			respond(w, http.StatusConflict, current)
			return
		}
		respond(w, http.StatusConflict, map[string]string{"error": "The draft changed."})
		return
	}
	if !exists {
		var count int
		if err = tx.QueryRow(r.Context(), `SELECT count(*) FROM guided_chat_drafts WHERE learner_id=$1 AND expires_at > now()`, learner).Scan(&count); err != nil || count >= maxGuidedDrafts {
			respond(w, http.StatusTooManyRequests, map[string]string{"error": "Too many drafts."})
			return
		}
	}
	var saved GuidedDraft
	err = tx.QueryRow(r.Context(), `INSERT INTO guided_chat_drafts(learner_id, script_id, script_version, step_id, answers)
 VALUES ($1,$2,$3,$4,$5::jsonb)
 ON CONFLICT (learner_id, script_id) DO UPDATE SET script_version=EXCLUDED.script_version, step_id=EXCLUDED.step_id,
  answers=EXCLUDED.answers, updated_at=clock_timestamp(), expires_at=clock_timestamp() + make_interval(days => $6)
 RETURNING updated_at`, learner, script, in.ScriptVersion, in.StepID, compact.Bytes(), GuidedDraftIdleDays).Scan(&saved.UpdatedAt)
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		respond(w, http.StatusServiceUnavailable, map[string]string{"error": "Could not save the draft."})
		return
	}
	saved.ScriptVersion, saved.StepID, saved.Answers = in.ScriptVersion, in.StepID, compact.Bytes()
	w.Header().Set("ETag", etag(saved.UpdatedAt))
	respond(w, http.StatusOK, saved)
}

func (s *Service) deleteGuidedDraft(w http.ResponseWriter, r *http.Request) {
	script, ok := scriptID(w, r)
	if !ok {
		return
	}
	if _, err := s.pool.Exec(r.Context(), `DELETE FROM guided_chat_drafts WHERE learner_id=$1 AND script_id=$2`, contextID(r), script); err != nil {
		respond(w, http.StatusServiceUnavailable, map[string]string{"error": "Could not clear the draft."})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
