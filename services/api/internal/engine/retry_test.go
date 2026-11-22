package engine

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type flakyEngine struct {
	enginev1.UnimplementedEngineServiceServer
	calls     atomic.Int32
	unavailOn int32
	code      codes.Code
}

func (f *flakyEngine) PreviewCourse(context.Context, *enginev1.PreviewCourseRequest) (*enginev1.PreviewCourseResponse, error) {
	if f.calls.Add(1) <= f.unavailOn {
		return nil, status.Error(f.code, "restarting")
	}
	return &enginev1.PreviewCourseResponse{}, nil
}

func serve(t *testing.T, addr string, svc enginev1.EngineServiceServer) *grpc.Server {
	t.Helper()
	l, err := new(net.ListenConfig).Listen(context.Background(), "tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	s := grpc.NewServer()
	enginev1.RegisterEngineServiceServer(s, svc)
	go func() { _ = s.Serve(l) }()
	t.Cleanup(s.Stop)
	return s
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().String()
}

func TestUnavailableIsRetriedAndOtherCodesAreNot(t *testing.T) {
	for _, tc := range []struct {
		code      codes.Code
		wantCalls int32
		wantOK    bool
	}{{codes.Unavailable, 3, true}, {codes.Internal, 1, false}} {
		f := &flakyEngine{unavailOn: 2, code: tc.code}
		addr := freeAddr(t)
		serve(t, addr, f)
		c, err := New("http://"+addr, 30*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.PreviewCourse(context.Background(), &enginev1.PreviewCourseRequest{})
		if (err == nil) != tc.wantOK || f.calls.Load() != tc.wantCalls {
			t.Fatalf("%v: err=%v calls=%d, want ok=%v calls=%d", tc.code, err, f.calls.Load(), tc.wantOK, tc.wantCalls)
		}
		_ = c.Close()
	}
}

// The engine is down when the call starts (deploy replacing it) and back within the retry window.
func TestCallSurvivesEngineRestart(t *testing.T) {
	addr := freeAddr(t)
	c, err := New("http://"+addr, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	go func() { time.Sleep(1500 * time.Millisecond); serve(t, addr, &flakyEngine{}) }()
	if _, err = c.PreviewCourse(context.Background(), &enginev1.PreviewCourseRequest{}); err != nil {
		t.Fatalf("call during engine restart failed: %v", err)
	}
}
