package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/engine"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"google.golang.org/grpc"
)

type financialPreviewEngine struct {
	fakeEngine
	preview func(context.Context, *enginev1.PreviewCourseRequest) (*enginev1.PreviewCourseResponse, error)
}

type delayedClaim struct{ *credits.Service }

func (f delayedClaim) Claim(ctx context.Context, owner, id string) error {
	if err := f.Service.Claim(ctx, owner, id); err != nil {
		return err
	}
	select {
	case <-time.After(40 * time.Millisecond):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *financialPreviewEngine) PreviewCourse(ctx context.Context, r *enginev1.PreviewCourseRequest) (*enginev1.PreviewCourseResponse, error) {
	return e.preview(ctx, r)
}

func TestPreviewFinancialDelivery(t *testing.T) {
	auth, token, db := signedInDatabase(t)
	claims, err := auth.ParseAccess(token)
	if err != nil {
		t.Fatal(err)
	}
	owner := claims.Subject
	cat, err := pricing.Load(t.Context(), db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	financial := credits.NewServiceWithCatalogue(db.Pool, nil, cat, nil)
	if _, err = db.Pool.Exec(t.Context(), "INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version) VALUES($1,1000,'purchase','preview-race',$2)", owner, cat.Config().Version); err != nil {
		t.Fatal(err)
	}
	fixture := &financialPreviewEngine{}
	lis, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer()
	enginev1.RegisterEngineServiceServer(rpc, fixture)
	go func() { _ = rpc.Serve(lis) }()
	defer rpc.Stop()
	client, err := engine.New("http://"+lis.Addr().String(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{Accounts: auth, Credits: delayedClaim{financial}, Quotes: pricing.NewQuotes(db.Pool), CourseBrief: courses.New(courses.Options{Pool: db.Pool}), Engine: client}, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()
	call := func(path, body, quote string) (int, map[string]any) {
		t.Helper()
		req, e := http.NewRequestWithContext(t.Context(), "POST", server.URL+path, strings.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Quote-ID", quote)
		resp, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer func() { _ = resp.Body.Close() }()
		var out map[string]any
		if e = json.NewDecoder(resp.Body).Decode(&out); e != nil {
			t.Fatal(e)
		}
		return resp.StatusCode, out
	}
	for _, mode := range []string{"deadline", "cleanup", "absent-usage"} {
		t.Run(mode, func(t *testing.T) {
			code, brief := call("/v1/course-brief/confirm", `{"confirmed":true,"brief":{"topic":"Sky"}}`, "")
			if code != 201 {
				t.Fatal(code, brief)
			}
			revision := brief["id"].(string)
			code, q := call("/v1/quotes", `{"action":"preview","brief_revision":"`+revision+`"}`, "")
			if code != 200 {
				t.Fatal(code, q)
			}
			quote := q["id"].(string)
			if code, _ = call("/v1/quotes/"+quote+"/accept", `{}`, ""); code != 200 {
				t.Fatal(code)
			}
			observed := make(chan error, 1)
			fixture.preview = func(ctx context.Context, r *enginev1.PreviewCourseRequest) (*enginev1.PreviewCourseResponse, error) {
				response, e := new(fakeEngine).PreviewCourse(ctx, r)
				if mode == "deadline" {
					var deadline time.Time
					e = db.Pool.QueryRow(t.Context(), "SELECT deadline_at FROM credit_jobs WHERE quote_id::text=$1", quote).Scan(&deadline)
					if e == nil && r.Limits.DeadlineUnixMs != uint64(deadline.UnixMilli()) {
						e = fmt.Errorf("provider absolute deadline %v differs from reservation deadline %v", r.Limits.DeadlineUnixMs, deadline)
					}
				}
				if mode == "cleanup" {
					_, e = db.Pool.Exec(t.Context(), "UPDATE credit_jobs SET deadline_at=clock_timestamp()-interval '1 second' WHERE quote_id::text=$1", quote)
					if e == nil {
						e = financial.ReapExpired(t.Context())
					}
				}
				if mode == "absent-usage" {
					response.Attempts[0].InputTokens, response.Attempts[0].OutputTokens = 0, 0
					response.Attempts[0].UsageUnavailable = true
				}
				observed <- e
				return response, nil
			}
			code, body := call("/v1/courses/preview", `{"brief_revision":"`+revision+`"}`, quote)
			select {
			case e := <-observed:
				if e != nil {
					t.Error(e)
				}
			default:
				t.Fatalf("provider never called: %d %v", code, body)
			}
			if mode == "cleanup" {
				if code != 502 || body["sample_outline"] != nil || body["example_slides"] != nil {
					t.Fatalf("delivered after cleanup: %d %v", code, body)
				}
			} else if code != 200 {
				t.Fatal(code, body)
			}
			if mode == "absent-usage" {
				var cost int64
				if err := db.Pool.QueryRow(t.Context(), "SELECT (result->>'cost_nanos')::bigint FROM credit_jobs WHERE quote_id::text=$1", quote).Scan(&cost); err != nil || cost <= 0 {
					t.Fatalf("unknown usage lost cost: %d %v", cost, err)
				}
			}
		})
	}
}
