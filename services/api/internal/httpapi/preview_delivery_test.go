package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
)

type previewAuth struct{}

func (previewAuth) ServeHTTP(http.ResponseWriter, *http.Request) {}
func (previewAuth) Protect(h http.Handler) http.Handler          { return h }

type previewBrief struct{}

func (previewBrief) ServeHTTP(http.ResponseWriter, *http.Request) {}
func (previewBrief) PreparePreview(context.Context, string, string, string) (*enginev1.PreviewCourseRequest, error) {
	return &enginev1.PreviewCourseRequest{}, nil
}
func (previewBrief) PreviewEvent(context.Context, string, string, *enginev1.PreviewCourseRequest, string, int64) {
}

type previewFinance struct {
	deadline      time.Time
	state         string
	settleErr     error
	claimDeadline time.Time
}

func (f *previewFinance) ServeHTTP(http.ResponseWriter, *http.Request) {}
func (f *previewFinance) ReserveAction(context.Context, string, string, string) (credits.Reservation, error) {
	return credits.Reservation{ID: "job", State: "reserved", DeadlineAt: f.deadline}, nil
}
func (*previewFinance) Limits(context.Context, string, string, string) (*enginev1.UsageLimits, error) {
	return &enginev1.UsageLimits{TimeoutMs: 1000}, nil
}
func (f *previewFinance) Claim(ctx context.Context, _, _ string) error {
	f.claimDeadline, _ = ctx.Deadline()
	return nil
}
func (f *previewFinance) Settle(context.Context, string, string, []*enginev1.Usage) (credits.Reservation, error) {
	return credits.Reservation{State: f.state}, f.settleErr
}
func (*previewFinance) Release(context.Context, string, string) (credits.Reservation, error) {
	return credits.Reservation{State: "released"}, nil
}

type deliveryEngine struct {
	preview func(context.Context, *enginev1.PreviewCourseRequest) (*enginev1.PreviewCourseResponse, error)
}

func (deliveryEngine) Health(context.Context) error { return nil }
func (deliveryEngine) SetupChat(context.Context, *enginev1.SetupChatRequest) (*enginev1.SetupChatResponse, error) {
	return nil, nil
}
func (e deliveryEngine) PreviewCourse(ctx context.Context, r *enginev1.PreviewCourseRequest) (*enginev1.PreviewCourseResponse, error) {
	return e.preview(ctx, r)
}

func TestPreviewRequiresDeadlineAndSuccessfulSettlement(t *testing.T) {
	for _, mode := range []string{"settled", "released", "failed", "settlement-error", "late-response"} {
		t.Run(mode, func(t *testing.T) {
			f := &previewFinance{deadline: time.Now().Add(30 * time.Millisecond), state: mode}
			if mode == "settlement-error" {
				f.settleErr = errors.New("commit failed")
			}
			if mode == "late-response" {
				f.state = "settled"
			}
			e := deliveryEngine{preview: func(ctx context.Context, _ *enginev1.PreviewCourseRequest) (*enginev1.PreviewCourseResponse, error) {
				if d, ok := ctx.Deadline(); !ok || !d.Equal(f.deadline) {
					t.Errorf("provider deadline %v != reserved %v", d, f.deadline)
				}
				if !f.claimDeadline.Equal(f.deadline) {
					t.Errorf("claim deadline %v != reserved %v", f.claimDeadline, f.deadline)
				}
				if mode == "late-response" {
					<-ctx.Done()
				}
				return &enginev1.PreviewCourseResponse{SampleOutline: &enginev1.CourseOutline{Title: "Private sample", Units: []*enginev1.Unit{{Title: "Unit", Lessons: []*enginev1.LessonRef{{Title: "Lesson", Goal: "Learn"}}}}}, ExampleSlides: []*enginev1.Slide{{SlideId: "slide", Html: `<div id="root">Example</div>`, Narration: "Example", DurationMs: 1000}}, Attempts: []*enginev1.Usage{{Delivered: true}}}, nil
			}}
			h := New(Dependencies{Accounts: previewAuth{}, CourseBrief: previewBrief{}, Credits: f, Engine: e}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			r := httptest.NewRequestWithContext(t.Context(), "POST", "/v1/courses/preview", strings.NewReader(`{"brief_revision":"brief"}`))
			r.Header.Set("X-Quote-ID", "quote")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if mode == "settled" {
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body)
				}
			} else if w.Code != 502 || strings.Contains(w.Body.String(), "Private sample") {
				t.Fatalf("delivered without finalization: %d %s", w.Code, w.Body)
			}
		})
	}
}
