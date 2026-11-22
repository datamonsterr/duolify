package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
)

// A live preview takes longer than the server's 20 s WriteTimeout, but its quote allows minutes:
// the handler must extend its write deadline to the reservation, or the learner's browser sees
// a dropped connection after the preview was paid for.
func TestPreviewOutlivesTheServerWriteTimeout(t *testing.T) {
	f := &previewFinance{deadline: time.Now().Add(5 * time.Second), state: "settled"}
	e := deliveryEngine{preview: func(context.Context, *enginev1.PreviewCourseRequest) (*enginev1.PreviewCourseResponse, error) {
		time.Sleep(300 * time.Millisecond)
		return &enginev1.PreviewCourseResponse{SampleOutline: &enginev1.CourseOutline{Title: "Private sample", Units: []*enginev1.Unit{{Title: "Unit", Lessons: []*enginev1.LessonRef{{Title: "Lesson", Goal: "Learn"}}}}}, ExampleSlides: []*enginev1.Slide{{SlideId: "slide", Html: `<div id="root">Example</div>`, Narration: "Example", DurationMs: 1000}}, Attempts: []*enginev1.Usage{{Delivered: true}}}, nil
	}}
	h := New(Dependencies{Accounts: previewAuth{}, CourseBrief: previewBrief{}, Credits: f, Engine: e}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewUnstartedServer(h)
	server.Config.WriteTimeout = 100 * time.Millisecond
	server.Start()
	defer server.Close()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/v1/courses/preview", strings.NewReader(`{"brief_revision":"brief"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Quote-ID", "quote")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("preview response dropped by the write timeout: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "Private sample") {
		t.Fatalf("preview: %d %s", response.StatusCode, body)
	}
}
