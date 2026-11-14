package steering

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
)

// Replanner is the paid private gRPC boundary, separate from platform screening.
type Replanner interface {
	ReplanOutline(context.Context, *pb.ReplanOutlineRequest) (*pb.ReplanOutlineResponse, error)
}

// ReplanProposal represents a proposed outline change for learner review.
type ReplanProposal struct {
	ID             string       `json:"id"`
	OutlineVersion int64        `json:"outline_version"`
	State          string       `json:"state"`
	ExpiresAt      time.Time    `json:"expires_at"`
	Diff           []ReplanDiff `json:"diff"`
	Reason         string       `json:"reason_code,omitempty"`
	Decision       string       `json:"decision,omitempty"`
	DecidedAt      *time.Time   `json:"decided_at,omitempty"`
}
type replanInput struct {
	Outline      *pb.CourseOutline       `json:"outline"`
	Protected    []string                `json:"protected"`
	Instructions []*pb.CourseInstruction `json:"instructions"`
	Version      int64                   `json:"version"`
}

func (in replanInput) hash() string {
	raw, _ := json.Marshal(in)
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func (s *Service) replanSnapshot(ctx context.Context, tx pgx.Tx, owner, course string) (replanInput, error) {
	in := replanInput{Outline: &pb.CourseOutline{CourseId: course}, Protected: []string{}, Instructions: []*pb.CourseInstruction{}}
	if err := tx.QueryRow(ctx, `SELECT title,description,outline_version FROM courses WHERE id=$1::uuid AND learner_id=$2::uuid AND deleted_at IS NULL AND visibility='private' AND NOT EXISTS(SELECT 1 FROM course_copies WHERE copy_course_id=$1::uuid AND forked_at IS NULL) AND EXISTS(SELECT 1 FROM course_enrollments WHERE course_id=$1::uuid AND learner_id=$2::uuid) FOR UPDATE`, course, owner).Scan(&in.Outline.Title, &in.Outline.Description, &in.Version); err != nil {
		return in, refusal("not_found")
	}
	rows, err := tx.Query(ctx, `SELECT u.id::text,u.title,l.id::text,l.title,l.goal,l.status<>'locked' OR EXISTS(SELECT 1 FROM generated_lessons g WHERE g.lesson_stub_id=l.id) OR EXISTS(SELECT 1 FROM lesson_generations lg WHERE lg.lesson_id=l.id) FROM course_units u JOIN course_lesson_stubs l ON l.unit_id=u.id WHERE u.course_id=$1::uuid AND u.learner_id=$2::uuid AND u.archived_at IS NULL AND l.archived_at IS NULL ORDER BY u.position,l.position`, course, owner)
	if err != nil {
		return in, err
	}
	for rows.Next() {
		var uid, utitle, lid, title, goal string
		var protected bool
		if err = rows.Scan(&uid, &utitle, &lid, &title, &goal, &protected); err != nil {
			rows.Close()
			return in, err
		}
		if len(in.Outline.Units) == 0 || in.Outline.Units[len(in.Outline.Units)-1].UnitId != uid {
			in.Outline.Units = append(in.Outline.Units, &pb.Unit{UnitId: uid, Title: utitle})
		}
		u := in.Outline.Units[len(in.Outline.Units)-1]
		u.Lessons = append(u.Lessons, &pb.LessonRef{LessonId: lid, Title: title, Goal: goal})
		if protected {
			in.Protected = append(in.Protected, lid)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return in, err
	}
	rows, err = tx.Query(ctx, `SELECT id::text,text,scope,version,(extract(epoch FROM added_at)*1000)::bigint FROM course_instructions WHERE learner_id=$1::uuid AND course_id=$2::uuid AND status='active' ORDER BY added_at,id`, owner, course)
	if err != nil {
		return in, err
	}
	defer rows.Close()
	for rows.Next() {
		var i pb.CourseInstruction
		var scope string
		if err = rows.Scan(&i.Id, &i.Text, &scope, &i.Version, &i.AddedAtUnixMs); err != nil {
			return in, err
		}
		i.Scope = pb.CourseInstruction_SCOPE_REST_OF_COURSE
		if scope == "next_lesson" {
			i.Scope = pb.CourseInstruction_SCOPE_NEXT_LESSON
		}
		in.Instructions = append(in.Instructions, &i)
	}
	if _, ok := outlineLocations(in.Outline); !ok {
		return in, refusal("replan_unavailable")
	}
	return in, rows.Err()
}

// QuoteReplan snapshots terms before consent, reservation or provider work.
func (s *Service) QuoteReplan(ctx context.Context, owner, course string) (pricing.Quote, error) {
	if s.opts.Quotes == nil || s.opts.Financial == nil || s.opts.Replanner == nil {
		return pricing.Quote{}, refusal("replan_unavailable")
	}
	if _, err := s.privateCourse(ctx, owner, course); err != nil {
		return pricing.Quote{}, err
	}
	if info, err := s.forkInfo(ctx, owner, course); err != nil {
		return pricing.Quote{}, err
	} else if info != nil && !info.Forked {
		return pricing.Quote{}, refusal("fork_confirmation_required")
	}
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		return pricing.Quote{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	in, err := s.replanSnapshot(ctx, tx, owner, course)
	if err != nil {
		return pricing.Quote{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return pricing.Quote{}, err
	}
	q, err := s.opts.Quotes.Create(ctx, owner, pricing.QuoteInput{Action: "outline_replan", CourseID: course, OutlineRevision: in.Version, RequestHash: in.hash()})
	if err != nil {
		return q, refusal("replan_unavailable")
	}
	s.emit(owner, "course_replan_quoted", map[string]any{"course_id": course, "credits": q.ContentCeiling, "pricing_version": q.PricingVersion})
	return q, nil
}
func (s *Service) readReplan(ctx context.Context, owner, course, quote string) (ReplanProposal, error) {
	p := ReplanProposal{}
	var raw []byte
	var dec *string
	err := s.opts.Pool.QueryRow(ctx, `SELECT id::text,outline_version,state,expires_at,diff,reason,decision,decided_at FROM course_replan_proposals WHERE learner_id::text=$1 AND course_id::text=$2 AND quote_id::text=$3`, owner, course, quote).Scan(&p.ID, &p.OutlineVersion, &p.State, &p.ExpiresAt, &raw, &p.Reason, &dec, &p.DecidedAt)
	if dec != nil {
		p.Decision = *dec
	}
	if err == nil {
		err = json.Unmarshal(raw, &p.Diff)
	}
	return p, err
}

func diffCounts(diff []ReplanDiff) (added, removed, changed int) {
	touched := map[string]bool{}
	for _, d := range diff {
		switch d.Kind {
		case "added":
			added++
		case "removed":
			removed++
		default:
			touched[d.LessonID] = true
		}
	}
	return added, removed, len(touched)
}

func checkCanConfirm(state, decision string, expiresAt, now time.Time) string {
	if state != "proposed" {
		return "proposal_not_ready"
	}
	if decision == "confirmed" {
		return "already_confirmed"
	}
	if decision == "discarded" {
		return "proposal_discarded"
	}
	if !now.Before(expiresAt) {
		return "proposal_expired"
	}
	return ""
}

func checkCanDiscard(state, decision string) string {
	if decision == "discarded" {
		return "already_discarded"
	}
	if decision == "confirmed" {
		return "proposal_confirmed"
	}
	if state != "proposed" {
		return "proposal_not_ready"
	}
	return ""
}

// ConfirmReplan applies the proposed remaining outline atomically and increases outline_version.
func (s *Service) ConfirmReplan(ctx context.Context, owner, course, proposalID string) (ReplanProposal, error) {
	if _, err := uuid.Parse(proposalID); err != nil {
		return ReplanProposal{}, refusal("not_found")
	}
	if _, err := s.privateCourse(ctx, owner, course); err != nil {
		return ReplanProposal{}, err
	}
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		return ReplanProposal{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var currentVersion int64
	if err = tx.QueryRow(ctx, `SELECT outline_version FROM courses WHERE id=$1::uuid AND learner_id=$2::uuid AND deleted_at IS NULL AND visibility='private' FOR UPDATE`, course, owner).Scan(&currentVersion); err != nil {
		return ReplanProposal{}, refusal("not_found")
	}
	var unforked bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM course_copies WHERE copy_course_id=$1::uuid AND forked_at IS NULL)`, course).Scan(&unforked); err != nil {
		return ReplanProposal{}, err
	}
	if unforked {
		return ReplanProposal{}, refusal("fork_confirmation_required")
	}
	// Serialise with lesson unlock/generation, which flip stub rows, before judging protection.
	if _, err = tx.Exec(ctx, `SELECT l.id FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1::uuid FOR UPDATE OF l`, course); err != nil {
		return ReplanProposal{}, err
	}

	var p ReplanProposal
	var dec *string
	var diffRaw []byte
	var resultRaw []byte
	var inputRaw []byte
	err = tx.QueryRow(ctx, `SELECT id::text,outline_version,state,expires_at,diff,reason,decision,decided_at,result_outline,input FROM course_replan_proposals WHERE id=$1::uuid AND learner_id=$2::uuid AND course_id=$3::uuid FOR UPDATE`, proposalID, owner, course).Scan(&p.ID, &p.OutlineVersion, &p.State, &p.ExpiresAt, &diffRaw, &p.Reason, &dec, &p.DecidedAt, &resultRaw, &inputRaw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return p, refusal("not_found")
		}
		return p, err
	}
	if dec != nil {
		p.Decision = *dec
	}
	if len(diffRaw) > 0 {
		_ = json.Unmarshal(diffRaw, &p.Diff)
	}

	var dbNow time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&dbNow); err != nil {
		return p, err
	}
	validity := checkCanConfirm(p.State, p.Decision, p.ExpiresAt, dbNow)
	if validity == "already_confirmed" {
		return p, nil
	}
	if validity != "" {
		return p, refusal(validity)
	}

	if currentVersion != p.OutlineVersion {
		return p, refusal("version_conflict")
	}

	var in replanInput
	if err = json.Unmarshal(inputRaw, &in); err != nil {
		return p, refusal("invalid_proposal")
	}

	var newlyProtected int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1::uuid AND u.archived_at IS NULL AND l.archived_at IS NULL AND (l.status<>'locked' OR EXISTS(SELECT 1 FROM generated_lessons g WHERE g.lesson_stub_id=l.id) OR EXISTS(SELECT 1 FROM lesson_generations lg WHERE lg.lesson_id=l.id)) AND l.id != ALL($2::uuid[])`, course, in.Protected).Scan(&newlyProtected)
	if err != nil || newlyProtected > 0 {
		return p, refusal("version_conflict")
	}

	var out pb.CourseOutline
	if err = json.Unmarshal(resultRaw, &out); err != nil {
		return p, refusal("invalid_proposal")
	}

	if _, err = ValidateReplan(in.Outline, &out, in.Protected); err != nil {
		return p, err
	}

	// Only rows of the snapshot may be reused; a model-supplied id never reaches another course's rows.
	baseUnits := map[string]bool{}
	baseLessons := map[string]string{}
	for _, u := range in.Outline.Units {
		baseUnits[u.UnitId] = true
		for _, l := range u.Lessons {
			baseLessons[l.LessonId] = u.UnitId
		}
	}

	// Park live rows far above any real position (at most 10 units x 20 lessons) so the unique
	// (parent, position) keys never collide mid-rewrite. The final guard proves none stays parked.
	if _, err = tx.Exec(ctx, `UPDATE course_units SET position=position+500000 WHERE course_id=$1::uuid AND archived_at IS NULL`, course); err != nil {
		return p, err
	}
	if _, err = tx.Exec(ctx, `UPDATE course_lesson_stubs st SET position=st.position+500000 FROM course_units u WHERE u.id=st.unit_id AND u.course_id=$1::uuid AND u.archived_at IS NULL AND st.archived_at IS NULL`, course); err != nil {
		return p, err
	}

	var keptUnitIDs []string
	var keptLessonIDs []string
	for ui, u := range out.Units {
		var unitID string
		if baseUnits[u.UnitId] {
			unitID = u.UnitId
			if _, err = tx.Exec(ctx, `UPDATE course_units SET position=$2, title=$3 WHERE id=$1::uuid AND course_id=$4::uuid`, unitID, ui, u.Title, course); err != nil {
				return p, err
			}
		} else if err = tx.QueryRow(ctx, `INSERT INTO course_units(learner_id, course_id, authored_id, position, title) VALUES($1::uuid, $2::uuid, 'replan-'||gen_random_uuid()::text, $3, $4) RETURNING id::text`, owner, course, ui, u.Title).Scan(&unitID); err != nil {
			return p, err
		}
		keptUnitIDs = append(keptUnitIDs, unitID)

		for li, l := range u.Lessons {
			var lessonID string
			if from, ok := baseLessons[l.LessonId]; ok {
				lessonID = l.LessonId
				// A lesson that changes unit gets a fresh authored_id so it cannot collide there; protected lessons never move.
				if _, err = tx.Exec(ctx, `UPDATE course_lesson_stubs SET unit_id=$2::uuid, position=$3, title=$4, goal=$5, authored_id=CASE WHEN $6::boolean THEN 'replan-'||id::text ELSE authored_id END WHERE id=$1::uuid`, lessonID, unitID, li, l.Title, l.Goal, from != u.UnitId); err != nil {
					return p, err
				}
			} else if err = tx.QueryRow(ctx, `INSERT INTO course_lesson_stubs(learner_id, unit_id, authored_id, position, title, goal, status) VALUES($1::uuid, $2::uuid, 'replan-'||gen_random_uuid()::text, $3, $4, $5, 'locked') RETURNING id::text`, owner, unitID, li, l.Title, l.Goal).Scan(&lessonID); err != nil {
				return p, err
			}
			keptLessonIDs = append(keptLessonIDs, lessonID)
		}
	}

	// Remove only untouched stubs (locked, no generation, manifest or consent rows): their cascades hold no learner data.
	if _, err = tx.Exec(ctx, `DELETE FROM course_lesson_stubs st USING course_units u WHERE u.id=st.unit_id AND u.course_id=$1::uuid AND u.archived_at IS NULL AND st.archived_at IS NULL AND st.id != ALL($2::uuid[]) AND st.status='locked'
		AND NOT EXISTS(SELECT 1 FROM generated_lessons g WHERE g.lesson_stub_id=st.id)
		AND NOT EXISTS(SELECT 1 FROM lesson_generations g WHERE g.lesson_id=st.id)
		AND NOT EXISTS(SELECT 1 FROM lesson_manifests m WHERE m.lesson_id=st.id)
		AND NOT EXISTS(SELECT 1 FROM copy_generation_consents c WHERE c.lesson_id=st.id)`, course, keptLessonIDs); err != nil {
		return p, err
	}
	// Anything still parked (or inside a unit about to be dropped) is real learner data: abort instead of cascading.
	var parked int
	if err = tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM course_lesson_stubs st JOIN course_units u ON u.id=st.unit_id WHERE u.course_id=$1::uuid AND st.position>=500000) + (SELECT count(*) FROM course_lesson_stubs st JOIN course_units u ON u.id=st.unit_id WHERE u.course_id=$1::uuid AND u.archived_at IS NULL AND st.archived_at IS NULL AND NOT (u.id=ANY($2::uuid[])))`, course, keptUnitIDs).Scan(&parked); err != nil {
		return p, err
	}
	if parked > 0 {
		return p, refusal("version_conflict")
	}
	if _, err = tx.Exec(ctx, `DELETE FROM course_units WHERE course_id=$1::uuid AND archived_at IS NULL AND id != ALL($2::uuid[])`, course, keptUnitIDs); err != nil {
		return p, err
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM course_units WHERE course_id=$1::uuid AND position>=500000`, course).Scan(&parked); err != nil || parked > 0 {
		if err == nil {
			err = refusal("version_conflict")
		}
		return p, err
	}

	var nextVersion int64
	if err = tx.QueryRow(ctx, `UPDATE courses SET outline_version=outline_version+1 WHERE id=$1::uuid RETURNING outline_version`, course).Scan(&nextVersion); err != nil {
		return p, err
	}

	var decidedAt time.Time
	if err = tx.QueryRow(ctx, `UPDATE course_replan_proposals SET decision='confirmed', decided_at=clock_timestamp() WHERE id=$1::uuid RETURNING decided_at`, proposalID).Scan(&decidedAt); err != nil {
		return p, err
	}

	if err = tx.Commit(ctx); err != nil {
		return p, err
	}

	p.Decision = "confirmed"
	p.DecidedAt = &decidedAt

	added, removed, changed := diffCounts(p.Diff)
	s.emit(owner, "course_replan_confirmed", map[string]any{"course_id": course, "proposal_id": proposalID, "outline_version": nextVersion, "lessons_added": added, "lessons_removed": removed, "lessons_changed": changed})
	return p, nil
}

// DiscardReplan marks the proposal discarded without modifying the course outline or refunding.
func (s *Service) DiscardReplan(ctx context.Context, owner, course, proposalID string) (ReplanProposal, error) {
	if _, err := uuid.Parse(proposalID); err != nil {
		return ReplanProposal{}, refusal("not_found")
	}
	if _, err := s.privateCourse(ctx, owner, course); err != nil {
		return ReplanProposal{}, err
	}
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		return ReplanProposal{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var p ReplanProposal
	var dec *string
	var diffRaw []byte
	err = tx.QueryRow(ctx, `SELECT id::text,outline_version,state,expires_at,diff,reason,decision,decided_at FROM course_replan_proposals WHERE id=$1::uuid AND learner_id=$2::uuid AND course_id=$3::uuid FOR UPDATE`, proposalID, owner, course).Scan(&p.ID, &p.OutlineVersion, &p.State, &p.ExpiresAt, &diffRaw, &p.Reason, &dec, &p.DecidedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return p, refusal("not_found")
		}
		return p, err
	}
	if dec != nil {
		p.Decision = *dec
	}
	if len(diffRaw) > 0 {
		_ = json.Unmarshal(diffRaw, &p.Diff)
	}

	validity := checkCanDiscard(p.State, p.Decision)
	if validity == "already_discarded" {
		return p, nil
	}
	if validity != "" {
		return p, refusal(validity)
	}

	var decidedAt time.Time
	if err = tx.QueryRow(ctx, `UPDATE course_replan_proposals SET decision='discarded', decided_at=clock_timestamp() WHERE id=$1::uuid RETURNING decided_at`, proposalID).Scan(&decidedAt); err != nil {
		return p, err
	}
	if err = tx.Commit(ctx); err != nil {
		return p, err
	}

	p.Decision = "discarded"
	p.DecidedAt = &decidedAt

	s.emit(owner, "course_replan_discarded", map[string]any{"course_id": course, "proposal_id": proposalID})
	return p, nil
}

// ProposeReplan resumes one immutable quote. Consent only permits this server-owned action.
func (s *Service) ProposeReplan(ctx context.Context, owner, course, quoteID string) (ReplanProposal, error) {
	if _, err := s.privateCourse(ctx, owner, course); err != nil {
		return ReplanProposal{}, err
	}
	if info, err := s.forkInfo(ctx, owner, course); err != nil {
		return ReplanProposal{}, err
	} else if info != nil && !info.Forked {
		return ReplanProposal{}, refusal("fork_confirmation_required")
	}
	p, err := s.readReplan(ctx, owner, course, quoteID)
	if err == nil && p.State != "pending" {
		if p.State == "failed" {
			return p, refusal(p.Reason)
		}
		return p, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return p, err
	}
	if s.opts.Quotes == nil || s.opts.Financial == nil || s.opts.Replanner == nil {
		return p, refusal("replan_unavailable")
	}
	q, err := s.opts.Quotes.Get(ctx, owner, quoteID)
	if err != nil || q.Action != "outline_replan" || !q.Accepted || q.Expired || q.Input.CourseID != course {
		return p, refusal("quote_invalid")
	}
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		return p, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	in, err := s.replanSnapshot(ctx, tx, owner, course)
	if err != nil {
		return p, err
	}
	if in.Version != q.Input.OutlineRevision || in.hash() != q.Input.RequestHash {
		return p, refusal("quote_invalid")
	}
	raw, _ := json.Marshal(in)
	_, err = tx.Exec(ctx, `INSERT INTO course_replan_proposals(learner_id,course_id,quote_id,outline_version,input) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5) ON CONFLICT(quote_id) DO NOTHING`, owner, course, quoteID, in.Version, raw)
	if err != nil {
		return p, err
	}
	if err = tx.Commit(ctx); err != nil {
		return p, err
	}
	p, err = s.readReplan(ctx, owner, course, quoteID)
	if err != nil {
		return p, err
	}
	// Refuse an unfunded balance before the shared daily-cost caps can mask it as a generic outage.
	// Daily free Credits are not eligible for this action; Reserve below stays authoritative.
	var job credits.Reservation
	wallet, err := s.opts.Financial.GetWallet(ctx, owner)
	if err == nil && wallet.Purchased+wallet.Subscription < q.ContentCeiling {
		err = credits.ErrInsufficient
	} else {
		job, err = s.opts.Financial.ReserveAction(ctx, owner, quoteID, "outline_replan")
	}
	if err != nil {
		code := "replan_unavailable"
		if errors.Is(err, credits.ErrInsufficient) {
			code = "insufficient_credits"
		}
		s.emit(owner, "course_replan_failed", map[string]any{"course_id": course, "reason": code})
		return p, refusal(code)
	}
	// Claim the product before the financial executor. Only one request can start work.
	tag, err := s.opts.Pool.Exec(ctx, `UPDATE course_replan_proposals SET state='running',job_id=$2::uuid WHERE id=$1::uuid AND state='pending'`, p.ID, job.ID)
	if err != nil {
		return p, err
	}
	if tag.RowsAffected() == 0 {
		return s.readReplan(ctx, owner, course, quoteID)
	}
	// Finish the accepted bounded operation even if the browser disconnects; reconnect uses the quote id.
	work, cancel := context.WithDeadline(context.WithoutCancel(ctx), job.DeadlineAt)
	defer cancel()
	if err = s.opts.Financial.Claim(work, owner, job.ID); err != nil {
		return p, s.failReplan(owner, course, p.ID, job.ID, "generation_failed", nil)
	}
	limits, err := s.opts.Financial.Limits(work, owner, job.ID, "outline_replan")
	if err != nil {
		return p, s.failReplan(owner, course, p.ID, job.ID, "generation_failed", nil)
	}
	response, callErr := s.opts.Replanner.ReplanOutline(work, &pb.ReplanOutlineRequest{Outline: in.Outline, ProtectedLessonIds: in.Protected, Instructions: in.Instructions, Learner: &pb.LearnerProfile{LearnerId: owner}, Limits: limits})
	var attempts []*pb.Usage
	if response != nil {
		attempts = response.Attempts
	}
	if callErr != nil || work.Err() != nil || response == nil || response.Unavailable {
		return p, s.failReplan(owner, course, p.ID, job.ID, "generation_failed", attempts)
	}
	diff, err := ValidateReplan(in.Outline, response.Outline, in.Protected)
	if err != nil {
		return p, s.failReplan(owner, course, p.ID, job.ID, Code(err), attempts)
	}
	if len(attempts) == 0 || len(attempts) > 2 {
		return p, s.failReplan(owner, course, p.ID, job.ID, "invalid_proposal", attempts)
	}
	delivered := false
	for _, a := range attempts {
		if a == nil {
			return p, s.failReplan(owner, course, p.ID, job.ID, "invalid_proposal", nil)
		}
		delivered = delivered || a.Delivered
		if a.Delivered {
			a.ArtifactId = "course-replan:" + p.ID
		}
	}
	if !delivered {
		return p, s.failReplan(owner, course, p.ID, job.ID, "invalid_proposal", attempts)
	}
	result, _ := json.Marshal(response.Outline)
	diffRaw, _ := json.Marshal(diff)
	usage, _ := json.Marshal(attempts)
	settleCtx, done := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer done()
	_, err = s.opts.Financial.SettleWith(settleCtx, owner, job.ID, attempts, func(tx pgx.Tx, r credits.Reservation) error {
		if r.State != "settled" {
			return refusal("invalid_proposal")
		}
		_, e := tx.Exec(settleCtx, `UPDATE course_replan_proposals SET result_outline=$2,diff=$3,attempts=$4,state='proposed',expires_at=clock_timestamp()+interval '24 hours' WHERE id=$1::uuid AND state='running'`, p.ID, result, diffRaw, usage)
		return e
	})
	if err != nil {
		return p, s.failReplan(owner, course, p.ID, job.ID, "invalid_proposal", attempts)
	}
	s.replanUsage(owner, q.PricingVersion, attempts, true)
	added, removed, changed := diffCounts(diff)
	s.emit(owner, "course_replan_proposed", map[string]any{"course_id": course, "proposal_id": p.ID, "lessons_added": added, "lessons_removed": removed, "lessons_changed": changed})
	return s.readReplan(settleCtx, owner, course, quoteID)
}
func (s *Service) replanUsage(owner, version string, attempts []*pb.Usage, success bool) {
	if len(attempts) == 0 {
		s.emit(owner, "$ai_generation", map[string]any{"feature": "steer_replan", "pricing_version": version, "succeeded": false, "usage_available": false})
		return
	}
	for _, a := range attempts {
		if a != nil {
			s.emit(owner, "$ai_generation", map[string]any{"feature": "steer_replan", "pricing_version": version, "provider": a.Provider, "model": a.Model, "input_tokens": a.InputTokens, "output_tokens": a.OutputTokens, "cache_write_tokens": a.CacheWriteTokens, "cache_read_tokens": a.CacheReadTokens, "latency_ms": a.LatencyMs, "fallback_used": a.FallbackUsed, "succeeded": success && a.Delivered, "usage_available": !a.UsageUnavailable})
		}
	}
}
func (s *Service) failReplan(owner, course, id, job, code string, attempts []*pb.Usage) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	safe := []*pb.Usage{}
	for _, a := range attempts {
		if a != nil {
			c := proto.Clone(a).(*pb.Usage)
			c.Delivered = false
			safe = append(safe, c)
		}
	}
	transition := func(tx pgx.Tx, _ credits.Reservation) error {
		_, e := tx.Exec(ctx, `UPDATE course_replan_proposals SET state='failed',reason=$2 WHERE id=$1::uuid AND state IN ('pending','running')`, id, code)
		return e
	}
	result, err := s.opts.Financial.SettleWith(ctx, owner, job, safe, transition)
	if errors.Is(err, credits.ErrUsage) {
		result, err = s.opts.Financial.SettleWith(ctx, owner, job, nil, transition)
	}
	if err != nil {
		return err
	}
	// The generic deadline reaper may have settled first; complete only its product record.
	if result.State == "released" {
		_, err = s.opts.Pool.Exec(ctx, `UPDATE course_replan_proposals SET state='failed',reason=$2 WHERE id=$1::uuid AND state IN ('pending','running')`, id, code)
	}
	if err != nil {
		return err
	}
	s.replanUsage(owner, result.PricingVersion, safe, false)
	s.emit(owner, "course_replan_failed", map[string]any{"course_id": course, "reason": code})
	return refusal(code)
}
func (s *Service) recoverReplans(ctx context.Context) error {
	if s.opts.Financial == nil {
		return nil
	}
	rows, err := s.opts.Pool.Query(ctx, `SELECT p.learner_id::text,p.course_id::text,p.id::text,p.job_id::text FROM course_replan_proposals p JOIN credit_jobs j ON j.id=p.job_id WHERE p.state IN ('pending','running') AND j.deadline_at<clock_timestamp()`)
	if err != nil {
		return err
	}
	var pending [][4]string
	for rows.Next() {
		var r [4]string
		if err = rows.Scan(&r[0], &r[1], &r[2], &r[3]); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, r := range pending {
		if e := s.failReplan(r[0], r[1], r[2], r[3], "generation_failed", nil); e != nil && Code(e) == "unavailable" {
			return e
		}
	}
	return nil
}
