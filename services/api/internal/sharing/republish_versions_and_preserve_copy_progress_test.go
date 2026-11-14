package sharing_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
)

func TestLessonKeyIsDeterministicStableAndDistinct(t *testing.T) {
	a, b := "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
	hex32 := regexp.MustCompile(`^[0-9a-f]{32}$`)
	if !hex32.MatchString(sharing.LessonKey(a)) {
		t.Fatalf("key %q is not 32 hex", sharing.LessonKey(a))
	}
	if sharing.LessonKey(a) != sharing.LessonKey(a) || sharing.LessonKey(a) == sharing.LessonKey(b) {
		t.Fatal("key must be deterministic per stub and differ between stubs")
	}
	if got := sharing.KeyOrLegacy(sharing.SnapshotLesson{Key: "abc"}, 1, 2); got != "abc" {
		t.Fatalf("explicit key lost: %q", got)
	}
	if got := sharing.KeyOrLegacy(sharing.SnapshotLesson{}, 1, 2); got != "legacy-1-2" {
		t.Fatalf("legacy key %q", got)
	}
}

func keyed(key, title string) sharing.SnapshotLesson {
	l := readyLesson(title)
	l.Key = key
	return l
}

func snapOf(units ...[]sharing.SnapshotLesson) sharing.Snapshot {
	s := sharing.Snapshot{Title: "T"}
	for _, u := range units {
		s.Units = append(s.Units, sharing.SnapshotUnit{Title: "U", Lessons: u})
	}
	return s
}

func TestDiffSnapshotsCountsByKeyNotTitle(t *testing.T) {
	oldSnap := snapOf([]sharing.SnapshotLesson{keyed("a", "Phases"), keyed("b", "Tides"), keyed("c", "Eclipses")})
	newSnap := snapOf([]sharing.SnapshotLesson{keyed("b", "Ocean tides"), keyed("a", "Phases"), keyed("d", "Comets")})
	got := sharing.DiffSnapshots(oldSnap, newSnap)
	if got != (sharing.ChangeSummary{Added: 1, Removed: 1, Retitled: 1}) {
		t.Fatalf("diff %+v", got)
	}
	if same := sharing.DiffSnapshots(oldSnap, oldSnap); same != (sharing.ChangeSummary{}) {
		t.Fatalf("identical snapshots differ: %+v", same)
	}
}

func TestCleanChangelogBounds(t *testing.T) {
	if got, err := sharing.CleanChangelog("  Rewrote lesson 2.\x00 ", 500); err != nil || got != "Rewrote lesson 2." {
		t.Fatalf("clean: %q %v", got, err)
	}
	if got, err := sharing.CleanChangelog("", 500); err != nil || got != "" {
		t.Fatalf("empty changelog is allowed: %q %v", got, err)
	}
	if _, err := sharing.CleanChangelog(strings.Repeat("é", 500), 500); err != nil {
		t.Fatalf("500 runes must fit: %v", err)
	}
	if _, err := sharing.CleanChangelog(strings.Repeat("é", 501), 500); !errors.Is(err, sharing.ErrChangelogTooLong) {
		t.Fatalf("501 runes: %v", err)
	}
}

func held(stub, key, title, status string, reused, content, test bool) sharing.HeldLesson {
	return sharing.HeldLesson{StubID: stub, Key: key, Title: title, Status: status, Reused: reused, HasContent: content, HasTest: test}
}

func planRow(p sharing.UpdatePlan) []string {
	var out []string
	for _, u := range p.Units {
		for _, l := range u.Lessons {
			out = append(out, l.Key+":"+l.Title+":"+l.Action+":"+l.Status+":"+l.StubID)
		}
	}
	return out
}

func TestPlanUpdateMapsByKeyKeepsProgressAndArchivesRemoved(t *testing.T) {
	have := []sharing.HeldLesson{
		held("s1", "a", "Phases", "done", true, true, true),
		held("s2", "b", "Tides", "done", true, true, true),
		held("s3", "c", "Eclipses", "locked", false, false, false),
	}
	next := snapOf([]sharing.SnapshotLesson{keyed("a", "Phases"), keyed("b", "Ocean tides"), {Key: "d", Title: "Comets", Goal: "g"}})
	plan := sharing.PlanUpdate(have, next, "https://assets.example.test")
	want := []string{
		"a:Phases:keep:done:s1",
		"b:Ocean tides:keep:done:s2",
		"d:Comets:add:ready_to_generate:",
	}
	if got := planRow(plan); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("plan\n got %v\nwant %v", got, want)
	}
	if len(plan.Archive) != 1 || plan.Archive[0] != "s3" || plan.Kept != 2 || plan.Added != 1 || plan.Archived != 1 {
		t.Fatalf("counts %+v", plan)
	}
}

func TestPlanUpdateReplacesOnlyUntouchedReusedContent(t *testing.T) {
	have := []sharing.HeldLesson{
		held("s1", "a", "Fresh", "current", true, true, false),   // untouched reused: replaced
		held("s2", "b", "Tested", "locked", true, true, true),    // has a test session: kept
		held("s3", "c", "Mine", "locked", false, true, false),    // learner generated: kept
		held("s4", "d", "Waiting", "locked", true, false, false), // no content yet: replaced
	}
	next := snapOf([]sharing.SnapshotLesson{keyed("a", "Fresh v2"), keyed("b", "Tested v2"), keyed("c", "Mine v2"), keyed("d", "Waiting v2")})
	plan := sharing.PlanUpdate(have, next, "https://assets.example.test")
	got := map[string]string{}
	for _, row := range planRow(plan) {
		p := strings.Split(row, ":")
		got[p[0]] = p[2]
	}
	if got["a"] != sharing.ActionReplace || got["b"] != sharing.ActionKeep || got["c"] != sharing.ActionKeep || got["d"] != sharing.ActionReplace {
		t.Fatalf("actions %v", got)
	}
}

func TestPlanUpdateGateOpensOnlyTheFirstUnfinishedLesson(t *testing.T) {
	have := []sharing.HeldLesson{
		held("s1", "a", "A", "done", true, true, true),
		held("s2", "b", "B", "ready_to_generate", true, false, false),
		held("s3", "c", "C", "generating", false, false, false),
	}
	next := snapOf([]sharing.SnapshotLesson{keyed("a", "A"), {Key: "n", Title: "New first", Goal: "g"}, keyed("b", "B"), keyed("c", "C")})
	var statuses []string
	for _, row := range planRow(sharing.PlanUpdate(have, next, "https://assets.example.test")) {
		statuses = append(statuses, strings.Split(row, ":")[3])
	}
	// The new lesson is the first unfinished one and opens; B relocks; an in-flight generation is never touched.
	if strings.Join(statuses, ",") != "done,ready_to_generate,locked,generating" {
		t.Fatalf("statuses %v", statuses)
	}
}

func TestPlanUpdateIsDeterministicAcrossRuns(t *testing.T) {
	have := []sharing.HeldLesson{held("s1", "a", "A", "done", true, true, true), held("s2", "b", "B", "locked", true, true, false), held("s3", "x", "X", "locked", true, true, false)}
	next := snapOf([]sharing.SnapshotLesson{keyed("b", "B2"), keyed("a", "A")}, []sharing.SnapshotLesson{keyed("n", "N")})
	first := strings.Join(planRow(sharing.PlanUpdate(have, next, "https://assets.example.test")), "|")
	for i := 0; i < 20; i++ {
		if got := strings.Join(planRow(sharing.PlanUpdate(have, next, "https://assets.example.test")), "|"); got != first {
			t.Fatalf("run %d differs:\n%s\n%s", i, got, first)
		}
	}
}

func TestConfigReadsRepublishLimits(t *testing.T) {
	if c := sharing.DefaultConfig(); c.MaxRepublishesPerDay != 3 || c.MaxChangelogLen != 500 {
		t.Fatalf("defaults %+v", c)
	}
	env := map[string]string{"SHARING_MAX_REPUBLISHES_PER_DAY": "5", "SHARING_MAX_CHANGELOG_LENGTH": "nope"}
	c := sharing.ConfigFromEnv(func(k string) string { return env[k] })
	if c.MaxRepublishesPerDay != 5 || c.MaxChangelogLen != 500 {
		t.Fatalf("env %+v", c)
	}
}

type fakeUpdater struct {
	republishErr, applyErr, statusErr error
	changelog                         string
	lock, to                          int
}

func (f *fakeUpdater) Republish(_ context.Context, _, _, changelog string) (sharing.Draft, error) {
	f.changelog = changelog
	return sharing.Draft{Number: 2, Verdict: sharing.VerdictPass}, f.republishErr
}
func (f *fakeUpdater) Versions(context.Context, string, string) (sharing.VersionList, error) {
	return sharing.VersionList{Versions: []sharing.Version{}, Limit: 3}, nil
}
func (f *fakeUpdater) UpdateStatus(context.Context, string, string) (sharing.UpdateStatus, error) {
	return sharing.UpdateStatus{Changelog: []sharing.ChangelogEntry{}}, f.statusErr
}
func (f *fakeUpdater) ApplyUpdate(_ context.Context, _, _ string, lock, to int) (sharing.UpdateResult, error) {
	f.lock, f.to = lock, to
	return sharing.UpdateResult{Version: to}, f.applyErr
}
func (f *fakeUpdater) ArchivedLessons(context.Context, string, string) ([]sharing.ArchivedLesson, error) {
	return []sharing.ArchivedLesson{}, nil
}

func serveUpdate(t *testing.T, f *fakeUpdater, method, path, body, actor string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h := sharing.NewUpdateHandler(f)
	for _, p := range sharing.UpdatePatterns {
		mux.Handle(p, h)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if actor != "" {
		req = req.WithContext(accounts.WithLearnerID(req.Context(), actor))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestUpdateRoutesMapErrorsNeutrally(t *testing.T) {
	const id, actor = "33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444"
	if rec := serveUpdate(t, &fakeUpdater{}, "POST", "/v1/courses/"+id+"/republish", `{}`, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous republish: %d", rec.Code)
	}
	cases := []struct {
		name, method, path, body string
		f                        *fakeUpdater
		status                   int
		code                     string
	}{
		{"republish ok", "POST", "/republish", `{"changelog":"  New unit "}`, &fakeUpdater{}, 201, ""},
		{"limit", "POST", "/republish", `{}`, &fakeUpdater{republishErr: sharing.ErrRepublishLimit}, 429, "republish_limit"},
		{"not published", "POST", "/republish", `{}`, &fakeUpdater{republishErr: sharing.ErrNotPublished}, 409, "not_published"},
		{"foreign course", "POST", "/republish", `{}`, &fakeUpdater{republishErr: sharing.ErrUnavailable}, 404, "draft_unavailable"},
		{"long changelog", "POST", "/republish", `{"changelog":"` + strings.Repeat("x", 501) + `"}`, &fakeUpdater{}, 422, "changelog_too_long"},
		{"bad body", "POST", "/republish", `not json`, &fakeUpdater{}, 400, "bad_request"},
		{"stale update", "POST", "/update", `{"lock_version":1,"to_version":2}`, &fakeUpdater{applyErr: sharing.ErrUpdateUnavailable}, 409, "update_unavailable"},
		{"unknown copy status", "GET", "/update", ``, &fakeUpdater{statusErr: sharing.ErrUpdateUnavailable}, 409, "update_unavailable"},
		{"versions", "GET", "/versions", ``, &fakeUpdater{}, 200, ""},
		{"archived", "GET", "/archived-lessons", ``, &fakeUpdater{}, 200, ""},
		{"missing versions in body", "POST", "/update", `{}`, &fakeUpdater{}, 400, "bad_request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := serveUpdate(t, tc.f, tc.method, "/v1/courses/"+id+tc.path, tc.body, actor)
			if rec.Code != tc.status {
				t.Fatalf("status %d body %s", rec.Code, rec.Body)
			}
			if tc.code != "" && !strings.Contains(rec.Body.String(), `"error":"`+tc.code+`"`) {
				t.Fatalf("body %s", rec.Body)
			}
			if tc.code == "republish_limit" && rec.Header().Get("Retry-After") == "" {
				t.Fatal("limit needs Retry-After")
			}
		})
	}
	f := &fakeUpdater{}
	serveUpdate(t, f, "POST", "/v1/courses/"+id+"/republish", `{"changelog":"  New unit "}`, actor)
	if f.changelog != "New unit" {
		t.Fatalf("changelog not cleaned: %q", f.changelog)
	}
	f = &fakeUpdater{}
	serveUpdate(t, f, "POST", "/v1/courses/"+id+"/update", `{"lock_version":4,"to_version":9}`, actor)
	if f.lock != 4 || f.to != 9 {
		t.Fatalf("body not forwarded: %+v", f)
	}
}
