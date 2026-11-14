package sharing

import (
	"context"
	"errors"
	"sync"
)

// PushMessage is everything a push adapter may carry: identifiers and an enum. It never holds
// Course material, a title, a Username or any other learner's text; the app fetches the invite
// itself after it opens.
type PushMessage struct {
	RecipientID string
	InviteID    string
	Kind        string
}

// PushKindCourseInvite is the only kind in v1.
const PushKindCourseInvite = "course_invite"

var (
	// ErrPushDisabled means no adapter is configured; the in-app inbox is the whole delivery.
	ErrPushDisabled = errors.New("push_disabled")
	// ErrPushDenied means the device or learner refused notifications; it is final, not retried.
	ErrPushDenied = errors.New("push_denied")
)

// PushSender is the only way an invite reaches APNs/FCM. Any error other than ErrPushDisabled and
// ErrPushDenied is retried a bounded number of times. The adapter resolves devices itself.
type PushSender interface {
	Send(ctx context.Context, msg PushMessage) error
}

// DisabledPush is the production default until E15-12 provides APNs/FCM credentials (ADR 0012).
type DisabledPush struct{}

// Send always reports ErrPushDisabled.
func (DisabledPush) Send(context.Context, PushMessage) error { return ErrPushDisabled }

// FakePush records messages for tests and local development. It is safe for concurrent use.
type FakePush struct {
	mu   sync.Mutex
	sent []PushMessage
	fail error
}

// NewFakePush returns an empty, succeeding fake.
func NewFakePush() *FakePush { return &FakePush{} }

// Send records the message unless a failure is set.
func (f *FakePush) Send(_ context.Context, msg PushMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.sent = append(f.sent, msg)
	return nil
}

// Fail makes every later Send return err; nil restores success.
func (f *FakePush) Fail(err error) { f.mu.Lock(); f.fail = err; f.mu.Unlock() }

// Sent returns a copy of the recorded messages in send order.
func (f *FakePush) Sent() []PushMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]PushMessage(nil), f.sent...)
}

// PushFromEnv chooses the adapter and returns its name. The default is disabled. "fake" is for
// dev and tests and is refused in production. APNs/FCM secrets (PUSH_APNS_*, PUSH_FCM_*, documented
// in .env.example) come from the secret store only; no live transport is built before E15-12, so
// their presence alone never enables push.
func PushFromEnv(getenv func(string) string) (PushSender, string) {
	if getenv("PUSH_ADAPTER") == "fake" && getenv("DUOLIFY_ENV") != "production" {
		return NewFakePush(), "fake"
	}
	return DisabledPush{}, "disabled"
}
