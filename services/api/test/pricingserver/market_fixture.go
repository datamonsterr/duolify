package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// seedMarketCourse gives the caller an account old enough to list (E15-07) and a Course with
// three generated Lessons plus one still to generate. The email is already verified by sign-in.
func seedMarketCourse(w http.ResponseWriter, r *http.Request, pool *pgxpool.Pool) {
	var in struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil || strings.TrimSpace(in.Title) == "" {
		http.Error(w, "title required", http.StatusBadRequest)
		return
	}
	owner, ctx := accounts.LearnerID(r.Context()), r.Context()
	tx, err := pool.Begin(ctx)
	if err != nil {
		http.Error(w, "seed unavailable", http.StatusServiceUnavailable)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var quote, creation, course, unit string
	steps := []func() error{
		func() error {
			_, e := tx.Exec(ctx, `UPDATE learners SET created_at = now() - interval '10 days' WHERE id=$1`, owner)
			return e
		},
		func() error {
			return tx.QueryRow(ctx, `INSERT INTO pricing_quotes(learner_id,owner_reference,pricing_version,snapshot,created_at,expires_at) SELECT $1::uuid,encode(sha256(convert_to($1::text,'UTF8')),'hex'),version,'{}',now(),now()+interval '1 hour' FROM pricing_quote_policies ORDER BY version LIMIT 1 RETURNING id::text`, owner).Scan(&quote)
		},
		func() error {
			return tx.QueryRow(ctx, `INSERT INTO course_creations(learner_id,draft_id,revision,quote_id,request,state) VALUES($1,gen_random_uuid(),1,$2,'{}','enrolled') RETURNING id::text`, owner, quote).Scan(&creation)
		},
		func() error {
			return tx.QueryRow(ctx, `INSERT INTO courses(learner_id,creation_id,title,description) VALUES($1,$2,$3,$4) RETURNING id::text`, owner, creation, in.Title, in.Description).Scan(&course)
		},
		func() error {
			_, e := tx.Exec(ctx, `INSERT INTO course_enrollments(learner_id,course_id) VALUES($1,$2)`, owner, course)
			return e
		},
		func() error {
			return tx.QueryRow(ctx, `INSERT INTO course_units(learner_id,course_id,authored_id,position,title) VALUES($1,$2,'u1',0,'Getting started') RETURNING id::text`, owner, course).Scan(&unit)
		},
	}
	for i := 0; i < 3; i++ {
		i := i
		steps = append(steps, func() error { return seedMarketLesson(ctx, tx, owner, course, unit, i) })
	}
	steps = append(steps, func() error {
		_, e := tx.Exec(ctx, `INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status) VALUES($1,$2,'l3',3,'Going further','Go further','locked')`, owner, unit)
		return e
	})
	for _, step := range steps {
		if err = step(); err != nil {
			http.Error(w, "seed unavailable: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
	}
	if err = tx.Commit(ctx); err != nil {
		http.Error(w, "seed unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"course_id": course})
}

func seedMarketLesson(ctx context.Context, tx pgx.Tx, owner, course, unit string, i int) error {
	var stub string
	title := fmt.Sprintf("Lesson %d", i+1)
	if err := tx.QueryRow(ctx, `INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status) VALUES($1,$2,$3,$4,$5,'Learn the idea','done') RETURNING id::text`, owner, unit, fmt.Sprintf("l%d", i), i, title).Scan(&stub); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO generated_lessons(lesson_stub_id,course_id,learner_id,title,manifest) VALUES($1,$2,$3,$4,'{}'::jsonb)`, stub, course, owner, title); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO lesson_slides(lesson_stub_id,learner_id,position,slide_id,html,duration_ms,narration) VALUES($1,$2,0,$3,$4,3000,'Narration.')`, stub, owner, fmt.Sprintf("s%d", i), fmt.Sprintf("<h1>%s</h1><p>A small, safe idea.</p>", title)); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO lesson_questions(lesson_stub_id,learner_id,position,topic_id,prompt,choices,answer_index,explanation) VALUES($1,$2,0,'t','Which one?','["Right","Wrong"]'::jsonb,0,'Because.')`, stub, owner)
	return err
}

// setMarketStats stands in for the ratings slice (E15-08): it writes the rating inputs only.
func setMarketStats(w http.ResponseWriter, r *http.Request, pool *pgxpool.Pool) {
	var in struct {
		CourseID    string `json:"course_id"`
		RatingSum   int64  `json:"rating_sum"`
		RatingCount int    `json:"rating_count"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in) != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	tag, err := pool.Exec(r.Context(), `UPDATE marketplace_listing_stats SET rating_sum=$2, rating_count=$3 WHERE course_id=$1`, in.CourseID, in.RatingSum, in.RatingCount)
	if err != nil || tag.RowsAffected() != 1 {
		http.Error(w, "stats unavailable", http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
