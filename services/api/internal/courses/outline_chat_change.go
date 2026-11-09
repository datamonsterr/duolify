package courses

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/jackc/pgx/v5"
)

type proposalInput struct {
	DraftID          string `json:"draft_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	Request          string `json:"request"`
	QuoteID          string `json:"quote_id"`
}

// OutlineProposal exposes an immutable diff and its separately recorded decision.
type OutlineProposal struct {
	ID           string             `json:"id"`
	DraftID      string             `json:"draft_id"`
	BaseRevision int64              `json:"base_revision"`
	BaseOutline  Outline            `json:"base_outline"`
	Request      string             `json:"request"`
	State        string             `json:"state"`
	Decision     string             `json:"decision"`
	Operations   []OutlineOperation `json:"operations"`
}

const proposalColumns = `p.id::text,p.draft_id::text,p.base_revision,p.state,p.decision,p.operations,p.base_outline,p.request`

func scanProposal(row pgx.Row) (OutlineProposal, error) {
	var p OutlineProposal
	var raw []byte
	var base []byte
	err := row.Scan(&p.ID, &p.DraftID, &p.BaseRevision, &p.State, &p.Decision, &raw, &base, &p.Request)
	if err == nil && len(raw) > 0 {
		err = json.Unmarshal(raw, &p.Operations)
	}
	if err == nil {
		err = json.Unmarshal(base, &p.BaseOutline)
	}
	return p, err
}
func requestHash(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }
func proposalError(w http.ResponseWriter, code int, message string) {
	respond(w, code, map[string]string{"error": message})
}
func (s *Service) proposalEvent(ctx context.Context, owner, event, id string, rev int64) {
	if s.opts.Events == nil {
		return
	}
	var optout bool
	if s.opts.Pool.QueryRow(ctx, "SELECT analytics_opt_out FROM learner_profiles WHERE learner_id::text=$1", owner).Scan(&optout) != nil || optout {
		return
	}
	_ = s.opts.Events.Capture(owner, event, map[string]any{"source": "api", "platform": "web", "app_version": "0.1.0", "proposal_id": id, "base_revision": rev})
}
func (s *Service) readProposal(ctx context.Context, owner, id, quoteID string) (OutlineProposal, error) {
	p, err := scanProposal(s.opts.Pool.QueryRow(ctx, `SELECT `+proposalColumns+` FROM outline_proposals p JOIN outline_drafts d ON d.id=p.draft_id JOIN course_brief_revisions b ON b.id=d.brief_revision WHERE p.learner_id::text=$1 AND (p.id::text=$2 OR ($2='' AND p.quote_id::text=$3)) AND b.expires_at>now()`, owner, id, quoteID))
	if err != nil || p.State != "pending" {
		return p, err
	}
	// An interrupted executor is never restarted by reconnect. Recover its hold
	// after the original deadline, including a crash between insertion and reserve.
	var job string
	var expired bool
	if err = s.opts.Pool.QueryRow(ctx, `SELECT COALESCE(j.id::text,''), clock_timestamp() >= COALESCE(j.deadline_at,p.created_at+(q.snapshot#>>'{bounds,timeout_ms}')::bigint*interval '1 millisecond') FROM outline_proposals p JOIN pricing_quotes q ON q.id=p.quote_id LEFT JOIN credit_jobs j ON j.quote_id=p.quote_id WHERE p.id::text=$1`, p.ID).Scan(&job, &expired); err != nil || !expired {
		return p, err
	}
	if job != "" {
		if s.opts.Financial == nil {
			return p, errors.New("financial unavailable")
		}
		if _, err = s.opts.Financial.Settle(ctx, owner, job, nil); err != nil {
			return p, err
		}
	}
	tag, err := s.opts.Pool.Exec(ctx, "UPDATE outline_proposals SET state='failed' WHERE id::text=$1 AND state='pending'", p.ID)
	if err != nil {
		return p, err
	}
	if tag.RowsAffected() == 1 {
		p.State = "failed"
		s.proposalEvent(ctx, owner, "outline_proposal_failed", p.ID, p.BaseRevision)
	}
	return p, nil
}

// ServeProposalHTTP shares authenticated draft ownership and the existing financial boundary.
func (s *Service) ServeProposalHTTP(w http.ResponseWriter, r *http.Request) {
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
	tail := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/outline-proposals"), "/")
	if tail == "quote" && r.Method == "POST" {
		s.quoteProposal(w, r, owner)
		return
	}
	if tail == "" && r.Method == "POST" {
		s.generateProposal(w, r, owner)
		return
	}
	parts := strings.Split(tail, "/")
	if len(parts) == 1 && r.Method == "GET" {
		p, err := s.readProposal(r.Context(), owner, tail, r.URL.Query().Get("quote_id"))
		if err != nil {
			outlineError(w, 404)
			return
		}
		if p.State == "validated" {
			p, err = s.deliverProposal(r.Context(), owner, p)
			if err != nil {
				outlineError(w, 503)
				return
			}
		}
		respond(w, 200, p)
		return
	}
	if len(parts) == 2 && r.Method == "POST" && (parts[1] == "apply" || parts[1] == "discard" || parts[1] == "undo") {
		s.decideProposal(w, r, owner, parts[0], parts[1])
		return
	}
	outlineError(w, 404)
}
func (s *Service) proposalBase(ctx context.Context, owner string, in proposalInput) (OutlineDraft, error) {
	if in.ExpectedRevision < 1 || !utf8.ValidString(in.Request) || utf8.RuneCountInString(in.Request) > 1000 || strings.TrimSpace(in.Request) == "" || strings.ContainsRune(in.Request, 0) {
		return OutlineDraft{}, errors.New("invalid proposal request")
	}
	d, err := s.readOutline(ctx, owner, in.DraftID)
	if err != nil || d.Revision != in.ExpectedRevision || d.Outline.Validate(false) != nil {
		return d, errors.New("stale draft")
	}
	return d, nil
}
func (s *Service) quoteProposal(w http.ResponseWriter, r *http.Request, owner string) {
	var in proposalInput
	if !decodeOutline(w, r, &in) || in.QuoteID != "" {
		outlineError(w, 400)
		return
	}
	if _, err := s.proposalBase(r.Context(), owner, in); err != nil {
		proposalError(w, 409, "Outline out of date. Save or reload your edits, then review a fresh quote.")
		return
	}
	quotes := pricing.NewQuotes(s.opts.Pool)
	input := pricing.QuoteInput{Action: "outline_chat_change", OutlineDraftID: in.DraftID, OutlineRevision: in.ExpectedRevision, RequestHash: requestHash(in.Request)}
	var q pricing.Quote
	var err error
	if s.opts.Environment == "development" || s.opts.Environment == "test" {
		q, err = quotes.CreateDevelopmentOutline(r.Context(), owner, input)
	} else if s.opts.LiveApproved {
		q, err = quotes.Create(r.Context(), owner, input)
	} else {
		err = errors.New("production approval required")
	}
	if err != nil {
		proposalError(w, 503, "Change quote unavailable. Your request is kept; manual edits remain available.")
		return
	}
	respond(w, 200, q)
}
func (s *Service) generateProposal(w http.ResponseWriter, r *http.Request, owner string) {
	var in proposalInput
	if !decodeOutline(w, r, &in) || in.QuoteID == "" {
		outlineError(w, 400)
		return
	}
	if p, err := s.readProposal(r.Context(), owner, "", in.QuoteID); err == nil {
		if p.State == "validated" {
			p, err = s.deliverProposal(r.Context(), owner, p)
			if err != nil {
				outlineError(w, 503)
				return
			}
		}
		respond(w, 200, p)
		return
	}
	d, err := s.proposalBase(r.Context(), owner, in)
	if err != nil {
		proposalError(w, 409, "Proposal out of date. Reload the outline and explicitly review a fresh quote.")
		return
	}
	q, err := pricing.NewQuotes(s.opts.Pool).Get(r.Context(), owner, in.QuoteID)
	if err != nil || !q.Accepted || q.Expired || q.Action != "outline_chat_change" || q.Input.OutlineDraftID != d.ID || q.Input.OutlineRevision != d.Revision || q.Input.RequestHash != requestHash(in.Request) {
		proposalError(w, 409, "Fresh accepted change quote required. Your request is kept.")
		return
	}
	if s.opts.ProposalEngine == nil || s.opts.Financial == nil || (s.opts.Environment != "test" && s.opts.Environment != "development" && !s.opts.LiveApproved) {
		outlineError(w, 503)
		return
	}
	if !s.limit(r.Context(), "outline-proposal:"+owner, 30, time.Hour) {
		outlineError(w, 429)
		return
	}
	raw, _ := json.Marshal(d.Outline)
	p, err := scanProposal(s.opts.Pool.QueryRow(r.Context(), `INSERT INTO outline_proposals AS p(learner_id,draft_id,quote_id,base_revision,base_outline,request) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,$6) ON CONFLICT(quote_id) DO NOTHING RETURNING `+proposalColumns, owner, d.ID, in.QuoteID, d.Revision, raw, in.Request))
	if err != nil {
		if existing, e := s.readProposal(r.Context(), owner, "", in.QuoteID); e == nil {
			respond(w, 200, existing)
		} else {
			outlineError(w, 503)
		}
		return
	}
	job, err := s.opts.Financial.ReserveAction(r.Context(), owner, in.QuoteID, "outline_chat_change")
	if err != nil {
		_, _ = s.opts.Pool.Exec(r.Context(), "UPDATE outline_proposals SET state='failed' WHERE id::text=$1", p.ID)
		message := "Generation limit reached. Your outline and request are kept; review a fresh quote to retry."
		if errors.Is(err, credits.ErrInsufficient) {
			message = "Not enough eligible credits. Your outline and request are kept."
		}
		proposalError(w, 409, message)
		return
	}
	if job.State != "reserved" {
		outlineError(w, 409)
		return
	}
	finished, claimed := false, false
	defer func() {
		if !finished {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
			defer cancel()
			if claimed {
				_, _ = s.opts.Financial.Settle(ctx, owner, job.ID, nil)
			} else {
				_, _ = s.opts.Financial.Release(ctx, owner, job.ID)
			}
			_, _ = s.opts.Pool.Exec(ctx, "UPDATE outline_proposals SET state='failed' WHERE id::text=$1 AND state='pending'", p.ID)
		}
	}()
	limits, err := s.opts.Financial.Limits(r.Context(), owner, job.ID, "outline_chat_change")
	if err != nil {
		outlineError(w, 409)
		return
	}
	if err = s.opts.Financial.Claim(r.Context(), owner, job.ID); err != nil {
		outlineError(w, 409)
		return
	}
	claimed = true
	s.proposalEvent(r.Context(), owner, "outline_proposal_requested", p.ID, p.BaseRevision)
	outline := &enginev1.CourseOutline{}
	for _, u := range d.Outline {
		unit := &enginev1.Unit{UnitId: u.ID, Title: u.Title}
		for _, l := range u.Lessons {
			unit.Lessons = append(unit.Lessons, &enginev1.LessonRef{LessonId: l.ID, Title: l.Title, Goal: l.Goal})
		}
		outline.Units = append(outline.Units, unit)
	}
	ctx, cancel := context.WithDeadline(r.Context(), job.DeadlineAt)
	defer cancel()
	response, engineErr := s.opts.ProposalEngine.OutlineChatChange(ctx, &enginev1.OutlineChatChangeRequest{ProposalId: p.ID, DraftId: d.ID, BaseRevision: d.Revision, Outline: outline, Request: in.Request, Limits: limits})
	var attempts []*enginev1.Usage
	if response != nil {
		attempts = response.Attempts
		for _, op := range response.Operations {
			p.Operations = append(p.Operations, OutlineOperation{Kind: op.GetKind(), ID: op.GetId(), ParentID: op.GetParentId(), Position: int(op.GetPosition()), Title: op.GetTitle(), Goal: op.GetGoal()})
		}
	}
	result, validateErr := ApplyOutlineOperations(d.Outline, p.Operations)
	valid := engineErr == nil && ctx.Err() == nil && response != nil && !response.Unavailable && validateErr == nil && len(attempts) > 0 && attempts[len(attempts)-1].GetDelivered()
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer finishCancel()
	if !valid {
		for _, a := range attempts {
			if a != nil {
				a.Delivered = false
			}
		}
		_, _ = s.opts.Financial.Settle(finishCtx, owner, job.ID, attempts)
		_, _ = s.opts.Pool.Exec(finishCtx, "UPDATE outline_proposals SET state='failed' WHERE id::text=$1 AND state='pending'", p.ID)
		s.proposalEvent(finishCtx, owner, "outline_proposal_failed", p.ID, p.BaseRevision)
		proposalError(w, 502, "Couldn't suggest a change. Reserved credits were released. Your outline and request are kept; review a fresh quote to retry.")
		return
	}
	ops, _ := json.Marshal(p.Operations)
	out, _ := json.Marshal(result)
	usage, _ := json.Marshal(attempts)
	if _, err = s.opts.Pool.Exec(finishCtx, "UPDATE outline_proposals SET operations=$2,result_outline=$3,attempts=$4,state='validated' WHERE id::text=$1 AND state='pending'", p.ID, ops, out, usage); err != nil {
		outlineError(w, 503)
		return
	}
	// Durable validated delivery owns settlement from here, including reconnects.
	finished = true
	p.State = "validated"
	p, err = s.deliverProposal(finishCtx, owner, p)
	if err != nil {
		proposalError(w, 503, "Delivery is being recovered. Resume the proposal; do not resend your request.")
		return
	}
	if p.State == "failed" {
		proposalError(w, 502, "Couldn't validate proposal delivery. Reserved credits were released. Your outline and request are kept; review a fresh quote to retry.")
		return
	}
	respond(w, 200, p)
}
func (s *Service) deliverProposal(ctx context.Context, owner string, p OutlineProposal) (OutlineProposal, error) {
	if s.opts.Financial == nil {
		return p, errors.New("financial unavailable")
	}
	var job string
	var raw []byte
	if err := s.opts.Pool.QueryRow(ctx, `SELECT j.id::text,p.attempts FROM outline_proposals p JOIN credit_jobs j ON j.quote_id=p.quote_id WHERE p.id::text=$1 AND p.learner_id::text=$2`, p.ID, owner).Scan(&job, &raw); err != nil {
		return p, err
	}
	var attempts []*enginev1.Usage
	if err := json.Unmarshal(raw, &attempts); err != nil {
		return p, err
	}
	settled, err := s.opts.Financial.Settle(ctx, owner, job, attempts)
	if errors.Is(err, credits.ErrUsage) || (err == nil && settled.State == "released") {
		// A malformed report must never strand a validated proposal or charge it.
		if err != nil {
			if _, err = s.opts.Financial.Settle(ctx, owner, job, nil); err != nil {
				return p, err
			}
		}
		tag, failErr := s.opts.Pool.Exec(ctx, "UPDATE outline_proposals SET state='failed' WHERE id::text=$1 AND state='validated'", p.ID)
		if failErr != nil {
			return p, failErr
		}
		p.State = "failed"
		if tag.RowsAffected() == 1 {
			s.proposalEvent(ctx, owner, "outline_proposal_failed", p.ID, p.BaseRevision)
		}
		return p, nil
	}
	if err != nil || settled.State != "settled" {
		return p, errors.New("settlement unavailable")
	}
	tag, err := s.opts.Pool.Exec(ctx, "UPDATE outline_proposals SET state='delivered' WHERE id::text=$1 AND state='validated'", p.ID)
	if err != nil {
		return p, err
	}
	p.State = "delivered"
	if tag.RowsAffected() == 1 {
		s.proposalEvent(ctx, owner, "outline_proposal_delivered", p.ID, p.BaseRevision)
	}
	return p, nil
}
func (s *Service) decideProposal(w http.ResponseWriter, r *http.Request, owner, id, action string) {
	ctx := r.Context()
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		outlineError(w, 503)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	p, err := scanProposal(tx.QueryRow(ctx, `SELECT `+proposalColumns+` FROM outline_proposals p WHERE p.id::text=$1 AND p.learner_id::text=$2 FOR UPDATE`, id, owner))
	if err != nil {
		outlineError(w, 404)
		return
	}
	d, err := scanOutline(tx.QueryRow(ctx, `SELECT `+outlineColumns+` FROM outline_drafts d JOIN course_brief_revisions b ON b.id=d.brief_revision WHERE d.id::text=$1 AND d.learner_id::text=$2 AND b.expires_at>now() FOR UPDATE OF d`, p.DraftID, owner))
	if err != nil {
		outlineError(w, 404)
		return
	}
	var base, result, decisionRaw []byte
	var applied *int64
	if tx.QueryRow(ctx, "SELECT base_outline,result_outline,applied_revision,decision_draft FROM outline_proposals WHERE id::text=$1", id).Scan(&base, &result, &applied, &decisionRaw) != nil {
		outlineError(w, 503)
		return
	}
	duplicate := (action == "apply" && (p.Decision == "applied" || p.Decision == "undone")) || (action == "discard" && p.Decision == "discarded") || (action == "undo" && p.Decision == "undone")
	if duplicate {
		if len(decisionRaw) > 0 {
			_ = json.Unmarshal(decisionRaw, &d)
		}
		respond(w, 200, map[string]any{"proposal": p, "draft": d})
		return
	}
	if p.State != "delivered" || (action == "undo" && (p.Decision != "applied" || applied == nil || d.Revision != *applied)) || (action != "undo" && p.Decision != "") || (action == "apply" && d.Revision != p.BaseRevision) {
		proposalError(w, 409, "Proposal out of date. Your manual edits are kept. Reload the outline, then explicitly review a fresh quote.")
		return
	}
	switch action {
	case "discard":
		p.Decision = "discarded"
	case "apply":
		d.History = append(d.History, OutlineSnapshot{Value: d.Outline})
		if len(d.History) > 50 {
			d.History = d.History[len(d.History)-50:]
		}
		_ = json.Unmarshal(result, &d.Outline)
		p.Decision = "applied"
	case "undo":
		_ = json.Unmarshal(base, &d.Outline)
		if len(d.History) > 0 {
			d.History = d.History[:len(d.History)-1]
		}
		p.Decision = "undone"
	}
	if action != "discard" {
		d.Revision++
		d.Confirmed = nil
		raw, _ := json.Marshal(d.Outline)
		history, _ := json.Marshal(d.History)
		if _, err = tx.Exec(ctx, "UPDATE outline_drafts SET outline=$2,history=$3,revision=$4,confirmed=NULL,updated_at=now() WHERE id::text=$1", d.ID, raw, history, d.Revision); err != nil {
			outlineError(w, 503)
			return
		}
	}
	saved, _ := json.Marshal(d)
	if _, err = tx.Exec(ctx, "UPDATE outline_proposals SET decision=$2,applied_revision=CASE WHEN $2='applied' THEN $3 ELSE applied_revision END,decision_draft=$4 WHERE id::text=$1", id, p.Decision, d.Revision, saved); err != nil || tx.Commit(ctx) != nil {
		outlineError(w, 503)
		return
	}
	s.proposalEvent(ctx, owner, "outline_proposal_"+p.Decision, p.ID, p.BaseRevision)
	respond(w, 200, map[string]any{"proposal": p, "draft": d})
}
