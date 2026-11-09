//go:build integration

package courses_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"google.golang.org/protobuf/encoding/protojson"
)

type reviewCreationEngine struct {
	run func(context.Context, *enginev1.GenerateCourseOutlineRequest) (*enginev1.GenerateCourseOutlineResponse, error)
}

func (e reviewCreationEngine) GenerateCourseOutline(ctx context.Context, r *enginev1.GenerateCourseOutlineRequest) (*enginev1.GenerateCourseOutlineResponse, error) {
	return e.run(ctx, r)
}

func TestE7ReviewCreationFinancialBoundary(t *testing.T) {
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("creation_review"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)
	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err = migrations.Up(ctx, url); err != nil {
		t.Fatal(err)
	}
	db, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cat, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"one pool connection", "absolute deadline", "cleanup before delivery", "unknown usage"} {
		t.Run(mode, func(t *testing.T) {
			cfg, e := pgxpool.ParseConfig(url)
			if e != nil {
				t.Fatal(e)
			}
			if mode == "one pool connection" {
				cfg.MaxConns = 1
			}
			pool, e := pgxpool.NewWithConfig(ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			financial := credits.NewServiceWithCatalogue(pool, nil, cat, nil)
			var owner string
			if e = db.Pool.QueryRow(ctx, "INSERT INTO learners(display_name) VALUES('creation review') RETURNING id::text").Scan(&owner); e != nil {
				t.Fatal(e)
			}
			if _, e = db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version) VALUES($1::uuid,$2,'purchase',($1::uuid)::text,$3)`, owner, cat.Config().Packs[0].Credits, cat.Config().Version); e != nil {
				t.Fatal(e)
			}
			quotes := pricing.NewQuotes(db.Pool)
			var draft string
			if e = db.Pool.QueryRow(ctx, `WITH b AS (INSERT INTO course_brief_revisions(learner_id,brief,content_hash) VALUES($1,'{"topic":"Sky"}',repeat('a',64)) RETURNING id) INSERT INTO outline_drafts(learner_id,brief_revision,outline,history) SELECT $1,id,'[]','[]' FROM b RETURNING id::text`, owner).Scan(&draft); e != nil {
				t.Fatal(e)
			}
			q, e := quotes.Create(ctx, owner, pricing.QuoteInput{Action: "outline", OutlineDraftID: draft, OutlineRevision: 1, RequestHash: strings.Repeat("a", 64)})
			if e != nil {
				t.Fatal(e)
			}
			if e = quotes.Accept(ctx, owner, q.ID); e != nil {
				t.Fatal(e)
			}
			request := &enginev1.GenerateCourseOutlineRequest{OutlineRevision: 1, EditedOutline: &enginev1.CourseOutline{Title: "Sky", Units: []*enginev1.Unit{{UnitId: "u1", Title: "Moon", Lessons: []*enginev1.LessonRef{{LessonId: "l1", Title: "Phases", Goal: "Explain phases"}}}}}}
			raw, e := protojson.Marshal(request)
			if e != nil {
				t.Fatal(e)
			}
			var id string
			if e = db.Pool.QueryRow(ctx, `INSERT INTO course_creations(learner_id,draft_id,revision,quote_id,request) VALUES($1,gen_random_uuid(),1,$2,$3) RETURNING id::text`, owner, q.ID, raw).Scan(&id); e != nil {
				t.Fatal(e)
			}
			engine := reviewCreationEngine{run: func(callCtx context.Context, r *enginev1.GenerateCourseOutlineRequest) (*enginev1.GenerateCourseOutlineResponse, error) {
				var job string
				var stored time.Time
				if e := db.Pool.QueryRow(ctx, "SELECT id::text,deadline_at FROM credit_jobs WHERE quote_id=$1", q.ID).Scan(&job, &stored); e != nil {
					t.Error(e)
					return nil, e
				}
				if mode == "absolute deadline" {
					deadline, ok := callCtx.Deadline()
					if !ok || !deadline.Equal(stored) {
						t.Errorf("provider deadline %s differs from reservation %s", deadline, stored)
					}
				}
				if mode == "cleanup before delivery" {
					if _, e := db.Pool.Exec(ctx, "UPDATE credit_jobs SET deadline_at=clock_timestamp()-interval '1 second' WHERE id=$1", job); e != nil {
						t.Error(e)
						return nil, e
					}
					if e := financial.ReapExpired(ctx); e != nil {
						t.Error(e)
						return nil, e
					}
				}
				return &enginev1.GenerateCourseOutlineResponse{Outline: r.EditedOutline, OutlineRevision: r.OutlineRevision, Attempts: []*enginev1.Usage{{Provider: r.Limits.Provider, Model: r.Limits.Model, Component: "content", Attempt: 1, Delivered: true, InputTokens: 100, OutputTokens: 20, UsageUnavailable: mode == "unknown usage"}}}, nil
			}}
			service := courses.New(courses.Options{Pool: pool, Financial: financial, OutlineEngine: engine})
			timeout := time.Duration(q.Bounds["timeout_ms"])*time.Millisecond + 5*time.Second
			if mode == "one pool connection" {
				timeout = 5 * time.Second
			}
			bounded, stop := context.WithTimeout(ctx, timeout)
			defer stop()
			if e = service.RunCreation(bounded, id); e != nil {
				t.Fatal(e)
			}
			var state string
			var count int
			var resultRaw []byte
			if e = db.Pool.QueryRow(ctx, "SELECT c.state,j.result,(SELECT count(*) FROM courses WHERE creation_id=c.id) FROM course_creations c JOIN credit_jobs j ON j.id=c.reservation_id WHERE c.id=$1", id).Scan(&state, &resultRaw, &count); e != nil {
				t.Fatal(e)
			}
			var result credits.Reservation
			if e = json.Unmarshal(resultRaw, &result); e != nil {
				t.Fatal(e)
			}
			if mode == "cleanup before delivery" {
				if state != "failed" || count != 0 || result.State != "released" {
					t.Fatalf("released generation delivered: %s %d %+v", state, count, result)
				}
			} else if state != "enrolled" || count != 1 || result.State != "settled" {
				t.Fatalf("generation lost: %s %d %+v", state, count, result)
			}
			if mode == "unknown usage" && (result.ContentSpent != q.ContentCeiling || result.CostNanos <= 0) {
				t.Fatalf("unknown usage discounted: %+v quote=%+v", result, q)
			}
		})
	}
}
