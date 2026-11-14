//go:build integration

package sharing_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/backup"
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
	"github.com/jackc/pgx/v5/pgxpool"
)

// List and Delete make the in-memory double a retention-job store as well.
func (m *memObjects) List(_ context.Context, prefix string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var keys []string
	for k := range m.puts {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (m *memObjects) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.puts, key)
	return nil
}

// memLedger is the backup-ledger double; failNext simulates an unavailable ledger.
type memLedger struct {
	mu       sync.Mutex
	blocks   map[string]string
	lifted   []string
	failNext bool
}

func (l *memLedger) RecordBlock(_ context.Context, courseRef, actionRef string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failNext {
		return errors.New("ledger unavailable")
	}
	if l.blocks == nil {
		l.blocks = map[string]string{}
	}
	l.blocks[actionRef] = courseRef
	return nil
}

func (l *memLedger) LiftBlock(_ context.Context, actionRef string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lifted = append(l.lifted, actionRef)
	return nil
}

// shareAs screens the course as its owner and creates the share link.
func shareAs(t *testing.T, s *sharing.Service, ownerID, course string) string {
	t.Helper()
	if d, err := s.BuildDraft(t.Context(), ownerID, course); err != nil || d.Verdict != sharing.VerdictPass {
		t.Fatalf("draft: %v %+v", err, d)
	}
	token, err := s.PublishLink(t.Context(), ownerID, course)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

type retentionRow struct {
	Refs                        int
	Released, PurgeAfter, Purge *time.Time
}

func rowFor(t *testing.T, pool *pgxpool.Pool, prefix string) retentionRow {
	t.Helper()
	var r retentionRow
	if err := pool.QueryRow(t.Context(), `SELECT ref_count,released_at,purge_after,purged_at FROM shared_asset_retention WHERE prefix=$1`, prefix).Scan(&r.Refs, &r.Released, &r.PurgeAfter, &r.Purge); err != nil {
		t.Fatalf("retention row %s: %v", prefix, err)
	}
	return r
}

func prefixOf(t *testing.T, f *backuptest.Fixture, copyID string) string {
	t.Helper()
	var p string
	if err := f.DB.Pool.QueryRow(t.Context(), `SELECT 'lessons/published/'||draft_id::text||'/' FROM course_copies WHERE copy_course_id=$1`, copyID).Scan(&p); err != nil {
		t.Fatal(err)
	}
	return p
}

func endCopy(t *testing.T, f *backuptest.Fixture, copyID string) {
	t.Helper()
	if _, err := f.DB.Pool.Exec(t.Context(), `UPDATE courses SET deleted_at=now() WHERE id=$1`, copyID); err != nil {
		t.Fatal(err)
	}
}

func sweep(t *testing.T, s *sharing.Service, at time.Time) sharing.SweepResult {
	t.Helper()
	r, err := s.SweepAssets(t.Context(), at)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	return r
}

// everyLiveReferenceIsKept is the safety property: no prefix a live copy still uses is released or purged.
func everyLiveReferenceIsKept(t *testing.T, f *backuptest.Fixture, s *sharing.Service) {
	t.Helper()
	live, err := s.RetainedObjectPrefixes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range live {
		var gone int
		if err = f.DB.Pool.QueryRow(t.Context(), `SELECT count(*) FROM shared_asset_retention r WHERE r.prefix=$1 AND (r.purged_at IS NOT NULL OR (r.released_at IS NOT NULL AND EXISTS (
SELECT 1 FROM course_copies cc JOIN courses c ON c.id=cc.copy_course_id WHERE c.deleted_at IS NULL AND 'lessons/published/'||cc.draft_id::text||'/'=r.prefix)))`, p).Scan(&gone); err != nil {
			t.Fatal(err)
		}
		if gone != 0 {
			t.Fatalf("live prefix %s was released or purged", p)
		}
	}
}

func TestUnpublishStopsNewEnrollmentsAndKeepsEveryCopy(t *testing.T) {
	f := newSharingFixture(t)
	ev := &capture{}
	s, _ := enrollService(t, f, ev, func(c *sharing.Config) { c.MarketplaceEnabled = true })
	ownerID := marketOwner(t, f, "unpublish-owner")
	course := marketCourse(t, f, ownerID, "Unpublish tides", "Short lessons", 3)
	listCourse(t, s, ownerID, course, "tech", "en")
	token := shareAs(t, s, ownerID, course)
	learner := newLearner(t, f, "Copy holder")
	got, err := s.Enroll(t.Context(), learner, token)
	if err != nil || !got.Created {
		t.Fatalf("enroll: %v %+v", err, got)
	}
	if titles, _ := browseTitles(t, s, "", sharing.BrowseQuery{Sort: "new"}); !slices.Contains(titles, "Unpublish tides") {
		t.Fatalf("not listed before unpublish: %v", titles)
	}
	manifests := count(t, f, `SELECT count(*) FROM lesson_manifests WHERE learner_id=$1`, learner)
	if manifests == 0 {
		t.Fatal("copy has no manifests")
	}

	if err = s.Unpublish(t.Context(), learner, course); !errors.Is(err, sharing.ErrUnavailable) {
		t.Fatalf("foreign unpublish = %v", err)
	}
	if count(t, f, `SELECT count(*) FROM courses WHERE id=$1 AND visibility='private'`, course) != 0 {
		t.Fatal("a stranger changed visibility")
	}
	if err = s.Unpublish(t.Context(), ownerID, course); err != nil {
		t.Fatalf("unpublish: %v", err)
	}
	if err = s.Unpublish(t.Context(), ownerID, course); err != nil {
		t.Fatalf("unpublish must be idempotent: %v", err)
	}

	if count(t, f, `SELECT count(*) FROM courses WHERE id=$1 AND visibility='private'`, course) != 1 {
		t.Fatal("course is not private")
	}
	if _, err = s.GetPreview(t.Context(), token, ""); !errors.Is(err, sharing.ErrLinkNotFound) {
		t.Fatalf("preview after unpublish = %v", err)
	}
	if _, err = s.Enroll(t.Context(), newLearner(t, f, "Late link"), token); err == nil {
		t.Fatal("enrolled through a revoked link")
	}
	if _, err = s.EnrollMarketplace(t.Context(), newLearner(t, f, "Late market"), course); !errors.Is(err, sharing.ErrCourseUnavailable) {
		t.Fatalf("marketplace enroll = %v", err)
	}
	if titles, _ := browseTitles(t, s, "", sharing.BrowseQuery{Sort: "new"}); slices.Contains(titles, "Unpublish tides") {
		t.Fatalf("still listed: %v", titles)
	}
	if count(t, f, `SELECT count(*) FROM marketplace_listings WHERE course_id=$1 AND delisted_at IS NOT NULL`, course) != 1 {
		t.Fatal("listing not delisted")
	}
	// The copy is untouched: same pin, same manifests, course alive and not blocked.
	if count(t, f, `SELECT count(*) FROM course_copies cc JOIN courses c ON c.id=cc.copy_course_id WHERE cc.copy_course_id=$1 AND cc.blocked_at IS NULL AND c.deleted_at IS NULL AND cc.version=1`, got.CourseID) != 1 ||
		count(t, f, `SELECT count(*) FROM lesson_manifests WHERE learner_id=$1`, learner) != manifests {
		t.Fatal("unpublish touched the copy")
	}
	changes := 0
	for _, e := range ev.named("course_visibility_changed") {
		if e["to"] == "private" {
			changes++
		}
	}
	if changes != 1 {
		t.Fatalf("private visibility events = %d, want exactly one", changes)
	}
}

func TestOwnerAccountDeletionAnonymizesAttributionAndCopiesKeepWorking(t *testing.T) {
	f := newSharingFixture(t)
	s, objects := enrollService(t, f, &capture{}, nil)
	s = s.WithAssetStore(objects)
	ownerID := marketOwner(t, f, "leaving-owner")
	course := marketCourse(t, f, ownerID, "Orphaned tides", "Short lessons", 3)
	token := shareAs(t, s, ownerID, course)
	learner := newLearner(t, f, "Keeper")
	got, err := s.Enroll(t.Context(), learner, token)
	if err != nil {
		t.Fatal(err)
	}
	if count(t, f, `SELECT count(*) FROM course_copies WHERE copy_course_id=$1 AND source_owner_id=$2`, got.CourseID, ownerID) != 1 {
		t.Fatal("copy did not record its creator")
	}
	manifests := count(t, f, `SELECT count(*) FROM lesson_manifests WHERE learner_id=$1`, learner)

	if _, err = f.DB.Pool.Exec(t.Context(), `DELETE FROM learners WHERE id=$1`, ownerID); err != nil {
		t.Fatal(err)
	}
	if count(t, f, `SELECT count(*) FROM course_copies WHERE copy_course_id=$1 AND source_owner_id IS NULL AND source_course_id IS NULL AND version=1`, got.CourseID) != 1 {
		t.Fatal("copy kept an owner reference or lost its pin")
	}
	if count(t, f, `SELECT count(*) FROM courses WHERE id=$1 AND deleted_at IS NULL`, got.CourseID) != 1 ||
		count(t, f, `SELECT count(*) FROM lesson_manifests WHERE learner_id=$1`, learner) != manifests {
		t.Fatal("copy lost lessons with its owner")
	}
	if _, err = s.Enroll(t.Context(), newLearner(t, f, "Too late"), token); err == nil {
		t.Fatal("enrolled into a deleted owner's course")
	}
	// The owner's personal data is gone, but the assets the copy pins stay for it.
	r := sweep(t, s, time.Now())
	if r.Released != 0 || r.Purged != 0 {
		t.Fatalf("assets of a live copy were released: %+v", r)
	}
	keys, _ := objects.List(t.Context(), prefixOf(t, f, got.CourseID))
	if len(keys) == 0 {
		t.Fatal("pinned objects were removed with the owner")
	}
	everyLiveReferenceIsKept(t, f, s)
}

func TestReferenceCountsKeepAssetsUntilTheLastCopyEndsThenPurgeAfterTheConfiguredPeriod(t *testing.T) {
	f := newSharingFixture(t)
	s, objects := enrollService(t, f, &capture{}, nil)
	s = s.WithAssetStore(objects)
	ownerID := marketOwner(t, f, "retention-owner")
	course := marketCourse(t, f, ownerID, "Retained tides", "Short lessons", 3)
	token := shareAs(t, s, ownerID, course)
	enroll := func(name string) string {
		t.Helper()
		got, err := s.Enroll(t.Context(), newLearner(t, f, name), token)
		if err != nil || !got.Created {
			t.Fatalf("enroll %s: %v", name, err)
		}
		return got.CourseID
	}
	a, b := enroll("Copy A"), enroll("Copy B")
	prefix := prefixOf(t, f, a)
	keys, _ := objects.List(t.Context(), prefix)
	if len(keys) == 0 {
		t.Fatal("no published objects were written")
	}
	base := time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)

	if r := sweep(t, s, base); r.Tracked != 1 || r.Released != 0 || rowFor(t, f.DB.Pool, prefix).Refs != 2 {
		t.Fatalf("two copies: %+v %+v", r, rowFor(t, f.DB.Pool, prefix))
	}
	endCopy(t, f, a)
	if r := sweep(t, s, base); r.Released != 0 || rowFor(t, f.DB.Pool, prefix).Refs != 1 {
		t.Fatalf("one copy left: %+v %+v", r, rowFor(t, f.DB.Pool, prefix))
	}
	endCopy(t, f, b)
	if r := sweep(t, s, base); r.Released != 1 {
		t.Fatalf("last copy ended: %+v", r)
	}
	row := rowFor(t, f.DB.Pool, prefix)
	if row.Refs != 0 || row.Released == nil || row.PurgeAfter == nil || row.PurgeAfter.Sub(base.Add(30*24*time.Hour)).Abs() > time.Millisecond {
		t.Fatalf("release row = %+v", row)
	}
	// Nothing is deleted before the period is over.
	if r := sweep(t, s, base.Add(29*24*time.Hour)); r.Purged != 0 {
		t.Fatalf("purged early: %+v", r)
	}
	if left, _ := objects.List(t.Context(), prefix); len(left) != len(keys) {
		t.Fatalf("objects removed early: %v", left)
	}
	if r := sweep(t, s, base.Add(31*24*time.Hour)); r.Purged != 1 {
		t.Fatalf("due purge: %+v", r)
	}
	if left, _ := objects.List(t.Context(), prefix); len(left) != 0 {
		t.Fatalf("objects kept after purge: %v", left)
	}
	if rowFor(t, f.DB.Pool, prefix).Purge == nil {
		t.Fatal("purge not recorded")
	}
	if r := sweep(t, s, base.Add(32*24*time.Hour)); r.Purged != 0 || r.Released != 0 {
		t.Fatalf("second pass must be idempotent: %+v", r)
	}

	// A new copy after a purge gets its objects back and cancels the release.
	c := enroll("Copy C")
	if prefixOf(t, f, c) != prefix {
		t.Fatal("same version must reuse the prefix")
	}
	if left, _ := objects.List(t.Context(), prefix); len(left) != len(keys) {
		t.Fatalf("objects not republished: %v", left)
	}
	if row = rowFor(t, f.DB.Pool, prefix); row.Released != nil || row.Purge != nil || row.PurgeAfter != nil {
		t.Fatalf("enrollment must cancel the release: %+v", row)
	}
	// A copy that comes back inside the window also cancels the pending purge.
	endCopy(t, f, c)
	sweep(t, s, base.Add(40*24*time.Hour))
	if _, err := f.DB.Pool.Exec(t.Context(), `UPDATE courses SET deleted_at=NULL WHERE id=$1`, c); err != nil {
		t.Fatal(err)
	}
	if r := sweep(t, s, base.Add(50*24*time.Hour)); r.Revived != 1 || r.Purged != 0 {
		t.Fatalf("revive: %+v", r)
	}
	everyLiveReferenceIsKept(t, f, s)
}

func TestAssetPurgePeriodComesFromConfiguration(t *testing.T) {
	f := newSharingFixture(t)
	s, objects := enrollService(t, f, &capture{}, func(c *sharing.Config) { c.AssetPurgeDays = 15 })
	s = s.WithAssetStore(objects)
	ownerID := marketOwner(t, f, "config-owner")
	token := shareAs(t, s, ownerID, marketCourse(t, f, ownerID, "Configured tides", "Short lessons", 3))
	got, err := s.Enroll(t.Context(), newLearner(t, f, "Only copy"), token)
	if err != nil {
		t.Fatal(err)
	}
	endCopy(t, f, got.CourseID)
	base := time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)
	sweep(t, s, base)
	if r := sweep(t, s, base.Add(14*24*time.Hour)); r.Purged != 0 {
		t.Fatalf("purged inside 15 days: %+v", r)
	}
	if r := sweep(t, s, base.Add(16*24*time.Hour)); r.Purged != 1 {
		t.Fatalf("not purged after 15 days: %+v", r)
	}
}

func TestEnrollmentRacingAPurgeNeverLosesObjectsOfALiveCopy(t *testing.T) {
	f := newSharingFixture(t)
	s, objects := enrollService(t, f, &capture{}, nil)
	s = s.WithAssetStore(objects)
	ownerID := marketOwner(t, f, "race-owner")
	token := shareAs(t, s, ownerID, marketCourse(t, f, ownerID, "Racing tides", "Short lessons", 3))
	base := time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)
	previous := ""
	for round := 0; round < 6; round++ {
		if previous != "" {
			endCopy(t, f, previous)
			sweep(t, s, base) // releases the prefix; the purge is already due at base+31d
		}
		learner := newLearner(t, f, fmt.Sprintf("Racer %d", round))
		var wg sync.WaitGroup
		var enrolled sharing.Enrollment
		var enrollErr, sweepErr error
		wg.Add(2)
		go func() { defer wg.Done(); enrolled, enrollErr = s.Enroll(t.Context(), learner, token) }()
		go func() { defer wg.Done(); _, sweepErr = s.SweepAssets(t.Context(), base.Add(31*24*time.Hour)) }()
		wg.Wait()
		if enrollErr != nil || sweepErr != nil {
			t.Fatalf("round %d: enroll=%v sweep=%v", round, enrollErr, sweepErr)
		}
		rows, err := f.DB.Pool.Query(t.Context(), `SELECT manifest_key FROM lesson_manifests WHERE learner_id=$1 ORDER BY manifest_key`, learner)
		if err != nil {
			t.Fatal(err)
		}
		var keys []string
		for rows.Next() {
			var k string
			if err = rows.Scan(&k); err != nil {
				t.Fatal(err)
			}
			keys = append(keys, k)
		}
		rows.Close()
		if len(keys) == 0 {
			t.Fatalf("round %d: no manifests", round)
		}
		for _, k := range keys {
			if !strings.HasPrefix(k, "lessons/published/") {
				t.Fatalf("manifest key %q", k)
			}
			if has, _ := objects.List(t.Context(), k); len(has) != 1 {
				t.Fatalf("round %d: live copy lost %s", round, k)
			}
		}
		row := rowFor(t, f.DB.Pool, prefixOf(t, f, enrolled.CourseID))
		if row.Purge != nil || row.Released != nil || row.Refs < 0 {
			t.Fatalf("round %d: live prefix marked %+v", round, row)
		}
		previous = enrolled.CourseID
		everyLiveReferenceIsKept(t, f, s)
	}
}

func TestSevereTakedownReachesTheBackupLedgerBeforeCommitAndOverturnLiftsIt(t *testing.T) {
	f := newSharingFixture(t)
	ledger := &memLedger{}
	s, _ := enrollService(t, f, &capture{}, func(c *sharing.Config) { c.MarketplaceEnabled = true })
	s = s.WithBlockLedger(ledger)
	mod := grantModerator(t, f, "Ledger moderator")
	other := grantModerator(t, f, "Second moderator")
	ownerID := marketOwner(t, f, "ledger-owner")
	spam := marketCourse(t, f, ownerID, "Spam ledger", "Short lessons", 3)
	unsafe := marketCourse(t, f, ownerID, "Unsafe ledger", "Short lessons", 3)
	for _, c := range []string{spam, unsafe} {
		listCourse(t, s, ownerID, c, "tech", "en")
	}
	learner := newLearner(t, f, "Ledger learner")
	spamCopy, err := s.EnrollMarketplace(t.Context(), learner, spam)
	if err != nil {
		t.Fatal(err)
	}
	unsafeCopy, err := s.EnrollMarketplace(t.Context(), learner, unsafe)
	if err != nil {
		t.Fatal(err)
	}
	spamItem := reportedCourse(t, f, s, ownerID, spam, "spam")
	unsafeItem := reportedCourse(t, f, s, ownerID, unsafe, "unsafe")

	// A normal action never writes a block record.
	act(t, s, mod, spamItem.ID, "takedown", "spam")
	if len(ledger.blocks) != 0 {
		t.Fatalf("normal takedown wrote %v", ledger.blocks)
	}
	// If the ledger cannot be written the takedown does not happen at all.
	ledger.failNext = true
	if _, err = s.ModerateItem(t.Context(), mod, unsafeItem.ID, sharing.ActionInput{Action: "takedown", ReasonCode: "unsafe", Reason: "Moderator explanation"}); err == nil {
		t.Fatal("severe takedown committed without a durable ledger record")
	}
	if count(t, f, `SELECT count(*) FROM course_copies WHERE copy_course_id=$1 AND blocked_at IS NOT NULL`, unsafeCopy.CourseID) != 0 ||
		count(t, f, `SELECT count(*) FROM moderation_actions WHERE course_id=$1`, unsafe) != 0 {
		t.Fatal("a failed ledger write left effects behind")
	}
	ledger.failNext = false
	res := act(t, s, mod, unsafeItem.ID, "takedown", "unsafe")
	if len(ledger.blocks) != 1 || ledger.blocks[sharing.LedgerRef(res.ID)] != sharing.LedgerRef(unsafe) {
		t.Fatalf("ledger = %v", ledger.blocks)
	}
	for ref := range ledger.blocks {
		if strings.Contains(ref, unsafe) || strings.Contains(ledger.blocks[ref], unsafe) || strings.Contains(ref, res.ID) {
			t.Fatal("ledger carries a raw id")
		}
	}
	if count(t, f, `SELECT count(*) FROM course_copies WHERE copy_course_id=$1 AND blocked_at IS NOT NULL`, spamCopy.CourseID) != 0 {
		t.Fatal("normal takedown blocked a copy")
	}
	appeal, err := s.FileAppeal(t.Context(), ownerID, res.ID, "The lessons are safe.")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DecideAppeal(t.Context(), other, appeal, "overturn", "The lessons are safe."); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ledger.lifted, []string{sharing.LedgerRef(res.ID)}) {
		t.Fatalf("lifted = %v", ledger.lifted)
	}
	if count(t, f, `SELECT count(*) FROM course_copies WHERE copy_course_id=$1 AND blocked_at IS NULL`, unsafeCopy.CourseID) != 1 {
		t.Fatal("overturn did not unblock the copy")
	}
}

func TestReleaseReadinessReportsEveryGateAndNeverTurnsTheFlagOn(t *testing.T) {
	f := newSharingFixture(t)
	s, _ := enrollService(t, f, &capture{}, nil)
	plain := newLearner(t, f, "Not a moderator")
	mod := grantModerator(t, f, "Release moderator")
	if _, err := s.ReleaseReadiness(t.Context(), plain); !errors.Is(err, sharing.ErrNotModerator) {
		t.Fatalf("non-moderator = %v", err)
	}
	r, err := s.ReleaseReadiness(t.Context(), mod)
	if err != nil {
		t.Fatal(err)
	}
	met := map[string]bool{}
	var keys []string
	for _, c := range r.Criteria {
		met[c.Key] = c.Met
		keys = append(keys, c.Key)
		if c.Detail == "" {
			t.Fatalf("criterion %s has no detail", c.Key)
		}
	}
	want := []string{"approval.cdn_policy", "approval.legal", "caps.enroll", "caps.invite", "caps.publish", "caps.report", "caps.review", "erasure.attribution", "noindex", "retention.assets"}
	sort.Strings(keys)
	if !slices.Equal(keys, want) {
		t.Fatalf("criteria = %v", keys)
	}
	for _, k := range []string{"caps.enroll", "caps.invite", "caps.publish", "caps.report", "caps.review", "erasure.attribution", "noindex", "retention.assets"} {
		if !met[k] {
			t.Fatalf("%s should be met by the defaults", k)
		}
	}
	if met["approval.legal"] || met["approval.cdn_policy"] || r.Ready || r.MarketplaceEnabled {
		t.Fatalf("approvals missing must keep it unready: %+v", r)
	}

	approved, _ := enrollService(t, f, &capture{}, func(c *sharing.Config) {
		c.ReleaseLegalApproved, c.ReleaseCDNPolicyApproved, c.MaxEnrollmentsPerDay = true, true, 0
	})
	r, err = approved.ReleaseReadiness(t.Context(), mod)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range r.Criteria {
		if c.Key == "caps.enroll" && c.Met {
			t.Fatal("a disabled enrollment cap must not count as enforced")
		}
	}
	if r.Ready || r.MarketplaceEnabled {
		t.Fatalf("one unmet cap keeps it unready and the flag stays off: %+v", r)
	}
	full, _ := enrollService(t, f, &capture{}, func(c *sharing.Config) { c.ReleaseLegalApproved, c.ReleaseCDNPolicyApproved = true, true })
	if r, err = full.ReleaseReadiness(t.Context(), mod); err != nil || !r.Ready || r.MarketplaceEnabled {
		t.Fatalf("ready report must not enable the flag: %+v %v", r, err)
	}
}

// rustObjects adapts the application bucket (RustFS) to the sharing object boundaries.
type rustObjects struct{ s3 *backup.S3 }

func (r rustObjects) Put(ctx context.Context, key string, raw []byte) error {
	return r.s3.Put(ctx, key, bytes.NewReader(raw), int64(len(raw)))
}

func (r rustObjects) List(ctx context.Context, prefix string) ([]string, error) {
	objs, err := r.s3.List(ctx, prefix)
	keys := make([]string, 0, len(objs))
	for _, o := range objs {
		keys = append(keys, o.Key)
	}
	sort.Strings(keys)
	return keys, err
}

func (r rustObjects) Delete(ctx context.Context, key string) error { return r.s3.Delete(ctx, key) }

func TestRestoreReappliesSevereBlocksAndResolvesEveryPinAgainstRustFS(t *testing.T) {
	f := backuptest.New(t)
	f.Seed(t)
	app := rustObjects{f.ApplicationStorage()}
	cfg := sharing.DefaultConfig()
	s := sharing.NewService(f.DB.Pool, &capture{}, cfg).WithAgeSource(adult(true)).WithObjects(app, asset).WithAssetStore(app).
		WithBlockLedger(backup.ModerationLedger{Objects: f.Store("writer")})
	s.ConfigureScreening(startEngine(t), fixedRates{})
	mod := grantModerator(t, f, "Drill moderator")
	ownerID := marketOwner(t, f, "drill-owner")
	course := marketCourse(t, f, ownerID, "Drill tides", "Short lessons", 3)
	token := shareAs(t, s, ownerID, course)
	learner := newLearner(t, f, "Drill learner")
	copied, err := s.Enroll(t.Context(), learner, token)
	if err != nil {
		t.Fatal(err)
	}
	prefix := prefixOf(t, f, copied.CourseID)
	if keys, _ := app.List(t.Context(), prefix); len(keys) == 0 {
		t.Fatal("RustFS has no published objects for the copy")
	}

	// The dump is taken before the takedown, so only the ledger can carry the block into a restore.
	f.RunJob(t, "2026-11-12")
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO moderation_queue_items(target_type,target_id,severity) VALUES('course',$1,100)`, course); err != nil {
		t.Fatal(err)
	}
	var item string
	if err = f.DB.Pool.QueryRow(t.Context(), `SELECT id::text FROM moderation_queue_items WHERE target_id=$1 AND resolved_at IS NULL`, course).Scan(&item); err != nil {
		t.Fatal(err)
	}
	res := act(t, s, mod, item, "takedown", "unsafe")
	if res.CopiesBlocked != 1 {
		t.Fatalf("takedown = %+v", res)
	}
	if objs, err := f.Store("restorer").List(t.Context(), "moderation-blocks/v1/"); err != nil || len(objs) != 1 {
		t.Fatalf("ledger objects = %v %v", objs, err)
	}

	options := f.DrillOptions(t, drillNow("2026-11-12"))
	var blocked bool
	options.Inspect = func(ctx context.Context, restored *pgxpool.Pool) error {
		return restored.QueryRow(ctx, `SELECT blocked_at IS NOT NULL FROM course_copies WHERE copy_course_id=$1`, copied.CourseID).Scan(&blocked)
	}
	report, err := backup.RunDrill(t.Context(), options)
	if err != nil || !report.OK {
		t.Fatalf("drill: %v %+v", err, report)
	}
	if !blocked {
		t.Fatal("a restored older dump un-blocked a severely taken-down copy")
	}
	names := map[string]bool{}
	for _, c := range report.Checks {
		names[c.Name] = c.OK
	}
	if !names["copy pins resolve"] || !names["moderation blocks"] {
		t.Fatalf("pin and block checks missing or failed: %+v", report.Checks)
	}

	// Purge on the real object store: only the prefix whose last copy ended goes.
	token2 := shareAs(t, s, ownerID, marketCourse(t, f, ownerID, "Drill ended", "Short lessons", 3))
	ended, err := s.Enroll(t.Context(), newLearner(t, f, "Ended learner"), token2)
	if err != nil {
		t.Fatal(err)
	}
	endedPrefix := prefixOf(t, f, ended.CourseID)
	endCopy(t, f, ended.CourseID)
	base := time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)
	sweep(t, s, base)
	if r := sweep(t, s, base.Add(31*24*time.Hour)); r.Purged < 1 {
		t.Fatalf("purge on RustFS: %+v", r)
	}
	if keys, _ := app.List(t.Context(), endedPrefix); len(keys) != 0 {
		t.Fatalf("RustFS still lists purged keys %v", keys)
	}
	if keys, _ := app.List(t.Context(), prefix); len(keys) == 0 {
		t.Fatal("purge removed objects of a blocked but retained copy")
	}

	// A restore whose pinned object is missing must fail the drill.
	keys, _ := app.List(t.Context(), prefix)
	if err = app.Delete(t.Context(), keys[0]); err != nil {
		t.Fatal(err)
	}
	report, err = backup.RunDrill(t.Context(), f.DrillOptions(t, drillNow("2026-11-12")))
	if err == nil || report.OK {
		t.Fatalf("drill passed with a missing pinned object: %+v", report)
	}
	for _, c := range report.Checks {
		if c.Name == "copy pins resolve" && c.OK {
			t.Fatal("pin resolution did not notice the missing object")
		}
	}
}

func drillNow(day string) time.Time {
	d, _ := time.Parse(time.DateOnly, day)
	return d.Add(6 * time.Hour)
}
