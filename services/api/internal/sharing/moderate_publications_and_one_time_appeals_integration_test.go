//go:build integration

package sharing_test

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
	"github.com/jackc/pgx/v5"
)

func grantModerator(t *testing.T, f *backuptest.Fixture, name string) string {
	t.Helper()
	id := newLearner(t, f, name)
	if _, err := f.DB.Pool.Exec(t.Context(), `INSERT INTO moderator_grants(account_id) VALUES($1)`, id); err != nil {
		t.Fatal(err)
	}
	return id
}

// hidden gets a listed course reported to the threshold, so it sits in the queue.
func reportedCourse(t *testing.T, f *backuptest.Fixture, s *sharing.Service, _, course, reason string) sharing.ModerationItem {
	t.Helper()
	for _, who := range reporters(t, f, 3) {
		if _, err := s.Report(t.Context(), who, sharing.ReportInput{TargetType: "course", TargetID: course, Reason: reason}); err != nil {
			t.Fatal(err)
		}
	}
	return itemFor(t, s, grantedAny(t, f), course)
}

// grantedAny returns an existing moderator of this fixture, granting one if there is none yet.
func grantedAny(t *testing.T, f *backuptest.Fixture) string {
	t.Helper()
	var id string
	err := f.DB.Pool.QueryRow(t.Context(), `SELECT account_id::text FROM moderator_grants ORDER BY granted_at,account_id LIMIT 1`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return grantModerator(t, f, "Queue reader")
	}
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func itemFor(t *testing.T, s *sharing.Service, mod, target string) sharing.ModerationItem {
	t.Helper()
	view, err := s.ModerationView(t.Context(), mod, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range view.Items {
		if it.TargetID == target {
			return it
		}
	}
	t.Fatalf("no queue item for %s in %+v", target, view.Items)
	return sharing.ModerationItem{}
}

func act(t *testing.T, s *sharing.Service, mod, item, action, code string) sharing.ActionResult {
	t.Helper()
	res, err := s.ModerateItem(t.Context(), mod, item, sharing.ActionInput{Action: action, ReasonCode: code, Reason: "Moderator explanation for " + action})
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
	return res
}

func TestModeratorRoleQueueOrderAndOverdueTarget(t *testing.T) {
	f := newSharingFixture(t)
	ev := &capture{}
	r := startRedis(t)
	s := marketService(t, f, ev, r, nil)
	plain := newLearner(t, f, "Plain learner")
	mod := grantModerator(t, f, "Moderator one")

	if ok, err := s.IsModerator(t.Context(), plain); err != nil || ok {
		t.Fatalf("plain moderator = %v %v", ok, err)
	}
	if ok, err := s.IsModerator(t.Context(), mod); err != nil || !ok {
		t.Fatalf("granted moderator = %v %v", ok, err)
	}
	if _, err := s.ModerationView(t.Context(), plain, 50); !errors.Is(err, sharing.ErrNotModerator) {
		t.Fatalf("plain view = %v", err)
	}
	if _, err := s.ModerateItem(t.Context(), plain, "00000000-0000-4000-8000-0000000000aa", sharing.ActionInput{Action: "dismiss", ReasonCode: "no_violation", Reason: "x"}); !errors.Is(err, sharing.ErrNotModerator) {
		t.Fatalf("plain action = %v", err)
	}

	low, high, held := marketOwner(t, f, "owner-low"), marketOwner(t, f, "owner-high"), marketOwner(t, f, "owner-held")
	lowCourse := listCourse(t, s, low, marketCourse(t, f, low, "Queue low spam", "Short lessons", 3), "tech", "en").CourseID
	highCourse := listCourse(t, s, high, marketCourse(t, f, high, "Queue high unsafe", "Short lessons", 3), "tech", "en").CourseID
	reportedCourse(t, f, s, low, lowCourse, "spam")
	hiItem := reportedCourse(t, f, s, high, highCourse, "unsafe")
	if _, err := f.DB.Pool.Exec(t.Context(), `UPDATE moderation_queue_items SET opened_at=now()-interval '50 hours' WHERE target_id=$1`, lowCourse); err != nil {
		t.Fatal(err)
	}
	// A borderline publication enters the queue and stays unlisted.
	heldCourse := marketCourse(t, f, held, "Chemistry safety borderline", "Storing cleaners safely borderline", 3)
	d, err := s.BuildDraft(t.Context(), held, heldCourse)
	if err != nil || d.Verdict != sharing.VerdictReview {
		t.Fatalf("draft = %+v %v", d, err)
	}

	view, err := s.ModerationView(t.Context(), mod, 50)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, it := range view.Items {
		order = append(order, it.TargetType+":"+it.Title)
	}
	// unsafe reports (100) outrank a held publication (90), which outranks spam (30).
	want := []string{"course:Queue high unsafe", "publication:Chemistry safety borderline", "course:Queue low spam"}
	if !slices.Equal(order, want) {
		t.Fatalf("order = %v; want %v", order, want)
	}
	if view.OpenCount != 3 || view.OverdueCount != 1 || view.ScreeningCount != 1 {
		t.Fatalf("counts = %+v", view)
	}
	if !view.Items[2].Overdue || view.Items[0].Overdue || view.Items[1].Overdue {
		t.Fatalf("overdue flags wrong: %+v", view.Items)
	}
	if hiItem.DueAt.Sub(hiItem.OpenedAt).Hours() != 48 {
		t.Fatalf("target = %v", hiItem.DueAt.Sub(hiItem.OpenedAt))
	}
	if pub := view.Items[1]; pub.Screening == nil || pub.Screening.Verdict != sharing.VerdictReview || len(pub.Screening.Codes) == 0 {
		t.Fatalf("screening info = %+v", pub.Screening)
	}
}

func TestResolveHeldPublicationsBeforeTheyAreVisible(t *testing.T) {
	f := newSharingFixture(t)
	r := startRedis(t)
	s := marketService(t, f, &capture{}, r, nil)
	mod := grantModerator(t, f, "Resolver")
	owner := marketOwner(t, f, "borderline-owner")
	course := marketCourse(t, f, owner, "Borderline chemistry", "Cleaners borderline", 3)
	if d, err := s.BuildDraft(t.Context(), owner, course); err != nil || d.Verdict != sharing.VerdictReview {
		t.Fatalf("draft = %+v %v", d, err)
	}
	if _, err := s.ListCourse(t.Context(), owner, course, sharing.ListingInput{Category: "tech", Language: "en"}); !errors.Is(err, sharing.ErrPublishNotAllowed) {
		t.Fatalf("held list = %v", err)
	}
	if _, err := s.PublishLink(t.Context(), owner, course); !errors.Is(err, sharing.ErrPublishNotAllowed) {
		t.Fatalf("held link = %v", err)
	}
	item := itemFor(t, s, mod, course)
	if item.TargetType != "publication" {
		t.Fatalf("target = %q", item.TargetType)
	}
	act(t, s, mod, item.ID, "dismiss", "no_violation")
	if _, err := s.ListCourse(t.Context(), owner, course, sharing.ListingInput{Category: "tech", Language: "en"}); err != nil {
		t.Fatalf("approved list = %v", err)
	}
	if n := count(t, f, `SELECT count(*) FROM moderation_queue_items WHERE status='open' AND target_id<>'00000000-0000-4000-8000-000000000000'`); n != 0 {
		t.Fatalf("%d items still open", n)
	}

	// Rejecting keeps it unpublishable; an engine outage (pending_screening) is resolved the same way.
	bad := marketCourse(t, f, owner, "Rejected borderline", "Weapons borderline", 3)
	if _, err := s.BuildDraft(t.Context(), owner, bad); err != nil {
		t.Fatal(err)
	}
	act(t, s, mod, itemFor(t, s, mod, bad).ID, "delist", "unsafe")
	if _, err := s.ListCourse(t.Context(), owner, bad, sharing.ListingInput{Category: "tech", Language: "en"}); !errors.Is(err, sharing.ErrPublishNotAllowed) {
		t.Fatalf("rejected list = %v", err)
	}
	down := marketService(t, f, &capture{}, r, nil)
	down.ConfigureScreening(downScreener{}, fixedRates{})
	pending := marketCourse(t, f, owner, "Pending screening course", "Short lessons", 3)
	if d, err := down.BuildDraft(t.Context(), owner, pending); err != nil || d.Verdict != sharing.VerdictPending {
		t.Fatalf("pending draft = %+v %v", d, err)
	}
	it := itemFor(t, s, mod, pending)
	if it.Screening == nil || it.Screening.Verdict != sharing.VerdictPending {
		t.Fatalf("pending item = %+v", it)
	}
	act(t, s, mod, it.ID, "dismiss", "no_violation")
	if _, err := s.ListCourse(t.Context(), owner, pending, sharing.ListingInput{Category: "tech", Language: "en"}); err != nil {
		t.Fatalf("approved pending list = %v", err)
	}
}

func TestActionsNeedReasonsAndWriteAnAppendOnlyAudit(t *testing.T) {
	f := newSharingFixture(t)
	r := startRedis(t)
	s := marketService(t, f, &capture{}, r, nil)
	mod := grantModerator(t, f, "Auditor")
	owner := marketOwner(t, f, "audit-owner")
	course := listCourse(t, s, owner, marketCourse(t, f, owner, "Audit target", "Short lessons", 3), "tech", "en").CourseID
	item := reportedCourse(t, f, s, owner, course, "spam")

	for name, in := range map[string]sharing.ActionInput{
		"no reason":        {Action: "warn", ReasonCode: "spam", Reason: "  "},
		"unknown action":   {Action: "obliterate", ReasonCode: "spam", Reason: "why"},
		"unknown code":     {Action: "warn", ReasonCode: "vibes", Reason: "why"},
		"too long":         {Action: "warn", ReasonCode: "spam", Reason: strings.Repeat("界", 1001)},
		"not for a course": {Action: "", ReasonCode: "spam", Reason: "why"},
	} {
		if _, err := s.ModerateItem(t.Context(), mod, item.ID, in); !errors.Is(err, sharing.ErrInvalidModeration) {
			t.Fatalf("%s = %v", name, err)
		}
	}
	if n := count(t, f, `SELECT count(*) FROM moderation_actions`); n != 0 {
		t.Fatalf("%d audit rows after refusals", n)
	}
	res := act(t, s, mod, item.ID, "warn", "spam")
	var moderatorID, action, code string
	if err := f.DB.Pool.QueryRow(t.Context(), `SELECT moderator_id::text,action,reason_code FROM moderation_actions WHERE id=$1`, res.ID).Scan(&moderatorID, &action, &code); err != nil ||
		moderatorID != mod || action != "warn" || code != "spam" {
		t.Fatalf("audit = %s %s %s %v", moderatorID, action, code, err)
	}
	if _, err := f.DB.Pool.Exec(t.Context(), `UPDATE moderation_actions SET reason='edited' WHERE id=$1`, res.ID); err == nil {
		t.Fatal("audit row was updated")
	}
	if _, err := f.DB.Pool.Exec(t.Context(), `DELETE FROM moderation_actions WHERE id=$1`, res.ID); err == nil {
		t.Fatal("audit row was deleted")
	}
	// The item was resolved by exactly one action; a second one finds nothing.
	if _, err := s.ModerateItem(t.Context(), mod, item.ID, sharing.ActionInput{Action: "warn", ReasonCode: "spam", Reason: "again"}); !errors.Is(err, sharing.ErrCourseUnavailable) {
		t.Fatalf("resolved item = %v", err)
	}
	// A moderator cannot act on their own content.
	selfMod := marketOwner(t, f, "self-moderator")
	if _, err := f.DB.Pool.Exec(t.Context(), `INSERT INTO moderator_grants(account_id) VALUES($1)`, selfMod); err != nil {
		t.Fatal(err)
	}
	own := listCourse(t, s, selfMod, marketCourse(t, f, selfMod, "Moderator own course", "Short lessons", 3), "tech", "en").CourseID
	ownItem := reportedCourse(t, f, s, selfMod, own, "spam")
	if _, err := s.ModerateItem(t.Context(), selfMod, ownItem.ID, sharing.ActionInput{Action: "dismiss", ReasonCode: "no_violation", Reason: "mine"}); !errors.Is(err, sharing.ErrSelfModeration) {
		t.Fatalf("self moderation = %v", err)
	}
}

func TestDelistKeepsCopiesAndSevereTakedownBlocksThem(t *testing.T) {
	f := newSharingFixture(t)
	r := startRedis(t)
	ev := &capture{}
	s := marketService(t, f, ev, r, nil)
	mod := grantModerator(t, f, "Enforcer")
	owner := marketOwner(t, f, "enforce-owner")
	spam := marketCourse(t, f, owner, "Spammy enforcement", "Short lessons", 3)
	unsafe := marketCourse(t, f, owner, "Unsafe enforcement", "Short lessons", 3)
	listCourse(t, s, owner, spam, "tech", "en")
	listCourse(t, s, owner, unsafe, "tech", "en")
	spamToken, err := s.PublishLink(t.Context(), owner, spam)
	if err != nil {
		t.Fatal(err)
	}
	unsafeToken, err := s.PublishLink(t.Context(), owner, unsafe)
	if err != nil {
		t.Fatal(err)
	}
	learner := newLearner(t, f, "Copy holder")
	spamCopy, err := s.EnrollMarketplace(t.Context(), learner, spam)
	if err != nil {
		t.Fatal(err)
	}
	unsafeCopy, err := s.EnrollMarketplace(t.Context(), learner, unsafe)
	if err != nil {
		t.Fatal(err)
	}
	spamItem := reportedCourse(t, f, s, owner, spam, "spam")
	unsafeItem := reportedCourse(t, f, s, owner, unsafe, "unsafe")
	blocked := func(copyID string) bool {
		return count(t, f, `SELECT count(*) FROM course_copies WHERE copy_course_id=$1 AND blocked_at IS NOT NULL`, copyID) == 1
	}
	if len(pageKeys(t, r)) == 0 {
		browseTitles(t, s, "", sharing.BrowseQuery{Sort: "new"})
	}

	// Normal takedown: delisted, link revoked, enrollment closed, existing copy keeps access.
	res := act(t, s, mod, spamItem.ID, "takedown", "spam")
	if res.Severe || res.CopiesBlocked != 0 || blocked(spamCopy.CourseID) {
		t.Fatalf("normal takedown = %+v blocked=%v", res, blocked(spamCopy.CourseID))
	}
	if _, err = s.GetPreview(t.Context(), spamToken, ""); !errors.Is(err, sharing.ErrLinkNotFound) {
		t.Fatalf("revoked link preview = %v", err)
	}
	if _, err = s.Enroll(t.Context(), newLearner(t, f, "Late link"), spamToken); err == nil {
		t.Fatal("enrolled through a revoked link")
	}
	if _, err = s.EnrollMarketplace(t.Context(), newLearner(t, f, "Late market"), spam); !errors.Is(err, sharing.ErrCourseUnavailable) {
		t.Fatalf("marketplace enroll = %v", err)
	}
	if _, err = s.ListCourse(t.Context(), owner, spam, sharing.ListingInput{Category: "tech", Language: "en"}); !errors.Is(err, sharing.ErrModerated) {
		t.Fatalf("relist = %v", err)
	}
	if _, err = s.PublishLink(t.Context(), owner, spam); !errors.Is(err, sharing.ErrModerated) {
		t.Fatalf("relink = %v", err)
	}
	if got := len(pageKeys(t, r)); got != 0 {
		t.Fatalf("catalogue cache kept %d pages after takedown", got)
	}

	// Severe takedown also blocks the existing copy.
	res = act(t, s, mod, unsafeItem.ID, "takedown", "unsafe")
	if !res.Severe || res.CopiesBlocked != 1 || !blocked(unsafeCopy.CourseID) || blocked(spamCopy.CourseID) {
		t.Fatalf("severe takedown = %+v", res)
	}
	if _, err = s.GetPreview(t.Context(), unsafeToken, ""); !errors.Is(err, sharing.ErrLinkNotFound) {
		t.Fatalf("unsafe link = %v", err)
	}
	if n := count(t, f, `SELECT count(*) FROM courses WHERE id=$1 AND visibility='private'`, unsafe); n != 1 {
		t.Fatalf("visibility not private")
	}
	sawVisibility := len(ev.named("course_visibility_changed")) > 0
	sawAction := false
	for _, props := range ev.named("moderation_action") {
		sawAction = sawAction || (props["action"] == "takedown" && props["reason_code"] == "unsafe" && props["target_type"] == "course")
		for _, v := range props {
			if str, ok := v.(string); ok && strings.Contains(str, "Moderator explanation") {
				t.Fatalf("reason text leaked into analytics: %v", props)
			}
		}
	}
	if !sawVisibility || !sawAction {
		t.Fatalf("events visibility=%v action=%v", sawVisibility, sawAction)
	}

	// A plain delist leaves link-less pinned access untouched and blocks relisting.
	plain := marketCourse(t, f, owner, "Plain delist", "Short lessons", 3)
	listCourse(t, s, owner, plain, "tech", "en")
	plainCopy, err := s.EnrollMarketplace(t.Context(), learner, plain)
	if err != nil {
		t.Fatal(err)
	}
	plainItem := reportedCourse(t, f, s, owner, plain, "misleading")
	if res = act(t, s, mod, plainItem.ID, "delist", "misleading"); res.CopiesBlocked != 0 || blocked(plainCopy.CourseID) {
		t.Fatalf("delist blocked a copy: %+v", res)
	}
}

func TestAuthorNoticeAndOneAppealDecidedByAnotherModerator(t *testing.T) {
	f := newSharingFixture(t)
	r := startRedis(t)
	ev := &capture{}
	s := marketService(t, f, ev, r, nil)
	first, second := grantModerator(t, f, "First moderator"), grantModerator(t, f, "Second moderator")
	owner := marketOwner(t, f, "appeal-owner")
	stranger := newLearner(t, f, "Stranger")
	course := listCourse(t, s, owner, marketCourse(t, f, owner, "Appealed unsafe", "Short lessons", 3), "tech", "en").CourseID
	holder := newLearner(t, f, "Appeal copy holder")
	copyRes, err := s.EnrollMarketplace(t.Context(), holder, course)
	if err != nil {
		t.Fatal(err)
	}
	item := reportedCourse(t, f, s, owner, course, "unsafe")
	res := act(t, s, first, item.ID, "takedown", "unsafe")

	notices, err := s.Notices(t.Context(), owner)
	if err != nil || len(notices) != 1 {
		t.Fatalf("notices = %+v %v", notices, err)
	}
	n := notices[0]
	if n.ID != res.ID || n.Reason != "Moderator explanation for takedown" || !n.Severe || !n.CanAppeal || n.Appeal != nil || n.Title != "Appealed unsafe" {
		t.Fatalf("notice = %+v", n)
	}
	if other, err := s.Notices(t.Context(), stranger); err != nil || len(other) != 0 {
		t.Fatalf("stranger notices = %+v %v", other, err)
	}
	if _, err = s.FileAppeal(t.Context(), stranger, res.ID, "Not mine"); !errors.Is(err, sharing.ErrCourseUnavailable) {
		t.Fatalf("stranger appeal = %v", err)
	}
	if _, err = s.FileAppeal(t.Context(), owner, res.ID, "   "); !errors.Is(err, sharing.ErrInvalidModeration) {
		t.Fatalf("empty appeal = %v", err)
	}

	// Two simultaneous appeals create exactly one.
	var wg sync.WaitGroup
	results := make([]error, 2)
	var appealID string
	var mu sync.Mutex
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := s.FileAppeal(t.Context(), owner, res.ID, "The lessons are safe.")
			mu.Lock()
			defer mu.Unlock()
			results[i] = err
			if err == nil {
				appealID = id
			}
		}()
	}
	wg.Wait()
	used := 0
	for _, err := range results {
		if errors.Is(err, sharing.ErrAppealUsed) {
			used++
		}
	}
	if used != 1 || appealID == "" || count(t, f, `SELECT count(*) FROM moderation_appeals`) != 1 {
		t.Fatalf("results = %v appeal=%q", results, appealID)
	}
	if _, err = s.FileAppeal(t.Context(), owner, res.ID, "A third try"); !errors.Is(err, sharing.ErrAppealUsed) {
		t.Fatalf("second appeal = %v", err)
	}
	if view, err := s.ModerationView(t.Context(), second, 50); err != nil || len(view.Appeals) != 1 || view.Appeals[0].Text != "The lessons are safe." {
		t.Fatalf("appeals = %+v %v", view.Appeals, err)
	}

	// The acting moderator and non-moderators cannot decide; the state stays open.
	if err = s.DecideAppeal(t.Context(), first, appealID, "overturn", "I changed my mind"); !errors.Is(err, sharing.ErrSameModerator) {
		t.Fatalf("same moderator = %v", err)
	}
	if err = s.DecideAppeal(t.Context(), owner, appealID, "overturn", "Let me out"); !errors.Is(err, sharing.ErrNotModerator) {
		t.Fatalf("author decides = %v", err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `UPDATE moderation_appeals SET status='overturned',decided_by=$2,decided_at=now() WHERE id=$1`, appealID, first); err == nil {
		t.Fatal("database allowed the acting moderator to decide")
	}
	if err = s.DecideAppeal(t.Context(), second, appealID, "overturn", ""); !errors.Is(err, sharing.ErrInvalidModeration) {
		t.Fatalf("decision without reason = %v", err)
	}
	if count(t, f, `SELECT count(*) FROM moderation_appeals WHERE status='open'`) != 1 {
		t.Fatal("appeal no longer open")
	}

	// A different moderator overturns: the course is restored and the copy unblocked.
	if err = s.DecideAppeal(t.Context(), second, appealID, "overturn", "The lessons are safe."); err != nil {
		t.Fatal(err)
	}
	if err = s.DecideAppeal(t.Context(), second, appealID, "uphold", "Twice"); err == nil {
		t.Fatal("decided twice")
	}
	if got, _ := browseTitles(t, s, "", sharing.BrowseQuery{Q: "Appealed", Sort: "new"}); !slices.Contains(got, "Appealed unsafe") {
		t.Fatalf("restored course missing: %v", got)
	}
	if count(t, f, `SELECT count(*) FROM course_copies WHERE copy_course_id=$1 AND blocked_at IS NULL`, copyRes.CourseID) != 1 {
		t.Fatal("copy still blocked")
	}
	if notices, _ = s.Notices(t.Context(), owner); len(notices) != 1 || notices[0].CanAppeal || notices[0].Appeal == nil || notices[0].Appeal.Status != "overturned" {
		t.Fatalf("notice after = %+v", notices)
	}
	var outcomes []string
	for _, props := range ev.named("moderation_appeal") {
		outcomes = append(outcomes, props["outcome"].(string))
	}
	if !slices.Equal(outcomes, []string{"filed", "overturned"}) {
		t.Fatalf("appeal events = %v", outcomes)
	}

	// An upheld appeal keeps the enforcement.
	other := listCourse(t, s, owner, marketCourse(t, f, owner, "Upheld unsafe", "Short lessons", 3), "tech", "en").CourseID
	otherCopy, _ := s.EnrollMarketplace(t.Context(), holder, other)
	it2 := reportedCourse(t, f, s, owner, other, "unsafe")
	res2 := act(t, s, first, it2.ID, "takedown", "unsafe")
	id2, err := s.FileAppeal(t.Context(), owner, res2.ID, "Please review again.")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DecideAppeal(t.Context(), second, id2, "uphold", "The decision stands."); err != nil {
		t.Fatal(err)
	}
	if count(t, f, `SELECT count(*) FROM course_copies WHERE copy_course_id=$1 AND blocked_at IS NOT NULL`, otherCopy.CourseID) != 1 {
		t.Fatal("upheld takedown released the copy")
	}
}

func TestReviewAndUserTargetsUseTheSameAuditedContract(t *testing.T) {
	f := newSharingFixture(t)
	r := startRedis(t)
	s := marketService(t, f, &capture{}, r, nil)
	mod := grantModerator(t, f, "Contract moderator")
	owner := marketOwner(t, f, "contract-owner")
	course := listCourse(t, s, owner, marketCourse(t, f, owner, "Contract course", "Short lessons", 3), "tech", "en").CourseID
	reviewer := newLearner(t, f, "Contract reviewer")
	if _, err := s.EnrollMarketplace(t.Context(), reviewer, course); err != nil {
		t.Fatal(err)
	}
	if _, err := f.DB.Pool.Exec(t.Context(), `UPDATE course_copies SET created_at=now()-interval '2 days' WHERE learner_id=$1`, reviewer); err != nil {
		t.Fatal(err)
	}
	var reviewID string
	if err := f.DB.Pool.QueryRow(t.Context(), `INSERT INTO course_reviews(course_id,learner_id,version,stars,text,verdict) VALUES($1,$2,1,2,'Rude words here','pass') RETURNING id::text`, course, reviewer).Scan(&reviewID); err != nil {
		t.Fatal(err)
	}
	claimUsername(t, f, reviewer, "contract.reviewer")
	who := reporters(t, f, 2)
	if _, err := s.Report(t.Context(), who[0], sharing.ReportInput{TargetType: "review", TargetID: reviewID, Reason: "inappropriate"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Report(t.Context(), who[1], sharing.ReportInput{TargetType: "user", TargetID: "contract.reviewer", Reason: "spam"}); err != nil {
		t.Fatal(err)
	}
	reviewItem := itemFor(t, s, mod, reviewID)
	userItem := itemFor(t, s, mod, reviewer)
	if reviewItem.TargetType != "review" || userItem.TargetType != "user" {
		t.Fatalf("items = %+v %+v", reviewItem, userItem)
	}
	// Matrix: a user cannot be delisted, a review cannot ban publishing.
	if _, err := s.ModerateItem(t.Context(), mod, userItem.ID, sharing.ActionInput{Action: "delist", ReasonCode: "spam", Reason: "no"}); !errors.Is(err, sharing.ErrInvalidModeration) {
		t.Fatalf("user delist = %v", err)
	}
	if _, err := s.ModerateItem(t.Context(), mod, reviewItem.ID, sharing.ActionInput{Action: "ban_publishing", ReasonCode: "spam", Reason: "no"}); !errors.Is(err, sharing.ErrInvalidModeration) {
		t.Fatalf("review ban = %v", err)
	}
	act(t, s, mod, reviewItem.ID, "delist", "inappropriate")
	if count(t, f, `SELECT count(*) FROM course_reviews WHERE id=$1 AND verdict='block'`, reviewID) != 1 {
		t.Fatal("review still visible")
	}
	if n := count(t, f, `SELECT count(*) FROM moderation_actions WHERE target_type IN ('review','user') OR action='delist'`); n != 1 {
		t.Fatalf("audit rows = %d", n)
	}

	// Banning a publisher delists their listings and refuses new publishing.
	banItem := userItem
	res := act(t, s, mod, banItem.ID, "ban_publishing", "spam")
	if res.Action != "ban_publishing" {
		t.Fatalf("ban = %+v", res)
	}
	// reviewer has no courses; ban the owner through their course instead.
	again := reportedCourse(t, f, s, owner, course, "spam")
	act(t, s, mod, again.ID, "ban_publishing", "spam")
	if got, _ := browseTitles(t, s, "", sharing.BrowseQuery{Q: "Contract", Sort: "new"}); len(got) != 0 {
		t.Fatalf("banned owner still listed: %v", got)
	}
	fresh := marketCourse(t, f, owner, "Banned new course", "Short lessons", 3)
	if _, err := s.BuildDraft(t.Context(), owner, fresh); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListCourse(t.Context(), owner, fresh, sharing.ListingInput{Category: "tech", Language: "en"}); !errors.Is(err, sharing.ErrModerated) {
		t.Fatalf("banned list = %v", err)
	}
	if _, err := s.PublishLink(t.Context(), owner, fresh); !errors.Is(err, sharing.ErrModerated) {
		t.Fatalf("banned link = %v", err)
	}
}
