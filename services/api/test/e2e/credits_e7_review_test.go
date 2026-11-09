package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestE7ReviewCreationSubmissionSingleConnection(t *testing.T) {
	auth, token, db := signedInDatabase(t)
	ctx := t.Context()
	claims, err := auth.ParseAccess(token)
	if err != nil {
		t.Fatal(err)
	}
	owner := claims.Subject
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err = worker.Migrate(ctx, db.Pool, logger); err != nil {
		t.Fatal(err)
	}
	outline := courses.Outline{{ID: "u1", Title: "Moon", Lessons: []courses.OutlineLesson{{ID: "l1", Title: "Phases", Goal: "Explain phases"}}}}
	raw, err := json.Marshal(outline)
	if err != nil {
		t.Fatal(err)
	}
	var draft string
	if err = db.Pool.QueryRow(ctx, `WITH b AS (INSERT INTO course_brief_revisions(learner_id,brief,content_hash) VALUES($1,'{"topic":"Sky"}',repeat('a',64)) RETURNING id)
 INSERT INTO outline_drafts(learner_id,brief_revision,outline) SELECT $1,id,$2 FROM b RETURNING id::text`, owner, raw).Scan(&draft); err != nil {
		t.Fatal(err)
	}
	confirmed, err := json.Marshal(courses.ConfirmedOutline{DraftID: draft, Revision: 1, ContentHash: outline.Hash(), Outline: outline})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, "UPDATE outline_drafts SET confirmed=$2 WHERE id::text=$1", draft, confirmed); err != nil {
		t.Fatal(err)
	}
	quotes := pricing.NewQuotes(db.Pool)
	q, err := quotes.Create(ctx, owner, pricing.QuoteInput{Action: "outline", OutlineDraftID: draft, OutlineRevision: 1, RequestHash: outline.Hash()})
	if err != nil {
		t.Fatal(err)
	}
	if err = quotes.Accept(ctx, owner, q.ID); err != nil {
		t.Fatal(err)
	}
	cfg := db.Pool.Config()
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	service := courses.New(courses.Options{Pool: pool})
	api := httpapi.New(httpapi.Dependencies{Accounts: auth, CourseBrief: service}, logger)
	body, err := json.Marshal(map[string]any{"draft_id": draft, "expected_revision": 1, "quote_id": q.ID})
	if err != nil {
		t.Fatal(err)
	}
	bounded, stop := context.WithTimeout(ctx, 3*time.Second)
	defer stop()
	req := httptest.NewRequestWithContext(bounded, "POST", "/v1/course-creations", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)
	if w.Code != 202 {
		t.Fatalf("constrained submission exhausted pool: %d %s", w.Code, w.Body)
	}
}

func TestE7ReviewTimezoneWritesAndDeletion(t *testing.T) {
	for _, path := range []string{"/v1/me/profile", "/v1/me/settings"} {
		t.Run(path, func(t *testing.T) {
			auth, token, db := signedInDatabase(t)
			ctx := t.Context()
			claims, err := auth.ParseAccess(token)
			if err != nil {
				t.Fatal(err)
			}
			owner := claims.Subject
			cat, err := pricing.Load(ctx, db.Queries)
			if err != nil {
				t.Fatal(err)
			}
			financial := credits.NewServiceWithCatalogue(db.Pool, nil, cat, nil)
			before := time.Now()
			body := `{"confirmed":true,"action":"save","fields":{"time_zone":"America/New_York"}}`
			if path == "/v1/me/settings" {
				body = `{"confirmed":true,"fields":{"time_zone":"America/New_York"}}`
			}
			req := httptest.NewRequestWithContext(ctx, "POST", path, strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			auth.ServeHTTP(w, req)
			if w.Code != 200 {
				t.Fatalf("save %d %s", w.Code, w.Body)
			}
			for _, check := range []struct {
				at   time.Time
				want string
			}{{before.Add(time.Hour), "UTC"}, {time.Now().Add(25 * time.Hour), "America/New_York"}} {
				got, e := financial.GetEffectiveTimezone(ctx, owner, check.at)
				if e != nil || got != check.want {
					t.Errorf("timezone at %s = %s, want %s: %v", check.at, got, check.want, e)
				}
			}
			// Repeated unrelated writes must not postpone or duplicate a pending change.
			req = httptest.NewRequestWithContext(ctx, "POST", path, strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+token)
			w = httptest.NewRecorder()
			auth.ServeHTTP(w, req)
			if w.Code != 200 {
				t.Fatal(w.Body)
			}
			var records int
			if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_timezones WHERE learner_id=$1", owner).Scan(&records); err != nil || records != 2 {
				t.Errorf("timezone history=%d, want initial + delayed: %v", records, err)
			}
			quote, err := pricing.NewQuotes(db.Pool).Create(ctx, owner, pricing.QuoteInput{Action: "quick_note"})
			if err != nil {
				t.Fatal(err)
			}
			if err = pricing.NewQuotes(db.Pool).Accept(ctx, owner, quote.ID); err != nil {
				t.Fatal(err)
			}
			job, err := financial.Reserve(ctx, owner, quote.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err = financial.Claim(ctx, owner, job.ID); err != nil {
				t.Fatal(err)
			}
			if path == "/v1/me/settings" {
				// Restore startup replays a durable deletion before serving traffic.
				if _, err = db.Pool.Exec(ctx, "INSERT INTO account_deletion_tombstones(learner_reference) VALUES($1)", accounts.LearnerReference(owner)); err != nil {
					t.Fatal(err)
				}
				if err = accounts.ApplyDeletionTombstones(ctx, db.Pool); err != nil {
					t.Fatal(err)
				}
			} else {
				req = httptest.NewRequestWithContext(ctx, "POST", "/v1/me/deletion-request", strings.NewReader(`{"confirmation":"DELETE"}`))
				req.Header.Set("Authorization", "Bearer "+token)
				w = httptest.NewRecorder()
				auth.ServeHTTP(w, req)
				if w.Code != 200 {
					t.Fatalf("delete %d %s", w.Code, w.Body)
				}
			}
			if err = financial.ReapExpired(ctx); err != nil {
				t.Fatal(err)
			}
			var state string
			var held, spent int64
			var raw []byte
			if err = db.Pool.QueryRow(ctx, "SELECT state,result FROM credit_jobs WHERE id=$1 AND learner_id IS NULL", job.ID).Scan(&state, &raw); err != nil {
				t.Fatal(err)
			}
			var result credits.Reservation
			if err = json.Unmarshal(raw, &result); err != nil {
				t.Fatal(err)
			}
			if err = db.Pool.QueryRow(ctx, "SELECT sum(held_nanos),sum(spent_nanos) FROM credit_cost_counters").Scan(&held, &spent); err != nil {
				t.Fatal(err)
			}
			if state != "released" || result.CostNanos <= 0 || held != 0 || spent != 2*result.CostNanos {
				t.Errorf("deleted job state=%s result=%+v counters=%d/%d", state, result, held, spent)
			}
			var net int64
			if err = db.Pool.QueryRow(ctx, "SELECT COALESCE(sum(amount),0) FROM credit_entries WHERE owner_reference=$1 AND kind IN ('reserve','release')", accounts.LearnerReference(owner)).Scan(&net); err != nil || net != 0 {
				t.Errorf("anonymous stranded holds=%d: %v", net, err)
			}
		})
	}
}
