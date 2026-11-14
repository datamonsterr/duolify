package sharing_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
)

func TestParseInviteBodyAcceptsExactlyOneTarget(t *testing.T) {
	const fid = "d4444444-4444-4444-8444-444444444444"
	cases := []struct {
		name, body string
		want       sharing.InviteInput
		ok         bool
	}{
		{"username", `{"username":"ada"}`, sharing.InviteInput{Username: "ada"}, true},
		{"strips at and space", `{"username":"  @Ada "}`, sharing.InviteInput{Username: "Ada"}, true},
		{"friend", `{"friendship_id":"` + fid + `"}`, sharing.InviteInput{FriendshipID: fid}, true},
		{"key", `{"username":"ada","operation_key":"` + fid + `"}`, sharing.InviteInput{Username: "ada", Key: fid}, true},
		{"both", `{"username":"ada","friendship_id":"` + fid + `"}`, sharing.InviteInput{}, false},
		{"neither", `{}`, sharing.InviteInput{}, false},
		{"blank username", `{"username":"   "}`, sharing.InviteInput{}, false},
		{"bad friendship id", `{"friendship_id":"nope"}`, sharing.InviteInput{}, false},
		{"bad key", `{"username":"ada","operation_key":"x"}`, sharing.InviteInput{}, false},
		{"unknown field", `{"username":"ada","course":"x"}`, sharing.InviteInput{}, false},
		{"trailing", `{"username":"ada"}{}`, sharing.InviteInput{}, false},
		{"too long", `{"username":"` + strings.Repeat("a", 4000) + `"}`, sharing.InviteInput{}, false},
		{"not json", `ada`, sharing.InviteInput{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sharing.ParseInviteBody(strings.NewReader(tc.body))
			if (err == nil) != tc.ok || (tc.ok && got != tc.want) {
				t.Fatalf("got %+v err=%v", got, err)
			}
		})
	}
}

func TestInviteMethodIsDerivedFromTheTarget(t *testing.T) {
	if (sharing.InviteInput{Username: "a"}).Method() != "username" || (sharing.InviteInput{FriendshipID: "x"}).Method() != "friend" {
		t.Fatal("method must be username or friend")
	}
}

func TestInviteExpiryBoundary(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	if sharing.InviteExpired(now, now.Add(time.Second)) || !sharing.InviteExpired(now, now) || !sharing.InviteExpired(now, now.Add(-time.Hour)) {
		t.Fatal("an invite is expired at and after expires_at")
	}
}

func TestInviteConfigDefaultsAndEnv(t *testing.T) {
	d := sharing.DefaultConfig()
	if d.InviteSendsPerDay != 20 || d.InviteExpiryDays != 30 || d.InvitePurgeDays != 90 || d.PushMaxAttempts != 3 {
		t.Fatalf("defaults %+v", d)
	}
	env := map[string]string{"SHARING_INVITES_PER_DAY": "5", "SHARING_INVITE_EXPIRY_DAYS": "7", "SHARING_INVITE_PURGE_DAYS": "30", "SHARING_PUSH_MAX_ATTEMPTS": "2"}
	c := sharing.ConfigFromEnv(func(k string) string { return env[k] })
	if c.InviteSendsPerDay != 5 || c.InviteExpiryDays != 7 || c.InvitePurgeDays != 30 || c.PushMaxAttempts != 2 {
		t.Fatalf("env %+v", c)
	}
	bad := map[string]string{"SHARING_INVITES_PER_DAY": "0", "SHARING_INVITE_EXPIRY_DAYS": "-1", "SHARING_INVITE_PURGE_DAYS": "x", "SHARING_PUSH_MAX_ATTEMPTS": "99"}
	c = sharing.ConfigFromEnv(func(k string) string { return bad[k] })
	if c.InviteSendsPerDay != 20 || c.InviteExpiryDays != 30 || c.InvitePurgeDays != 90 || c.PushMaxAttempts != 3 {
		t.Fatalf("invalid env must keep defaults: %+v", c)
	}
}

func TestPushAdapterSelectionNeedsExplicitOptInAndNeverRunsFakeInProduction(t *testing.T) {
	pick := func(env map[string]string) string {
		p, name := sharing.PushFromEnv(func(k string) string { return env[k] })
		if p == nil {
			t.Fatal("an adapter is always returned")
		}
		return name
	}
	if got := pick(nil); got != "disabled" {
		t.Fatalf("default %q", got)
	}
	if got := pick(map[string]string{"PUSH_ADAPTER": "fake"}); got != "fake" {
		t.Fatalf("fake in dev %q", got)
	}
	if got := pick(map[string]string{"PUSH_ADAPTER": "fake", "DUOLIFY_ENV": "production"}); got != "disabled" {
		t.Fatalf("fake in production %q", got)
	}
	// Secrets alone never enable anything: no live transport exists before E15-12.
	if got := pick(map[string]string{"PUSH_APNS_KEY_ID": "k", "PUSH_FCM_SERVICE_ACCOUNT_JSON": "{}"}); got != "disabled" {
		t.Fatalf("secrets %q", got)
	}
	if err := (sharing.DisabledPush{}).Send(context.Background(), sharing.PushMessage{}); !errors.Is(err, sharing.ErrPushDisabled) {
		t.Fatalf("disabled adapter: %v", err)
	}
}

func TestFakePushRecordsInOrderAndCanFail(t *testing.T) {
	p := sharing.NewFakePush()
	for _, id := range []string{"a", "b", "c"} {
		if err := p.Send(context.Background(), sharing.PushMessage{RecipientID: "r", InviteID: id, Kind: "course_invite"}); err != nil {
			t.Fatal(err)
		}
	}
	got := p.Sent()
	if len(got) != 3 || got[0].InviteID != "a" || got[2].InviteID != "c" {
		t.Fatalf("sent %+v", got)
	}
	p.Fail(sharing.ErrPushDenied)
	if err := p.Send(context.Background(), sharing.PushMessage{}); !errors.Is(err, sharing.ErrPushDenied) || len(p.Sent()) != 3 {
		t.Fatalf("failing send recorded or passed: %v", err)
	}
	p.Fail(nil)
	if err := p.Send(context.Background(), sharing.PushMessage{InviteID: "d"}); err != nil || len(p.Sent()) != 4 {
		t.Fatal("clearing the failure must resume sends")
	}
}

type stubInvites struct {
	err    error
	gotIn  sharing.InviteInput
	actor  string
	course string
}

func (s *stubInvites) SendInvite(_ context.Context, actor, course string, in sharing.InviteInput) (sharing.Invite, error) {
	s.actor, s.course, s.gotIn = actor, course, in
	return sharing.Invite{ID: "i1", Status: "pending", ExpiresAt: time.Date(2026, 12, 15, 0, 0, 0, 0, time.UTC)}, s.err
}
func (s *stubInvites) SentInvites(_ context.Context, actor, _ string) ([]sharing.SentInvite, error) {
	s.actor = actor
	return []sharing.SentInvite{{ID: "i1", Status: "pending", Method: "username"}}, s.err
}
func (s *stubInvites) ReceivedInvites(_ context.Context, actor string) ([]sharing.ReceivedInvite, int, error) {
	s.actor = actor
	return []sharing.ReceivedInvite{{ID: "i1", Course: sharing.InviteCourse{Title: "T"}}}, 1, s.err
}
func (s *stubInvites) MarkInvitesRead(_ context.Context, actor string) error {
	s.actor = actor
	return s.err
}
func (s *stubInvites) AcceptInvite(_ context.Context, actor, _ string) (sharing.InviteOutcome, error) {
	s.actor = actor
	return sharing.InviteOutcome{Status: "accepted", CourseID: "copy"}, s.err
}
func (s *stubInvites) DeclineInvite(_ context.Context, actor, _ string) (sharing.InviteOutcome, error) {
	s.actor = actor
	return sharing.InviteOutcome{Status: "declined"}, s.err
}
func (s *stubInvites) RevokeInvite(_ context.Context, actor, _ string) (sharing.InviteOutcome, error) {
	s.actor = actor
	return sharing.InviteOutcome{Status: "revoked"}, s.err
}

func inviteMux(h http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	for _, p := range sharing.InvitePatterns {
		mux.Handle(p, h)
	}
	return mux
}

func TestInviteHandlerUsesTheSessionActorAndMapsErrorsNeutrally(t *testing.T) {
	const actor = "a1111111-1111-4111-8111-111111111111"
	do := func(h http.Handler, method, path, body string, signedIn bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if signedIn {
			req = req.WithContext(accounts.WithLearnerID(req.Context(), actor))
		}
		rec := httptest.NewRecorder()
		inviteMux(h).ServeHTTP(rec, req)
		return rec
	}
	stub := &stubInvites{}
	h := sharing.NewInviteHandler(stub)

	rec := do(h, "POST", "/v1/courses/c1/invites", `{"username":"ada"}`, true)
	if rec.Code != 201 || stub.actor != actor || stub.course != "c1" || stub.gotIn.Username != "ada" {
		t.Fatalf("send: %d %s actor=%s", rec.Code, rec.Body, stub.actor)
	}
	var sent map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &sent)
	if sent["id"] != "i1" || sent["status"] != "pending" || sent["expires_at"] == nil {
		t.Fatalf("body %v", sent)
	}
	if rec := do(h, "POST", "/v1/courses/c1/invites", `{"actor":"x","username":"ada"}`, true); rec.Code != 400 {
		t.Fatalf("a body naming an actor must be rejected: %d", rec.Code)
	}
	if rec := do(h, "POST", "/v1/courses/c1/invites", `{"username":"ada"}`, false); rec.Code != 401 {
		t.Fatalf("signed out: %d", rec.Code)
	}
	if rec := do(h, "GET", "/v1/invites/received", "", true); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"unread":1`) {
		t.Fatalf("received %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/v1/courses/c1/invites", "", true); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"invites"`) {
		t.Fatalf("sent %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", "/v1/invites/read", "", true); rec.Code != 204 {
		t.Fatalf("read %d", rec.Code)
	}
	for path, want := range map[string]string{"/v1/invites/i1/accept": `"course_id":"copy"`, "/v1/invites/i1/decline": `"declined"`, "/v1/invites/i1/revoke": `"revoked"`} {
		if rec := do(h, "POST", path, "", true); rec.Code != 200 || !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}

	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{sharing.ErrInviteUnavailable, 404, "invite_unavailable"},
		{sharing.ErrCourseUnavailable, 404, "course_unavailable"},
		{sharing.ErrCourseNotShared, 409, "course_not_shared"},
		{sharing.ErrInviteDuplicate, 409, "invite_duplicate"},
		{sharing.ErrInviteRateLimited, 429, "invite_rate_limited"},
		{errors.New("pq: relation course_invites secret"), 503, "sharing_unavailable"},
	} {
		stub.err = tc.err
		rec := do(h, "POST", "/v1/invites/i1/accept", "", true)
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.code) || strings.Contains(rec.Body.String(), "secret") {
			t.Errorf("%v -> %d %s", tc.err, rec.Code, rec.Body)
		}
	}
}
