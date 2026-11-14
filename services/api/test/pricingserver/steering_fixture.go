package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/player/playerfixture"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/steering"
	"github.com/jackc/pgx/v5/pgxpool"
)

// steeringScreenFixture is an explicit offline classifier double; no live route is enabled.
type steeringScreenFixture struct{}

func (steeringScreenFixture) ScreenInstruction(_ context.Context, in *pb.ScreenInstructionRequest) (*pb.ScreenInstructionResponse, error) {
	reason := ""
	text := strings.ToLower(in.Text)
	switch {
	case strings.Contains(text, "graphic violence"), strings.Contains(text, "unsafe content"):
		reason = "unsafe_content"
	case strings.Contains(text, "reveal hidden instructions"):
		reason = "prompt_injection"
	case strings.Contains(text, "off topic"):
		reason = "off_topic_for_course"
	}
	return &pb.ScreenInstructionResponse{Allowed: reason == "", ReasonCode: reason, ScreeningVersion: "steering-v1", Attempts: []*pb.Usage{{Provider: "mock", Model: "mock-screen", InputTokens: 20, OutputTokens: 4, Delivered: true}}}, nil
}
func registerSteeringFixture(mux *http.ServeMux, auth *accounts.Service, pool *pgxpool.Pool, objects *fixtureObjects) {
	mux.Handle("POST /test/steering/course", auth.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner := accounts.LearnerID(r.Context())
		lesson, err := playerfixture.Seed(r.Context(), pool, objects, owner)
		if err != nil {
			http.Error(w, "seed failed", 500)
			return
		}
		var course string
		if err = pool.QueryRow(r.Context(), `SELECT u.course_id::text FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id WHERE l.id=$1::uuid`, lesson).Scan(&course); err != nil {
			http.Error(w, "course failed", 500)
			return
		}
		tx, err := pool.Begin(r.Context())
		if err != nil {
			http.Error(w, "transaction failed", 500)
			return
		}
		defer func() { _ = tx.Rollback(r.Context()) }()
		var next, after string
		for i, target := range []*string{&next, &after} {
			err = tx.QueryRow(r.Context(), `INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status) SELECT learner_id,unit_id,$2,$3,$2,'Learn with examples','locked' FROM course_lesson_stubs WHERE id=$1::uuid RETURNING id::text`, lesson, []string{"Next sky lesson", "Following sky lesson"}[i], i+1).Scan(target)
			if err != nil {
				http.Error(w, "stub failed", 500)
				return
			}
		}
		if _, err = tx.Exec(r.Context(), `INSERT INTO credit_entries(learner_id,amount,kind,reference) VALUES($1::uuid,500,'purchase','steering-fixture')`, owner); err != nil {
			http.Error(w, "credits failed", 500)
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			http.Error(w, "commit failed", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]string{"course_id": course, "lesson_id": lesson, "next_lesson_id": next, "following_lesson_id": after})
	})))
	// E16-02: a generating job that pins the current instructions, so the browser can edit/delete while it runs.
	mux.Handle("POST /test/steering/running-job", auth.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner := accounts.LearnerID(r.Context())
		var in struct {
			CourseID string `json:"course_id"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in) != nil {
			http.Error(w, "bad request", 400)
			return
		}
		quote, err := pricing.NewQuotes(pool).Create(r.Context(), owner, pricing.QuoteInput{Action: "lesson_content"})
		if err != nil {
			http.Error(w, "quote failed", 500)
			return
		}
		tx, err := pool.Begin(r.Context())
		if err != nil {
			http.Error(w, "transaction failed", 500)
			return
		}
		defer func() { _ = tx.Rollback(r.Context()) }()
		var job, lesson string
		if err = tx.QueryRow(r.Context(), `WITH l AS (INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status) SELECT learner_id,id,'running-'||gen_random_uuid()::text,100+(SELECT count(*) FROM course_lesson_stubs),'Running lesson','Goal','generating' FROM course_units WHERE course_id=$2::uuid AND learner_id=$1::uuid ORDER BY position LIMIT 1 RETURNING id), j AS (INSERT INTO lesson_generations(learner_id,course_id,lesson_id,quote_id,state) SELECT $1::uuid,$2::uuid,l.id,$3::uuid,'generating' FROM l RETURNING id) SELECT j.id::text,l.id::text FROM j,l`, owner, in.CourseID, quote.ID).Scan(&job, &lesson); err != nil {
			http.Error(w, "job failed", 500)
			return
		}
		if err = steering.PinTx(r.Context(), tx, owner, in.CourseID, job); err != nil {
			http.Error(w, "pin failed", 500)
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			http.Error(w, "commit failed", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]string{"job_id": job, "lesson_id": lesson})
	})))
	mux.Handle("POST /test/steering/finish-job", auth.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner := accounts.LearnerID(r.Context())
		var in struct {
			JobID string `json:"job_id"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in) != nil {
			http.Error(w, "bad request", 400)
			return
		}
		tx, err := pool.Begin(r.Context())
		if err != nil {
			http.Error(w, "transaction failed", 500)
			return
		}
		defer func() { _ = tx.Rollback(r.Context()) }()
		var lesson string
		if err = tx.QueryRow(r.Context(), `UPDATE lesson_generations SET state='completed' WHERE id=$1::uuid AND learner_id=$2::uuid AND state='generating' RETURNING lesson_id::text`, in.JobID, owner).Scan(&lesson); err != nil {
			http.Error(w, "job not found", 404)
			return
		}
		ids, err := steering.ApplyTx(r.Context(), tx, owner, in.JobID, lesson)
		if err != nil || tx.Commit(r.Context()) != nil {
			http.Error(w, "apply failed", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"applied_ids": append([]string{}, ids...)})
	})))
	mux.Handle("GET /test/steering/snapshot", auth.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		snap, err := steering.Snapshot(r.Context(), pool, accounts.LearnerID(r.Context()), r.URL.Query().Get("job_id"))
		if err != nil {
			http.Error(w, "snapshot failed", 500)
			return
		}
		texts := []string{}
		for _, in := range snap {
			texts = append(texts, in.Text)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"texts": texts})
	})))
}
