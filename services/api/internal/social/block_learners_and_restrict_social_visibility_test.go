package social

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestNotBlockedSQLCoversBothDirectionsAndRejectsUnsafeFragments(t *testing.T) {
	sql := NotBlockedSQL("$1::uuid", "u.learner_id")
	for _, want := range []string{"NOT EXISTS", "learner_blocks", "blocker_id=$1::uuid", "blocked_id=u.learner_id", "blocker_id=u.learner_id", "blocked_id=$1::uuid"} {
		if !strings.Contains(sql, want) {
			t.Errorf("fragment lacks %q: %s", want, sql)
		}
	}
	if !strings.HasPrefix(sql, " AND ") {
		t.Errorf("fragment must append to a WHERE: %q", sql)
	}
	// The search and profile seam is built from the same fragment: one definition.
	if blockedPairClause != sql {
		t.Errorf("blockedPairClause diverged from NotBlockedSQL:\n%s", blockedPairClause)
	}
	for _, bad := range [][2]string{{"1; DROP TABLE learners", "u.id"}, {"$1", "u.id OR true"}, {"", "u.id"}, {"$1", ""}, {"$1", "u.id)--"}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("NotBlockedSQL(%q,%q) accepted an unsafe fragment", bad[0], bad[1])
				}
			}()
			NotBlockedSQL(bad[0], bad[1])
		}()
	}
}

func TestOnBlockCallbacksRunInOrderAndStopOnFirstError(t *testing.T) {
	s := &Service{}
	var seen []string
	s.OnBlock(func(_ context.Context, _ pgx.Tx, blocker, blocked string) error {
		seen = append(seen, "one:"+blocker+">"+blocked)
		return nil
	})
	boom := errors.New("invite store down")
	s.OnBlock(func(context.Context, pgx.Tx, string, string) error { seen = append(seen, "two"); return boom })
	s.OnBlock(func(context.Context, pgx.Tx, string, string) error { seen = append(seen, "three"); return nil })
	if err := s.runOnBlock(t.Context(), nil, "a", "b"); !errors.Is(err, boom) {
		t.Fatalf("err=%v", err)
	}
	if want := []string{"one:a>b", "two"}; !slices.Equal(seen, want) {
		t.Fatalf("callbacks ran %v, want %v", seen, want)
	}
	s.OnBlock(nil) // a nil callback is ignored, never a panic at block time
	if err := (&Service{}).runOnBlock(t.Context(), nil, "a", "b"); err != nil {
		t.Fatalf("no callbacks: %v", err)
	}
}

func TestParseBlockBodyIsStrict(t *testing.T) {
	for in, want := range map[string]string{
		`{"username":"Kai.Two"}`:     "Kai.Two",
		`{"username":"  @Kai.Two "}`: "Kai.Two",
	} {
		got, err := ParseBlockBody(strings.NewReader(in))
		if err != nil || got != want {
			t.Errorf("ParseBlockBody(%s) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{``, `{}`, `{"username":""}`, `{"username":"  "}`, `{"username":"a","x":1}`, `{"username":"a"} {}`, `[]`, `{"username":7}`, `{"username":"` + strings.Repeat("a", 3000) + `"}`} {
		if got, err := ParseBlockBody(strings.NewReader(in)); err == nil {
			t.Errorf("ParseBlockBody(%.30s) accepted %q", in, got)
		}
	}
}

func TestBlockRejectionsAreNeutralAndMapped(t *testing.T) {
	un, lim := &Rejection{Code: CodeBlockUnavailable}, &Rejection{Code: CodeBlockLimit}
	if un.Status() != 404 || lim.Status() != 409 {
		t.Fatalf("status %d %d", un.Status(), lim.Status())
	}
	if !strings.Contains(strings.ToLower(un.Error()), "couldn't") || strings.Contains(strings.ToLower(un.Error()), "blocked") {
		t.Fatalf("unavailable copy: %q", un.Error())
	}
	if lim.Error() == un.Error() || lim.Error() == "" {
		t.Fatalf("limit copy: %q", lim.Error())
	}
}

func TestInviteAllowsBoundaries(t *testing.T) {
	for _, c := range []struct {
		setting string
		friends bool
		want    bool
	}{
		{"friends", true, true}, {"friends", false, false},
		{"everyone", false, true}, {"everyone", true, true},
		{"nobody", true, false}, {"nobody", false, false},
		{"", true, false}, {"unknown", true, false}, // unknown values fail closed
	} {
		if got := inviteAllows(c.setting, c.friends); got != c.want {
			t.Errorf("inviteAllows(%q,%v)=%v want %v", c.setting, c.friends, got, c.want)
		}
	}
}

func TestSettingsPatchAcceptsOnlyKnownInviteValues(t *testing.T) {
	for _, v := range []string{"friends", "everyone", "nobody"} {
		if p, err := ParseSettingsPatch(strings.NewReader(`{"course_invites_from":"` + v + `"}`)); err != nil || p.CourseInvitesFrom == nil || *p.CourseInvitesFrom != v {
			t.Errorf("%s: %v %v", v, p, err)
		}
	}
	for _, v := range []string{`""`, `"friends_of_friends"`, `"Friends"`, `true`, `null`} {
		if _, err := ParseSettingsPatch(strings.NewReader(`{"course_invites_from":` + v + `}`)); err == nil {
			t.Errorf("accepted course_invites_from=%s", v)
		}
	}
	if defaultSettings.CourseInvitesFrom != "friends" {
		t.Fatalf("default=%q", defaultSettings.CourseInvitesFrom)
	}
}

func TestProjectNeverLeaksStreakToNonFriendsOrWhenOff(t *testing.T) {
	src := fakeStreakSource{"L1": 12}
	row := identityRow{LearnerID: "L1", Username: "Kai", DisplayName: "Kai", ShowStreakToFriends: true}
	if got := row.Project(src, true); got.Streak == nil || got.Streak.Days != 12 {
		t.Fatalf("friend: %+v", got)
	}
	if got := row.Project(src, false); got.Streak != nil {
		t.Fatalf("non-friend sees %+v", got.Streak)
	}
	row.ShowStreakToFriends = false
	if got := row.Project(src, true); got.Streak != nil {
		t.Fatalf("setting off but %+v", got.Streak)
	}
}

type fakeStreakSource map[string]int

func (f fakeStreakSource) Streak(id string) (int, bool) { d, ok := f[id]; return d, ok }
