package social

import (
	"strings"
	"testing"
)

func TestCanonicalNFKCLowercase(t *testing.T) {
	for in, want := range map[string]string{"Maya": "maya", "Ｍaya": "maya", "MAYA.Learns": "maya.learns", "ﬁsh": "fish", "K": "k"} {
		if got := Canonical(in); got != want {
			t.Errorf("Canonical(%q)=%q want %q", in, got, want)
		}
	}
}

func TestValidateBoundaries(t *testing.T) {
	ok := []string{"abc", "Abc", "a_b", "a.b", "a1_2.3", strings.Repeat("a", 20), "Ｍaya"}
	bad := []string{"", "ab", strings.Repeat("a", 21), "1abc", "_abc", ".abc", "abc_", "abc.", "a__b", "a..b", "a._b", "a b", "a-b", "ünï", "ab\u200bc", "a%c", "🙂abc"}
	for _, name := range ok {
		if err := Validate(name); err != nil {
			t.Errorf("Validate(%q)=%v", name, err)
		}
	}
	for _, name := range bad {
		if err := Validate(name); err == nil || err.Code != CodeInvalid {
			t.Errorf("Validate(%q) accepted or wrong code: %v", name, err)
		}
	}
}

func TestListsAreConfigurationAndFoldConfusables(t *testing.T) {
	cfg := Config{Reserved: []string{"admin", "Support", "duolify"}, Profanity: []string{"heck"}}
	for name, want := range map[string]string{
		"Admin": CodeReserved, "SUPPORT": CodeReserved, "supp0rt": CodeReserved, "du0lify": CodeReserved, "d.u_olify": CodeReserved,
		"Oh_h3ck": CodeNotAllowed, "HECK99": CodeNotAllowed,
		"maya": "", "admin2": "", "Teacher": "",
	} {
		err := cfg.Check(name)
		got := ""
		if err != nil {
			got = err.Code
		}
		if got != want {
			t.Errorf("Check(%q)=%q want %q", name, got, want)
		}
	}
	if err := (Config{}).Check("support"); err != nil {
		t.Fatal("empty config must not hard-code a list")
	}
}

func TestRejectionIsNeutral(t *testing.T) {
	msgs := map[string]bool{}
	for _, code := range []string{CodeTaken, CodeReserved, CodeNotAllowed} {
		msgs[(&Rejection{Code: code}).Error()] = true
	}
	if len(msgs) != 1 {
		t.Fatalf("rejection copy distinguishes reasons: %v", msgs)
	}
}

type fakeStreak struct{ days int }

func (f fakeStreak) Streak(string) (int, bool) { return f.days, f.days > 0 }

func TestProjectionExposesOnlyAllowedFields(t *testing.T) {
	row := identityRow{LearnerID: "x", Username: "Maya", DisplayName: "Learner", ShowStreakToFriends: true}
	p := row.Project(fakeStreak{days: 4}, false)
	if p.Streak != nil || p.Username != "Maya" || p.Avatar.Initial != "M" {
		t.Fatalf("stranger projection: %+v", p)
	}
	if p := row.Project(fakeStreak{days: 4}, true); p.Streak == nil || p.Streak.Days != 4 {
		t.Fatalf("friend projection: %+v", p)
	}
	row.ShowStreakToFriends = false
	if p := row.Project(fakeStreak{days: 4}, true); p.Streak != nil {
		t.Fatal("streak shown against privacy setting")
	}
	if p := row.Project(nil, true); p.Streak != nil {
		t.Fatal("streak without source")
	}
}
