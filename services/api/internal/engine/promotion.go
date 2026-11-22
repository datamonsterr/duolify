package engine

import (
	"context"
	"errors"
	"io"
	"log/slog"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// PromotionDomain is the ErrorInfo domain the engine sets when a live AI feature has no passing
// E10-06 promotion evidence (engine_server::ERROR_DOMAIN).
const PromotionDomain = "engine.duolify"

// NotPromoted reports the engine's typed refusal of a live model call: an UNAVAILABLE status
// carrying a google.rpc.ErrorInfo in PromotionDomain. reason is the stable code, such as
// promotion_evidence_missing or promotion_evidence_stale, and task is the Golden set task.
// Callers keep their existing fail-closed, retryable "unavailable" handling.
func NotPromoted(err error) (reason, task string, ok bool) {
	st, isStatus := status.FromError(err)
	if !isStatus || st.Code() != codes.Unavailable {
		return "", "", false
	}
	for _, d := range st.Details() {
		if info, isInfo := d.(*errdetails.ErrorInfo); isInfo && info.GetDomain() == PromotionDomain {
			return info.GetReason(), info.GetMetadata()["task"], true
		}
	}
	return "", "", false
}

// logNotPromoted makes a refused live feature alertable: one error line per refused call.
func logNotPromoted(ctx context.Context, method string, err error) {
	if reason, task, ok := NotPromoted(err); ok {
		slog.ErrorContext(ctx, "engine refused a live AI feature without promotion evidence",
			"event", "live_feature_not_promoted", "method", method, "reason", reason, "task", task)
	}
}

// watchStream logs a typed refusal that a server-streaming RPC returns on its first receive.
func watchStream(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	s, err := streamer(ctx, desc, cc, method, opts...)
	if err != nil {
		logNotPromoted(ctx, method, err)
		return nil, err
	}
	return &watchedStream{ClientStream: s, ctx: ctx, method: method}, nil
}

type watchedStream struct {
	grpc.ClientStream
	ctx    context.Context
	method string
}

func (s *watchedStream) RecvMsg(m any) error {
	err := s.ClientStream.RecvMsg(m)
	if err != nil && !errors.Is(err, io.EOF) {
		logNotPromoted(s.ctx, s.method, err)
	}
	return err
}
