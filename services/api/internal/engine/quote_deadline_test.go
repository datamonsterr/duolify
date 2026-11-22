package engine

import (
	"context"
	"net"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"google.golang.org/grpc"
)

type slowEngine struct {
	enginev1.UnimplementedEngineServiceServer
	delay time.Duration
}

func (s slowEngine) PreviewCourse(ctx context.Context, _ *enginev1.PreviewCourseRequest) (*enginev1.PreviewCourseResponse, error) {
	select {
	case <-time.After(s.delay):
		return &enginev1.PreviewCourseResponse{}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s slowEngine) GenerateCourseOutline(ctx context.Context, _ *enginev1.GenerateCourseOutlineRequest) (*enginev1.GenerateCourseOutlineResponse, error) {
	select {
	case <-time.After(s.delay):
		return &enginev1.GenerateCourseOutlineResponse{}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s slowEngine) GenerateLesson(_ *enginev1.GenerateLessonRequest, stream grpc.ServerStreamingServer[enginev1.GenerateLessonEvent]) error {
	select {
	case <-time.After(s.delay):
		return stream.Send(&enginev1.GenerateLessonEvent{Event: &enginev1.GenerateLessonEvent_Lesson{Lesson: &enginev1.Lesson{}}})
	case <-stream.Context().Done():
		return stream.Context().Err()
	}
}

// Quoted generation runs inside the caller's quote/reservation deadline (minutes for a live
// model). The client's short default timeout must not cut it to seconds: a live preview took
// longer than 10 s and every preview answered 504.
func TestQuotedCallsKeepTheCallersDeadline(t *testing.T) {
	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	enginev1.RegisterEngineServiceServer(server, slowEngine{delay: 300 * time.Millisecond})
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()

	client, err := New("http://"+listener.Addr().String(), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	quoted, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err = client.PreviewCourse(quoted, &enginev1.PreviewCourseRequest{}); err != nil {
		t.Fatalf("preview within the quote deadline: %v", err)
	}
	if _, err = client.GenerateCourseOutline(quoted, &enginev1.GenerateCourseOutlineRequest{}); err != nil {
		t.Fatalf("outline within the quote deadline: %v", err)
	}
	if _, _, err = client.GenerateLesson(quoted, &enginev1.GenerateLessonRequest{}); err != nil {
		t.Fatalf("lesson within the quote deadline: %v", err)
	}
	// Without a caller deadline the default still bounds the call.
	if _, err = client.PreviewCourse(t.Context(), &enginev1.PreviewCourseRequest{}); err == nil {
		t.Fatal("an unbounded caller must get the default timeout")
	}
}
