package courses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type creationArgs struct {
	ID string `json:"id"`
}

func (creationArgs) Kind() string { return "course_outline" }

type creationWorker struct {
	river.WorkerDefaults[creationArgs]
	service *Service
}

func (w *creationWorker) Work(ctx context.Context, j *river.Job[creationArgs]) error {
	return w.service.RunCreation(ctx, j.Args.ID)
}

// CreationQueue serves a dedicated queue; its durable args contain no learner text.
func (s *Service) CreationQueue(logger *slog.Logger) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, &creationWorker{service: s})
	return river.NewClient(riverpgxv5.New(s.opts.Pool), &river.Config{
		Logger:                 logger,
		Workers:                workers,
		Queues:                 map[string]river.QueueConfig{"outlines": {MaxWorkers: 4}},
		LeaderElectionDisabled: true,
	})
}

type creationInput struct {
	DraftID          string `json:"draft_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	QuoteID          string `json:"quote_id"`
}

// Creation represents an active or completed course creation job.
type Creation struct {
	ID       string `json:"id"`
	State    string `json:"state"`
	CourseID string `json:"course_id"`
}

func (s *Service) creation(ctx context.Context, owner, id string) (Creation, error) {
	var c Creation
	err := s.opts.Pool.QueryRow(ctx, `SELECT id::text,state,COALESCE(course_id::text,'') FROM course_creations WHERE learner_id::text=$1 AND id::text=$2`, owner, id).Scan(&c.ID, &c.State, &c.CourseID)
	if err != nil {
		return c, err
	}
	return c, nil
}

// ServeCreationHTTP binds consent to the owned, confirmed head before enqueueing.
func (s *Service) ServeCreationHTTP(w http.ResponseWriter, r *http.Request) {
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
	tail := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/course-creations"), "/")
	if r.Method == "GET" {
		id := strings.TrimSuffix(tail, "/events")
		if strings.HasSuffix(tail, "/events") {
			s.creationEvents(w, r, owner, id)
			return
		}
		c, err := s.creation(r.Context(), owner, id)
		if err != nil {
			outlineError(w, 404)
			return
		}
		respond(w, 200, c)
		return
	}
	var in creationInput
	if r.Method != "POST" || !decodeOutline(w, r, &in) || in.ExpectedRevision < 1 {
		outlineError(w, 400)
		return
	}
	ctx := r.Context()
	if tail == "quote" {
		snapshot, err := s.ResolveConfirmed(ctx, owner, in.DraftID, in.ExpectedRevision)
		if err != nil || in.QuoteID != "" {
			outlineError(w, 409)
			return
		}
		q, err := pricing.NewQuotes(s.opts.Pool).Create(ctx, owner, pricing.QuoteInput{Action: "outline", OutlineDraftID: in.DraftID, OutlineRevision: in.ExpectedRevision, RequestHash: snapshot.ContentHash})
		if err != nil {
			outlineError(w, 503)
			return
		}
		respond(w, 201, q)
		return
	}
	if tail != "" || in.QuoteID == "" {
		outlineError(w, 400)
		return
	}
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		outlineError(w, 503)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Lock the same head as manual edits and Apply so a submitted snapshot is atomic.
	var locked string
	if tx.QueryRow(ctx, `SELECT id::text FROM outline_drafts WHERE id::text=$1 AND learner_id::text=$2 FOR UPDATE`, in.DraftID, owner).Scan(&locked) != nil {
		outlineError(w, 404)
		return
	}
	var old Creation
	var revision int64
	var oldQuoteID string
	err = tx.QueryRow(ctx, `SELECT id::text,state,COALESCE(course_id::text,''),revision,quote_id::text FROM course_creations WHERE draft_id::text=$1 AND learner_id::text=$2`, in.DraftID, owner).Scan(&old.ID, &old.State, &old.CourseID, &revision, &oldQuoteID)
	if err == nil {
		if revision != in.ExpectedRevision {
			outlineError(w, 409)
			return
		}
		if old.State != "failed" || in.QuoteID == oldQuoteID {
			respond(w, 202, old)
			return
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		outlineError(w, 503)
		return
	}
	snapshot, err := resolveConfirmed(ctx, tx, owner, in.DraftID, in.ExpectedRevision)
	if err != nil {
		outlineError(w, 409)
		return
	}
	q, err := pricing.ReadQuote(ctx, tx, owner, in.QuoteID)
	if err != nil || q.Action != "outline" || !q.Accepted || q.Expired || q.Input.OutlineDraftID != in.DraftID || q.Input.OutlineRevision != in.ExpectedRevision || q.Input.RequestHash != snapshot.ContentHash {
		outlineError(w, 409)
		return
	}
	var briefRaw []byte
	if tx.QueryRow(ctx, `SELECT b.brief FROM course_brief_revisions b JOIN outline_drafts d ON d.brief_revision=b.id WHERE d.id::text=$1`, in.DraftID).Scan(&briefRaw) != nil {
		outlineError(w, 409)
		return
	}
	var brief Brief
	if json.Unmarshal(briefRaw, &brief) != nil {
		outlineError(w, 409)
		return
	}
	var profileRaw []byte
	if tx.QueryRow(ctx, `SELECT jsonb_build_object('learnerId',learner_id,'locale',locale,'background',background,'goals',goal,'level',level,'topic',topic) FROM learner_profiles WHERE learner_id::text=$1`, owner).Scan(&profileRaw) != nil {
		outlineError(w, 503)
		return
	}
	profile := new(enginev1.LearnerProfile)
	if protojson.Unmarshal(profileRaw, profile) != nil {
		outlineError(w, 503)
		return
	}
	outline := &enginev1.CourseOutline{Title: brief.Topic}
	for _, u := range snapshot.Outline {
		unit := &enginev1.Unit{UnitId: u.ID, Title: u.Title}
		for _, l := range u.Lessons {
			unit.Lessons = append(unit.Lessons, &enginev1.LessonRef{LessonId: l.ID, Title: l.Title, Goal: l.Goal})
		}
		outline.Units = append(outline.Units, unit)
	}
	request := &enginev1.GenerateCourseOutlineRequest{Topic: brief.Topic, Learner: profile, Brief: brief.proto(), EditedOutline: outline, OutlineRevision: snapshot.Revision, DraftId: snapshot.DraftID}
	raw, err := protojson.Marshal(request)
	if err != nil {
		outlineError(w, 400)
		return
	}
	var c Creation
	c.State = "queued"
	if old.ID != "" {
		c = old
		c.State = "queued"
		if err = tx.QueryRow(ctx, `UPDATE course_creations SET state='queued',quote_id=$2::uuid,revision=$3,request=$4,response=NULL,reservation_id=NULL WHERE id::text=$1 AND state='failed' RETURNING id::text`, old.ID, in.QuoteID, in.ExpectedRevision, raw).Scan(&c.ID); err != nil {
			outlineError(w, 409)
			return
		}
	} else {
		if tx.QueryRow(ctx, `INSERT INTO course_creations(learner_id,draft_id,revision,quote_id,request) VALUES($1::uuid,$2::uuid,$3,$4::uuid,$5) RETURNING id::text`, owner, in.DraftID, in.ExpectedRevision, in.QuoteID, raw).Scan(&c.ID) != nil {
			outlineError(w, 409)
			return
		}
	}
	queue, err := s.CreationQueue(slog.Default())
	if err != nil {
		outlineError(w, 503)
		return
	}
	if _, err = queue.InsertTx(ctx, tx, creationArgs{ID: c.ID}, &river.InsertOpts{Queue: "outlines", MaxAttempts: 5}); err != nil {
		outlineError(w, 503)
		return
	}
	if tx.Commit(ctx) != nil {
		outlineError(w, 503)
		return
	}
	respond(w, 202, c)
}
func (s *Service) creationEvents(w http.ResponseWriter, r *http.Request, owner, id string) {
	if _, err := s.creation(r.Context(), owner, id); err != nil {
		outlineError(w, 404)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	timer := time.NewTicker(200 * time.Millisecond)
	defer timer.Stop()
	previous := ""
	for {
		c, err := s.creation(r.Context(), owner, id)
		if err != nil {
			return
		}
		if c.State != previous {
			raw, _ := json.Marshal(c)
			if _, err = fmt.Fprintf(w, "event: stage\ndata: %s\n\n", raw); err != nil {
				return
			}
			if controller.Flush() != nil {
				return
			}
			previous = c.State
		}
		if c.State == "enrolled" || c.State == "failed" {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-timer.C:
		}
	}
}

// ValidateCreatedOutline rejects unknown protobuf payloads as well as changed text/order.
func ValidateCreatedOutline(request *enginev1.GenerateCourseOutlineRequest, response *enginev1.GenerateCourseOutlineResponse) error {
	invalid := errors.New("invalid confirmed outline output")
	if request == nil || response == nil || response.Unavailable || request.OutlineRevision < 1 || response.OutlineRevision != request.OutlineRevision || request.EditedOutline == nil || !proto.Equal(request.EditedOutline, response.Outline) || len(response.Attempts) == 0 {
		return invalid
	}
	if len(response.ProtoReflect().GetUnknown()) > 0 || len(response.Outline.ProtoReflect().GetUnknown()) > 0 {
		return invalid
	}
	o := Outline{}
	for _, u := range response.Outline.Units {
		if u == nil || len(u.ProtoReflect().GetUnknown()) > 0 {
			return invalid
		}
		unit := OutlineUnit{ID: u.UnitId, Title: u.Title, Lessons: []OutlineLesson{}}
		for _, l := range u.Lessons {
			if l == nil || len(l.ProtoReflect().GetUnknown()) > 0 {
				return invalid
			}
			unit.Lessons = append(unit.Lessons, OutlineLesson{ID: l.LessonId, Title: l.Title, Goal: l.Goal})
		}
		o = append(o, unit)
	}
	if o.Validate(true) != nil {
		return invalid
	}
	for _, u := range response.Attempts {
		if u != nil && u.Delivered && u.Component != "audio" {
			return nil
		}
	}
	return invalid
}

// RunCreation resumes durable validation/finalization; interrupted calls never spend twice.
func (s *Service) RunCreation(ctx context.Context, id string) error {
	if s.opts.Financial == nil {
		return errors.New("outline dependencies unavailable")
	}
	// The generation lock spans provider work. Keep it outside the work pool so
	// concurrent lock holders cannot consume the connections needed to finalize.
	conn, err := pgx.ConnectConfig(ctx, s.opts.Pool.Config().ConnConfig.Copy())
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()
	var locked bool
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,202))`, id).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return river.JobSnooze(time.Second)
	}
	defer func() {
		unlock, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_, _ = conn.Exec(unlock, `SELECT pg_advisory_unlock(hashtextextended($1,202))`, id)
	}()
	var owner, quoteID, state, reservation string
	var raw, responseRaw []byte
	if err = s.opts.Pool.QueryRow(ctx, `SELECT learner_id::text,quote_id::text,state,COALESCE(reservation_id::text,''),request,response FROM course_creations WHERE id::text=$1`, id).Scan(&owner, &quoteID, &state, &reservation, &raw, &responseRaw); errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if state == "enrolled" || state == "failed" {
		return nil
	}
	request := new(enginev1.GenerateCourseOutlineRequest)
	if err = protojson.Unmarshal(raw, request); err != nil {
		return err
	}
	if state == "generating" {
		return s.failCreation(ctx, owner, id, quoteID, state, reservation, nil)
	}
	if state == "queued" {
		if s.opts.OutlineEngine == nil {
			return errors.New("outline engine unavailable")
		}
		if ctx.Err() != nil {
			return s.failCreation(context.WithoutCancel(ctx), owner, id, quoteID, "queued", "", nil)
		}
		job, e := s.opts.Financial.ReserveAction(ctx, owner, quoteID, "outline")
		if e != nil || job.State != "reserved" {
			return s.failCreation(context.WithoutCancel(ctx), owner, id, quoteID, "queued", "", nil)
		}
		reservation = job.ID
		// Crash before this transition leaves a reserved job safely discoverable by quote.
		tag, err := s.opts.Pool.Exec(ctx, `UPDATE course_creations SET reservation_id=$2::uuid,state='generating' WHERE id::text=$1 AND state='queued' AND quote_id::text=$3`, id, reservation, quoteID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			_, err = s.opts.Financial.Settle(ctx, owner, reservation, nil)
			return err
		}
		state = "generating"
		request.Limits, err = s.opts.Financial.Limits(ctx, owner, reservation, "outline")
		if err != nil {
			return s.failCreation(ctx, owner, id, quoteID, state, reservation, nil)
		}
		if err = s.opts.Financial.Claim(ctx, owner, reservation); err != nil {
			return s.failCreation(ctx, owner, id, quoteID, state, reservation, nil)
		}
		callCtx, cancel := context.WithDeadline(ctx, job.DeadlineAt)
		response, e := s.opts.OutlineEngine.GenerateCourseOutline(callCtx, request)
		callErr := callCtx.Err()
		cancel()
		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer finishCancel()
		if e != nil || callErr != nil || ValidateCreatedOutline(request, response) != nil {
			var attempts []*enginev1.Usage
			if response != nil {
				attempts = response.Attempts
			}
			return s.failCreation(finishCtx, owner, id, quoteID, "generating", reservation, attempts)
		}
		responseRaw, err = protojson.Marshal(response)
		if err != nil {
			return err
		}
		tag, err = s.opts.Pool.Exec(finishCtx, `UPDATE course_creations SET state='validated',response=$2 WHERE id::text=$1 AND state='generating' AND reservation_id::text=$3`, id, responseRaw, reservation)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return nil
		}
		state = "validated"
	}
	response := new(enginev1.GenerateCourseOutlineResponse)
	if protojson.Unmarshal(responseRaw, response) != nil || ValidateCreatedOutline(request, response) != nil {
		return s.failCreation(ctx, owner, id, quoteID, state, reservation, nil)
	}
	var courseID string
	o := response.Outline
	persist := func(tx pgx.Tx) error {
		var locked string
		if err := tx.QueryRow(ctx, `SELECT id::text FROM course_creations WHERE id::text=$1 AND state='validated' AND reservation_id::text=$2 FOR UPDATE`, id, reservation).Scan(&locked); errors.Is(err, pgx.ErrNoRows) {
			return errStateLost
		} else if err != nil {
			return err
		}
		if err = tx.QueryRow(ctx, `INSERT INTO courses(learner_id,creation_id,title,description) VALUES($1::uuid,$2::uuid,$3,$4) RETURNING id::text`, owner, id, o.Title, o.Description).Scan(&courseID); err != nil {
			return err
		}
		for i, u := range o.Units {
			var unitID string
			if err = tx.QueryRow(ctx, `INSERT INTO course_units(learner_id,course_id,authored_id,position,title) VALUES($1::uuid,$2::uuid,$3,$4,$5) RETURNING id::text`, owner, courseID, u.UnitId, i, u.Title).Scan(&unitID); err != nil {
				return err
			}
			for j, l := range u.Lessons {
				status := "locked"
				if i == 0 && j == 0 {
					status = "ready_to_generate"
				}
				if _, err = tx.Exec(ctx, `INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status) VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$7)`, owner, unitID, l.LessonId, j, l.Title, l.Goal, status); err != nil {
					return err
				}
			}
		}
		if _, err = tx.Exec(ctx, `INSERT INTO course_enrollments(learner_id,course_id) VALUES($1::uuid,$2::uuid)`, owner, courseID); err != nil {
			return err
		}
		if err = tx.QueryRow(ctx, `UPDATE course_creations SET state='enrolled',course_id=$2::uuid WHERE id::text=$1 AND state='validated' AND reservation_id::text=$3 RETURNING id::text`, id, courseID, reservation).Scan(&locked); err != nil {
			return err
		}
		return nil
	}
	committed := false
	settled, err := s.opts.Financial.SettleWith(ctx, owner, reservation, response.Attempts, func(tx pgx.Tx, r credits.Reservation) error {
		if r.State != "settled" {
			return credits.ErrUsage
		}
		if err := persist(tx); err != nil {
			return err
		}
		committed = true
		return nil
	})
	if errors.Is(err, errStateLost) {
		return nil
	}
	if errors.Is(err, credits.ErrUsage) || err == nil && settled.State != "settled" {
		return s.failCreation(ctx, owner, id, quoteID, "validated", reservation, nil)
	}
	if err != nil {
		return err
	}
	// Legacy settled jobs predate atomic persistence. Complete their durable
	// validated response once; the expected-state row lock prevents duplicates.
	if !committed {
		tx, e := s.opts.Pool.Begin(ctx)
		if e != nil {
			return e
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if e = persist(tx); errors.Is(e, errStateLost) {
			return nil
		} else if e != nil {
			return e
		}
		if e = tx.Commit(ctx); e != nil {
			return e
		}
	}
	s.creationEvent(ctx, owner, "course_outline_generated", courseID, o, response.Attempts)
	s.creationEvent(ctx, owner, "course_enrolled", courseID, o, nil)
	return nil
}

// errStateLost rolls financial finalization back when another executor won.
var errStateLost = errors.New("course job state changed")

func (s *Service) failCreation(ctx context.Context, owner, id, quoteID, expected, reservation string, attempts []*enginev1.Usage) error {
	if reservation == "" {
		// Find the hold after a crash between reserve and recording its ID.
		err := s.opts.Pool.QueryRow(ctx, `SELECT j.id::text FROM course_creations c JOIN credit_jobs j ON j.quote_id=c.quote_id WHERE c.id::text=$1 AND c.state=$2 AND c.quote_id::text=$3`, id, expected, quoteID).Scan(&reservation)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	for _, a := range attempts {
		if a != nil {
			a.Delivered = false
		}
	}
	changed := false
	transition := func(tx pgx.Tx, _ credits.Reservation) error {
		tag, err := tx.Exec(ctx, `UPDATE course_creations SET state='failed' WHERE id::text=$1 AND state=$2 AND ($3='' OR reservation_id IS NULL OR reservation_id::text=$3) AND quote_id::text=$4`, id, expected, reservation, quoteID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return errStateLost
		}
		changed = true
		return nil
	}
	if reservation != "" {
		result, err := s.opts.Financial.SettleWith(ctx, owner, reservation, attempts, transition)
		if errors.Is(err, credits.ErrUsage) {
			result, err = s.opts.Financial.SettleWith(ctx, owner, reservation, nil, transition)
		}
		if errors.Is(err, errStateLost) {
			return nil
		}
		if err != nil {
			return err
		}
		if changed {
			s.creationEvent(ctx, owner, "course_generation_failed", "", nil, nil)
			return nil
		}
		if result.State == "settled" {
			return nil
		}
	}
	// Already released financial jobs skip SettleWith's callback; finish their
	// product state with the same compare-and-swap on retry.
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = transition(tx, credits.Reservation{}); errors.Is(err, errStateLost) {
		return nil
	} else if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	s.creationEvent(ctx, owner, "course_generation_failed", "", nil, nil)
	return nil
}
func (s *Service) creationEvent(ctx context.Context, owner, event, courseID string, o *enginev1.CourseOutline, attempts []*enginev1.Usage) {
	if s.opts.Events == nil {
		return
	}
	var optout bool
	if s.opts.Pool.QueryRow(ctx, `SELECT analytics_opt_out FROM learner_profiles WHERE learner_id::text=$1`, owner).Scan(&optout) != nil || optout {
		return
	}
	props := map[string]any{"source": "api", "platform": "web", "app_version": "0.1.0", "course_id": courseID, "stage": "outline"}
	if o != nil {
		props["unit_count"] = len(o.Units)
		n := 0
		for _, u := range o.Units {
			n += len(u.Lessons)
		}
		props["lesson_count"] = n
	}
	if event == "course_generation_failed" {
		props["error_code"] = "generation_unavailable"
	}
	for _, a := range attempts {
		if a != nil && a.Delivered {
			props["provider"] = a.Provider
			props["model"] = a.Model
			props["latency_ms"] = a.LatencyMs
		}
	}
	_ = s.opts.Events.Capture(owner, event, props)
}

// ServeCourseHTTP only exposes an enrolled owner's persisted path.
func (s *Service) ServeCourseHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	owner := accounts.LearnerID(r.Context())
	if owner == "" {
		outlineError(w, 401)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		id = strings.TrimPrefix(r.URL.Path, "/v1/courses/")
	}
	var raw []byte
	err := s.opts.Pool.QueryRow(r.Context(), `SELECT jsonb_build_object('id',c.id,'title',c.title,'description',c.description,'level_override',COALESCE(e.level_override,''),'units',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',u.id,'title',u.title,'lessons',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',l.id,'title',l.title,'goal',l.goal,'state',l.status) ORDER BY l.position) FROM course_lesson_stubs l WHERE l.unit_id=u.id),'[]'::jsonb)) ORDER BY u.position) FROM course_units u WHERE u.course_id=c.id),'[]'::jsonb)) FROM courses c JOIN course_enrollments e ON e.course_id=c.id WHERE c.id::text=$1 AND e.learner_id::text=$2 AND c.deleted_at IS NULL`, id, owner).Scan(&raw)
	if err != nil {
		outlineError(w, 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}
