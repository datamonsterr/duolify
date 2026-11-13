package social

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestOrderedPairIsCanonical(t *testing.T) {
	lo, hi := orderedPair("b", "a")
	lo2, hi2 := orderedPair("a", "b")
	if lo != "a" || hi != "b" || lo != lo2 || hi != hi2 {
		t.Fatalf("pair order: %s %s / %s %s", lo, hi, lo2, hi2)
	}
}

func TestFriendTokenIsOpaqueUniqueAndHashedStably(t *testing.T) {
	seen := map[string]bool{}
	for range 50 {
		token, hash, err := newFriendToken()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil || len(raw) != 32 || !validToken(token) {
			t.Fatalf("token %q: %v", token, err)
		}
		if hash != hashToken(token) || len(hash) != 64 || strings.Contains(hash, token) || seen[token] {
			t.Fatalf("hash=%q", hash)
		}
		seen[token] = true
	}
}

func TestValidTokenRejectsMalformedInputBeforeAnyLookup(t *testing.T) {
	good := strings.Repeat("A", 43)
	for _, bad := range []string{"", "short", good + "A", strings.Repeat("A", 42) + "=", strings.Repeat("A", 42) + "+", strings.Repeat("é", 43), good[:42] + " ", good[:42] + "\x00"} {
		if validToken(bad) {
			t.Errorf("validToken(%q)", bad)
		}
	}
	if !validToken(good) || !validToken(strings.Repeat("a-_", 14)+"a") {
		t.Fatal("valid token refused")
	}
}

func TestPolicyAllows(t *testing.T) {
	for _, c := range []struct {
		policy string
		link   bool
		common bool
		want   bool
	}{
		{"everyone", false, false, true},
		{"everyone", true, false, true},
		{"friends_of_friends", false, false, false},
		{"friends_of_friends", false, true, true},
		{"friends_of_friends", true, false, true},
		{"nobody", false, true, false},
		{"nobody", true, true, false},
		{"", false, false, false},
		{"surprise", true, true, false},
	} {
		if got := policyAllows(c.policy, c.link, c.common); got != c.want {
			t.Errorf("policyAllows(%q,link=%v,common=%v)=%v", c.policy, c.link, c.common, got)
		}
	}
}

func TestParseSendBodyNeedsExactlyOneTarget(t *testing.T) {
	for body, want := range map[string][2]string{
		`{"username":"Mina"}`:   {"Mina", ""},
		`{"token":"abc"}`:       {"", "abc"},
		`{"username":"@Mina "}`: {"Mina", ""},
	} {
		u, tok, err := parseSendBody(strings.NewReader(body))
		if err != nil || u != want[0] || tok != want[1] {
			t.Errorf("%s -> %q %q %v", body, u, tok, err)
		}
	}
	for _, body := range []string{``, `{}`, `{"username":""}`, `{"username":"a","token":"b"}`, `{"username":1}`, `{"id":"x"}`, `{"username":"a"} {}`, `[`, `{"username":"` + strings.Repeat("a", 5000) + `"}`} {
		if _, _, err := parseSendBody(strings.NewReader(body)); err == nil {
			t.Errorf("accepted %.30q", body)
		}
	}
}

func TestFriendRejectionsAreNeutralAndMapToStatus(t *testing.T) {
	// Every refusal that could reveal a recipient's state shares one message and status.
	a, b := (&Rejection{Code: CodeRequestUnavailable}), (&Rejection{Code: CodeRequestUnavailable})
	if a.Error() != b.Error() || a.Status() != 404 || a.Error() == "" {
		t.Fatalf("unavailable: %d %q", a.Status(), a.Error())
	}
	if (&Rejection{Code: CodeInviteUnavailable}).Status() != 404 {
		t.Fatal("invite status")
	}
	for _, code := range []string{CodePendingLimit, CodeFriendLimit, CodeNotPending} {
		if (&Rejection{Code: code}).Status() != 409 {
			t.Errorf("%s status", code)
		}
	}
	for _, r := range []*Rejection{{Code: CodeRequestUnavailable}, {Code: CodeInviteUnavailable}, {Code: CodePendingLimit}, {Code: CodeFriendLimit}} {
		msg := strings.ToLower(r.Error())
		for _, leak := range []string{"block", "private", "policy", "nobody", "declined", "cooldown", "exist"} {
			if strings.Contains(msg, leak) {
				t.Errorf("%s message %q mentions %q", r.Code, msg, leak)
			}
		}
	}
}

func TestFriendEventPropertiesAreEnumsOnly(t *testing.T) {
	if got := sentProps(true); len(got) != 1 || got["method"] != "invite_link" {
		t.Fatalf("link props: %v", got)
	}
	if got := sentProps(false); len(got) != 1 || got["method"] != "username" {
		t.Fatalf("username props: %v", got)
	}
}
