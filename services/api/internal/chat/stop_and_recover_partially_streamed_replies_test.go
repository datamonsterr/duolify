package chat

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
)

func TestClassifyEnd(t *testing.T) {
	canceled := context.Canceled
	for name, tc := range map[string]struct {
		streamErr, cause, reqErr error
		state, reason            string
		retryable                bool
	}{
		"clean eof":                      {nil, nil, nil, "completed", "", false},
		"late stop after clean eof":      {nil, errStopped, nil, "completed", "", false},
		"late disconnect after eof":      {nil, nil, canceled, "completed", "", false},
		"explicit stop":                  {canceled, errStopped, nil, "stopped", "stop", false},
		"stop wins over disconnect":      {canceled, errStopped, canceled, "stopped", "stop", false},
		"client disconnect":              {canceled, nil, canceled, "interrupted", "disconnect", true},
		"engine failure":                 {errors.New("provider died"), nil, nil, "interrupted", "engine_error", true},
		"typed idle":                     {&streamFailure{Code: "idle_timeout", Retryable: true}, nil, nil, "interrupted", "idle_timeout", true},
		"typed non retryable":            {&streamFailure{Code: "deadline_exceeded"}, nil, nil, "interrupted", "deadline_exceeded", false},
		"typed failure during stop":      {&streamFailure{Code: "idle_timeout", Retryable: true}, errStopped, nil, "stopped", "stop", false},
		"wrapped typed":                  {fmt.Errorf("recv: %w", &streamFailure{Code: "provider_error", Retryable: true}), nil, nil, "interrupted", "provider_error", true},
		"engine failure then disconnect": {errors.New("x"), nil, canceled, "interrupted", "disconnect", true},
	} {
		t.Run(name, func(t *testing.T) {
			got := classifyEnd(tc.streamErr, tc.cause, tc.reqErr)
			if got.State != tc.state || got.Reason != tc.reason || got.Retryable != tc.retryable {
				t.Fatalf("got %+v want state=%s reason=%s retryable=%v", got, tc.state, tc.reason, tc.retryable)
			}
		})
	}
}

func TestFlightsAreOwnerScopedAndStopIsIdempotent(t *testing.T) {
	var f flights
	var cause error
	ctx, cancel := context.WithCancelCause(context.Background())
	owned := f.add("owner", "key", cancel)
	if f.add("owner", "key", cancel) != nil {
		t.Fatal("a live key cannot be registered twice")
	}
	if f.stop("intruder", "key") != nil || ctx.Err() != nil {
		t.Fatal("another learner must not reach the stream")
	}
	if f.stop("owner", "other-key") != nil {
		t.Fatal("unknown key")
	}
	for range 3 {
		if got := f.stop("owner", "key"); got != owned {
			t.Fatal("owner stop must find the flight every time while it lives")
		}
	}
	cause = context.Cause(ctx)
	if !errors.Is(cause, errStopped) {
		t.Fatalf("cause %v", cause)
	}
	f.finish("owner", "key", owned)
	if f.stop("owner", "key") != nil {
		t.Fatal("finished flights are gone: stop is then answered from storage")
	}
	select {
	case <-owned.done:
	default:
		t.Fatal("finish must release waiters")
	}
	f.finish("owner", "key", owned) // idempotent, no panic on double close
}

func TestFlightsConcurrentStopFinishRace(_ *testing.T) {
	var f flights
	for i := range 50 {
		key := fmt.Sprint("k", i)
		_, cancel := context.WithCancelCause(context.Background())
		fl := f.add("o", key, cancel)
		var wg sync.WaitGroup
		for range 4 {
			wg.Add(1)
			go func() { defer wg.Done(); _ = f.stop("o", key) }()
		}
		wg.Add(1)
		go func() { defer wg.Done(); f.finish("o", key, fl) }()
		wg.Wait()
	}
}

func TestPartialUsageIsFlaggedAndBoundedByTheQuote(t *testing.T) {
	q := pricing.Quote{Bounds: map[string]int64{"input_tokens": 100, "output_tokens": 10, "timeout_ms": 1000}}
	q.Rate.Provider, q.Rate.Model = "p", "m"
	huge := string(make([]byte, 10_000))
	got := settlementUsage(q, 100_000, huge, nil)[0]
	if !got.Estimated || got.InputTokens != 100 || got.OutputTokens != 10 || !got.Delivered || got.Provider != "p" {
		t.Fatalf("api estimate must be flagged and clamped: %+v", got)
	}
	engine := &enginev1.Usage{Provider: "p2", Model: "m2", InputTokens: 50, OutputTokens: 4, Estimated: true}
	got = settlementUsage(q, 400, "abc", engine)[0]
	if !got.Estimated || got.Provider != "p2" || !got.FallbackUsed || got.OutputTokens != 4 {
		t.Fatalf("engine estimate keeps the actual route: %+v", got)
	}
	got = settlementUsage(q, 400, "abc", &enginev1.Usage{Provider: "p", Model: "m", InputTokens: 7, OutputTokens: 3})[0]
	if got.Estimated || got.FallbackUsed || got.InputTokens != 7 {
		t.Fatalf("reported usage is not an estimate: %+v", got)
	}
}

func TestEngineErrorEventsBecomeTypedFailures(t *testing.T) {
	err := failureOf(&enginev1.ChatStreamError{Code: "first_token_timeout", Retryable: true})
	var sf *streamFailure
	if !errors.As(err, &sf) || sf.Code != "first_token_timeout" || !sf.Retryable {
		t.Fatalf("%v", err)
	}
	if got := failureOf(&enginev1.ChatStreamError{}).(*streamFailure).Code; got != "provider_error" {
		t.Fatalf("empty code defaults to provider_error: %q", got)
	}
}
