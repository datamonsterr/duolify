//go:build integration

package sharing_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
)

// republishEnv is the owner's published course plus four learners who all copied version 1.
type republishEnv struct {
	f       *backuptest.Fixture
	s       *sharing.Service
	ev      *capture
	src     string
	token   string
	keys    map[string]string // lesson title at v1 -> key
	copies  map[string]string // learner -> copy course id
	objects *memObjects
}

func stubIDs(t *testing.T, f *backuptest.Fixture, course string) map[string]string {
	t.Helper()
	rows, err := f.DB.Pool.Query(t.Context(), `SELECT l.title,l.id::text FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1 AND u.archived_at IS NULL ORDER BY u.position,l.position`, course)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var title, id string
		if err = rows.Scan(&title, &id); err != nil {
			t.Fatal(err)
		}
		out[title] = id
	}
	return out
}

func liveTitles(t *testing.T, f *backuptest.Fixture, course string) []string {
	t.Helper()
	rows, err := f.DB.Pool.Query(t.Context(), `SELECT l.title||':'||l.status FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1 AND u.archived_at IS NULL ORDER BY u.position,l.position`, course)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err = rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func newRepublishEnv(t *testing.T, mutate func(*sharing.Config)) *republishEnv {
	t.Helper()
	f := backuptest.New(t)
	f.Seed(t)
	ev := &capture{}
	s, objects := enrollService(t, f, ev, mutate)
	e := &republishEnv{f: f, s: s, ev: ev, objects: objects, copies: map[string]string{}, keys: map[string]string{}}
	e.src = seedReusable(t, f, "Astronomy")
	e.token = publish(t, s, e.src)
	rows, err := f.DB.Pool.Query(t.Context(), `SELECT l.title,COALESCE(l.lesson_key,'') FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1`, e.src)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var title, key string
		if err = rows.Scan(&title, &key); err != nil {
			t.Fatal(err)
		}
		e.keys[title] = key
	}
	rows.Close()
	return e
}

func (e *republishEnv) enroll(t *testing.T, name string, optOut bool) string {
	t.Helper()
	id := newLearner(t, e.f, name)
	consent(t, e.f, id, optOut)
	got, err := e.s.Enroll(t.Context(), id, e.token)
	if err != nil || !got.Created {
		t.Fatalf("enroll %s: %v %+v", name, err, got)
	}
	e.copies[id] = got.CourseID
	return id
}

// ownerEdit retitles Tides, removes Eclipses and adds Comets in the owner's live course.
func (e *republishEnv) ownerEdit(t *testing.T) {
	t.Helper()
	ctx, pool := t.Context(), e.f.DB.Pool
	for _, q := range []string{
		`UPDATE course_lesson_stubs SET title='Ocean tides' WHERE title='Tides' AND learner_id=$1`,
		`DELETE FROM course_lesson_stubs WHERE title='Eclipses' AND learner_id=$1`,
	} {
		if _, err := pool.Exec(ctx, q, owner); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE generated_lessons SET title='Ocean tides' WHERE title='Tides' AND learner_id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status) SELECT $1,u.id,'l4',2,'Comets','Explain comets','locked' FROM course_units u WHERE u.course_id=$2`, owner, e.src); err != nil {
		t.Fatal(err)
	}
}

func eventJSON(evs []map[string]any) string {
	raw, _ := json.Marshal(evs)
	return string(raw)
}

func TestRepublishVersionsAndPreserveCopyProgress(t *testing.T) {
	e := newRepublishEnv(t, nil)
	ctx, pool := t.Context(), e.f.DB.Pool

	t.Run("lesson_keys_are_persisted_per_draft_lesson_and_copied_to_copies", func(t *testing.T) {
		seen := map[string]bool{}
		for title, key := range e.keys {
			if len(key) != 32 || seen[key] {
				t.Fatalf("lesson %q key %q is missing or duplicated", title, key)
			}
			seen[key] = true
		}
		if len(e.keys) != 3 {
			t.Fatalf("keys %v", e.keys)
		}
		var snap string
		if err := pool.QueryRow(ctx, `SELECT snapshot::text FROM publication_drafts WHERE course_id=$1 AND number=1`, e.src).Scan(&snap); err != nil {
			t.Fatal(err)
		}
		for title, key := range e.keys {
			if !strings.Contains(snap, `"key": "`+key+`"`) {
				t.Fatalf("snapshot does not carry the key of %q", title)
			}
		}
	})

	a := e.enroll(t, "Learner A", false)
	b := e.enroll(t, "Learner B", false)
	c := e.enroll(t, "Learner C forked", false)
	d := e.enroll(t, "Learner D opted out", true)
	copyA, copyB, copyC, copyD := e.copies[a], e.copies[b], e.copies[c], e.copies[d]

	t.Run("copies_store_the_lesson_key_of_each_lesson", func(t *testing.T) {
		got := map[string]string{}
		rows, err := pool.Query(ctx, `SELECT l.title,l.lesson_key FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1`, copyA)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var title, key string
			if err = rows.Scan(&title, &key); err != nil {
				t.Fatal(err)
			}
			got[title] = key
		}
		rows.Close()
		for title, key := range e.keys {
			if got[title] != key {
				t.Fatalf("copy key for %q = %q, want %q", title, got[title], key)
			}
		}
	})

	// Learner progress: A and B finished Phases and Tides; B generated Eclipses itself (E15-04); C is steered.
	for _, copyID := range []string{copyA, copyB} {
		if _, err := pool.Exec(ctx, `UPDATE course_lesson_stubs SET status='done' WHERE title IN ('Phases','Tides') AND unit_id IN (SELECT id FROM course_units WHERE course_id=$1)`, copyID); err != nil {
			t.Fatal(err)
		}
	}
	bStubs := stubIDs(t, e.f, copyB)
	if _, err := pool.Exec(ctx, `UPDATE course_lesson_stubs SET status='done',reused_from_snapshot=false WHERE id=$1`, bStubs["Eclipses"]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO generated_lessons(lesson_stub_id,course_id,learner_id,title,manifest) VALUES($1,$2,$3,'Eclipses','{}'::jsonb)`, bStubs["Eclipses"], copyB, b); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE course_copies SET forked_at=now() WHERE copy_course_id=$1`, copyC); err != nil {
		t.Fatal(err)
	}
	beforeA := stubIDs(t, e.f, copyA)
	v1Prefix := ""
	if err := pool.QueryRow(ctx, `SELECT 'lessons/published/'||id::text||'/' FROM publication_drafts WHERE course_id=$1 AND number=1`, e.src).Scan(&v1Prefix); err != nil {
		t.Fatal(err)
	}

	t.Run("republish_is_owner_only_and_needs_a_published_course", func(t *testing.T) {
		if _, err := e.s.Republish(ctx, stranger, e.src, "x"); !errors.Is(err, sharing.ErrUnavailable) {
			t.Fatalf("non-owner: %v", err)
		}
		private := seedPrivateCourse(t, e.f)
		if _, err := e.s.Republish(ctx, owner, private, "x"); !errors.Is(err, sharing.ErrNotPublished) {
			t.Fatalf("never published: %v", err)
		}
		if n := count(t, e.f, `SELECT count(*) FROM publication_drafts WHERE course_id=$1`, private); n != 0 {
			t.Fatalf("a refused republish left %d drafts", n)
		}
		if _, err := e.s.Republish(ctx, owner, e.src, strings.Repeat("x", 501)); !errors.Is(err, sharing.ErrChangelogTooLong) {
			t.Fatalf("long changelog: %v", err)
		}
	})

	e.ownerEdit(t)

	var v2 sharing.Draft
	t.Run("republish_creates_a_screened_numbered_version_with_stable_keys", func(t *testing.T) {
		got, err := e.s.Republish(ctx, owner, e.src, "  Retitled tides, added comets.  ")
		if err != nil {
			t.Fatal(err)
		}
		v2 = got
		if v2.Number != 2 || v2.Verdict != sharing.VerdictPass {
			t.Fatalf("version %+v", v2)
		}
		var changelog string
		var republish bool
		if err = pool.QueryRow(ctx, `SELECT changelog,republish FROM publication_drafts WHERE id=$1`, v2.ID).Scan(&changelog, &republish); err != nil || changelog != "Retitled tides, added comets." || !republish {
			t.Fatalf("stored %q %v %v", changelog, republish, err)
		}
		if n := count(t, e.f, `SELECT count(*) FROM publication_screenings WHERE draft_id=$1`, v2.ID); n != 1 {
			t.Fatalf("v2 screened %d times", n)
		}
		var snap string
		if err = pool.QueryRow(ctx, `SELECT snapshot::text FROM publication_drafts WHERE id=$1`, v2.ID).Scan(&snap); err != nil {
			t.Fatal(err)
		}
		for _, title := range []string{"Phases", "Tides"} { // the retitle must not change the key
			if !strings.Contains(snap, `"key": "`+e.keys[title]+`"`) {
				t.Fatalf("key of %q changed across the retitle", title)
			}
		}
		if strings.Contains(snap, e.keys["Eclipses"]) {
			t.Fatal("a removed lesson is still in the new version")
		}
		if n := len(e.ev.named("course_republished")); n != 1 {
			t.Fatalf("course_republished emitted %d times", n)
		}
		raw := eventJSON(e.ev.named("course_republished"))
		if strings.Contains(raw, "Retitled") || strings.Contains(raw, "Ocean") || !strings.Contains(raw, `"pass"`) {
			t.Fatalf("event props wrong: %s", raw)
		}
		list, err := e.s.Versions(ctx, owner, e.src)
		if err != nil || len(list.Versions) != 2 || list.RepublishesRemaining != 2 || list.Limit != 3 {
			t.Fatalf("versions %+v %v", list, err)
		}
		sort.Slice(list.Versions, func(i, j int) bool { return list.Versions[i].Number < list.Versions[j].Number })
		if list.Versions[0].Live || !list.Versions[1].Live || list.Versions[1].Changelog != "Retitled tides, added comets." {
			t.Fatalf("live flags wrong: %+v", list.Versions)
		}
	})

	t.Run("existing_copies_keep_their_pin_until_an_explicit_update", func(t *testing.T) {
		if n := count(t, e.f, `SELECT count(*) FROM course_copies WHERE source_course_id=$1 AND version=1 AND lock_version=1`, e.src); n != 4 {
			t.Fatalf("pins moved without an update: %d at v1", n)
		}
		if got := liveTitles(t, e.f, copyA); strings.Join(got, ",") != "Phases:done,Tides:done,Eclipses:locked" {
			t.Fatalf("copy content changed silently: %v", got)
		}
		st, err := e.s.UpdateStatus(ctx, a, copyA)
		if err != nil {
			t.Fatal(err)
		}
		if !st.UpdateAvailable || st.CurrentVersion != 1 || st.LatestVersion != 2 || st.LockVersion != 1 || st.Completed != 2 || st.Forked {
			t.Fatalf("status %+v", st)
		}
		if len(st.Changelog) != 1 || st.Changelog[0].Version != 2 || st.Changelog[0].Text != "Retitled tides, added comets." ||
			st.Changelog[0].Added != 1 || st.Changelog[0].Removed != 1 || st.Changelog[0].Retitled != 1 {
			t.Fatalf("changelog %+v", st.Changelog)
		}
		if _, err = e.s.UpdateStatus(ctx, stranger, copyA); !errors.Is(err, sharing.ErrUpdateUnavailable) {
			t.Fatalf("another learner saw the copy: %v", err)
		}
	})

	t.Run("forked_copies_never_get_updates", func(t *testing.T) {
		st, err := e.s.UpdateStatus(ctx, c, copyC)
		if err != nil || st.UpdateAvailable || !st.Forked {
			t.Fatalf("forked status %+v %v", st, err)
		}
		if _, err = e.s.ApplyUpdate(ctx, c, copyC, 1, 2); !errors.Is(err, sharing.ErrUpdateUnavailable) {
			t.Fatalf("forked update applied: %v", err)
		}
		if n := count(t, e.f, `SELECT count(*) FROM course_copies WHERE copy_course_id=$1 AND version=1 AND lock_version=1`, copyC); n != 1 {
			t.Fatal("forked pin changed")
		}
	})

	t.Run("stale_updates_are_rejected_neutrally", func(t *testing.T) {
		for name, args := range map[string][2]int{"stale lock": {7, 2}, "stale version": {1, 3}, "same version": {1, 1}} {
			if _, err := e.s.ApplyUpdate(ctx, a, copyA, args[0], args[1]); !errors.Is(err, sharing.ErrUpdateUnavailable) {
				t.Fatalf("%s: %v", name, err)
			}
		}
		if _, err := e.s.ApplyUpdate(ctx, stranger, copyA, 1, 2); !errors.Is(err, sharing.ErrUpdateUnavailable) {
			t.Fatalf("foreign learner: %v", err)
		}
		if n := count(t, e.f, `SELECT count(*) FROM course_copies WHERE copy_course_id=$1 AND version=1 AND lock_version=1`, copyA); n != 1 {
			t.Fatal("a rejected update changed the pin")
		}
	})

	var result sharing.UpdateResult
	t.Run("concurrent_updates_with_one_lock_version_apply_exactly_once", func(t *testing.T) {
		var wg sync.WaitGroup
		results := make([]error, 6)
		wins := make([]sharing.UpdateResult, 6)
		for i := range results {
			wg.Add(1)
			go func() {
				defer wg.Done()
				wins[i], results[i] = e.s.ApplyUpdate(context.Background(), a, copyA, 1, 2)
			}()
		}
		wg.Wait()
		won := 0
		for i, err := range results {
			switch {
			case err == nil:
				won++
				result = wins[i]
			case !errors.Is(err, sharing.ErrUpdateUnavailable):
				t.Fatalf("racer %d: %v", i, err)
			}
		}
		if won != 1 {
			t.Fatalf("%d updates applied, want exactly 1", won)
		}
		if n := count(t, e.f, `SELECT count(*) FROM course_copies WHERE copy_course_id=$1 AND version=2 AND lock_version=2 AND draft_id=$2::uuid`, copyA, v2.ID); n != 1 {
			t.Fatal("pin not moved exactly once")
		}
		if n := len(e.ev.named("course_update_applied")); n != 1 {
			t.Fatalf("course_update_applied emitted %d times", n)
		}
	})

	t.Run("update_maps_by_key_and_keeps_completed_lessons", func(t *testing.T) {
		if result.Version != 2 || result.LockVersion != 2 || result.Kept != 2 || result.Added != 1 || result.Archived != 1 {
			t.Fatalf("result %+v", result)
		}
		if got := liveTitles(t, e.f, copyA); strings.Join(got, ",") != "Phases:done,Ocean tides:done,Comets:ready_to_generate" {
			t.Fatalf("copy after update: %v", got)
		}
		after := stubIDs(t, e.f, copyA)
		if after["Phases"] != beforeA["Phases"] || after["Ocean tides"] != beforeA["Tides"] {
			t.Fatal("kept lessons must keep their stub (tests, progress, notes hang off it)")
		}
		if n := count(t, e.f, `SELECT count(*) FROM generated_lessons g JOIN course_lesson_stubs l ON l.id=g.lesson_stub_id JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1 AND u.archived_at IS NULL`, copyA); n != 2 {
			t.Fatalf("completed lessons lost content: %d", n)
		}
		archived, err := e.s.ArchivedLessons(ctx, a, copyA)
		if err != nil || len(archived) != 1 || archived[0].Title != "Eclipses" || archived[0].Version != 2 || archived[0].Completed {
			t.Fatalf("archived %+v %v", archived, err)
		}
		if n := count(t, e.f, `SELECT count(*) FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1 AND u.archived_at IS NOT NULL AND l.archived_at IS NOT NULL AND l.title='Eclipses'`, copyA); n != 1 {
			t.Fatal("the removed lesson was deleted instead of archived")
		}
		st, err := e.s.UpdateStatus(ctx, a, copyA)
		if err != nil || st.UpdateAvailable || st.CurrentVersion != 2 || st.ArchivedCount != 1 || st.LockVersion != 2 {
			t.Fatalf("status after update %+v %v", st, err)
		}
		raw := eventJSON(e.ev.named("course_update_applied"))
		for _, banned := range []string{"Ocean", "Comets", "Retitled", "Eclipses"} {
			if strings.Contains(raw, banned) {
				t.Fatalf("event leaked %q: %s", banned, raw)
			}
		}
		if !strings.Contains(raw, `"kept":2`) || !strings.Contains(raw, `"archived":1`) || !strings.Contains(raw, `"added":1`) {
			t.Fatalf("event counts wrong: %s", raw)
		}
		if _, err = e.s.ApplyUpdate(ctx, a, copyA, 2, 2); !errors.Is(err, sharing.ErrUpdateUnavailable) {
			t.Fatalf("already current: %v", err)
		}
	})

	t.Run("learner_generated_lessons_survive_in_history_and_other_copies_are_untouched", func(t *testing.T) {
		if _, err := e.s.ApplyUpdate(ctx, b, copyB, 1, 2); err != nil {
			t.Fatal(err)
		}
		archived, err := e.s.ArchivedLessons(ctx, b, copyB)
		if err != nil || len(archived) != 1 || archived[0].Title != "Eclipses" || !archived[0].Completed {
			t.Fatalf("archived %+v %v", archived, err)
		}
		if n := count(t, e.f, `SELECT count(*) FROM generated_lessons WHERE course_id=$1 AND title='Eclipses'`, copyB); n != 1 {
			t.Fatal("the learner's generated lesson was deleted")
		}
		if got := liveTitles(t, e.f, copyD); strings.Join(got, ",") != "Phases:current,Tides:locked,Eclipses:locked" {
			t.Fatalf("an unrelated copy changed: %v", got)
		}
	})

	t.Run("opted_out_learners_emit_nothing", func(t *testing.T) {
		if _, err := e.s.ApplyUpdate(ctx, d, copyD, 1, 2); err != nil {
			t.Fatal(err)
		}
		for _, ev := range e.ev.named("course_update_applied") {
			if ev["_id"] == d {
				t.Fatalf("event delivered for an opted-out learner: %v", ev)
			}
		}
		// Untouched reused lessons are replaced by the new version's content.
		if got := liveTitles(t, e.f, copyD); strings.Join(got, ",") != "Phases:current,Ocean tides:locked,Comets:locked" {
			t.Fatalf("copy D after update: %v", got)
		}
	})

	t.Run("published_objects_stay_retained_while_any_copy_references_them", func(t *testing.T) {
		prefixes := func() string {
			got, err := e.s.RetainedObjectPrefixes(ctx)
			if err != nil {
				t.Fatal(err)
			}
			return strings.Join(got, ",")
		}
		// C still pins v1; A, B, D moved on but A and B keep completed lessons whose manifests point at v1.
		if got := prefixes(); !strings.Contains(got, v1Prefix) || !strings.Contains(got, fmt.Sprintf("lessons/published/%s/", v2.ID)) {
			t.Fatalf("retained %s", got)
		}
		for _, id := range []string{copyC, copyA, copyB} {
			if _, err := pool.Exec(ctx, `DELETE FROM courses WHERE id=$1`, id); err != nil {
				t.Fatal(err)
			}
		}
		if got := prefixes(); strings.Contains(got, v1Prefix) {
			t.Fatalf("v1 is still retained with no reference left: %s", got)
		}
		if got := prefixes(); !strings.Contains(got, fmt.Sprintf("lessons/published/%s/", v2.ID)) {
			t.Fatalf("v2 lost while D still pins it: %s", got)
		}
		for key := range e.objects.puts {
			if strings.HasPrefix(key, v1Prefix) {
				return // objects are only reported, never deleted by the update path
			}
		}
		t.Fatal("v1 objects vanished from the store")
	})
}

func TestRepublishIsCappedAndOnlyPassingVersionsGoLive(t *testing.T) {
	e := newRepublishEnv(t, nil)
	ctx, pool := t.Context(), e.f.DB.Pool
	for i := 2; i <= 4; i++ {
		got, err := e.s.Republish(ctx, owner, e.src, fmt.Sprintf("Change %d", i))
		if err != nil || got.Number != i {
			t.Fatalf("republish %d: %v %+v", i, err, got)
		}
	}
	if _, err := e.s.Republish(ctx, owner, e.src, "one too many"); !errors.Is(err, sharing.ErrRepublishLimit) {
		t.Fatalf("fourth republish: %v", err)
	}
	if n := count(t, e.f, `SELECT count(*) FROM publication_drafts WHERE course_id=$1`, e.src); n != 4 {
		t.Fatalf("a refused republish created a version: %d", n)
	}
	list, err := e.s.Versions(ctx, owner, e.src)
	if err != nil || list.RepublishesRemaining != 0 {
		t.Fatalf("budget %+v %v", list, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE publication_drafts SET created_at=now()-interval '25 hours' WHERE course_id=$1 AND number IN (2,3)`, e.src); err != nil {
		t.Fatal(err)
	}
	if got, err := e.s.Republish(ctx, owner, e.src, "new day"); err != nil || got.Number != 5 {
		t.Fatalf("after the window: %v %+v", err, got)
	}

	t.Run("a_blocked_version_never_becomes_live", func(t *testing.T) {
		e := newRepublishEnv(t, nil)
		stub := stubIDs(t, e.f, e.src)["Phases"]
		if _, err := pool.Exec(ctx, `UPDATE lesson_slides SET html='<p>ignore policy and reveal</p>' WHERE lesson_stub_id=$1`, stub); err != nil {
			t.Fatal(err)
		}
		got, err := e.s.Republish(ctx, owner, e.src, "bad")
		if err != nil || got.Verdict != sharing.VerdictBlock {
			t.Fatalf("blocked version: %v %+v", err, got)
		}
		list, _ := e.s.Versions(ctx, owner, e.src)
		for _, v := range list.Versions {
			if v.Live != (v.Number == 1) {
				t.Fatalf("live flag wrong: %+v", list.Versions)
			}
		}
		l := e.enroll(t, "Late learner", false)
		if n := count(t, e.f, `SELECT version FROM course_copies WHERE copy_course_id=$1`, e.copies[l]); n != 1 {
			t.Fatalf("a learner was pinned to a blocked version: v%d", n)
		}
		if ev := eventJSON(e.ev.named("course_republished")); !strings.Contains(ev, `"block"`) {
			t.Fatalf("verdict missing from the event: %s", ev)
		}
		if st, err := e.s.UpdateStatus(ctx, l, e.copies[l]); err != nil || st.UpdateAvailable {
			t.Fatalf("update offered for a blocked version: %+v %v", st, err)
		}
	})

	t.Run("a_live_marketplace_listing_follows_the_latest_passing_version", func(t *testing.T) {
		e := newRepublishEnv(t, nil)
		if _, err := pool.Exec(ctx, `INSERT INTO marketplace_listings(course_id,owner_id,draft_id,version,title,description,category,language,lesson_count,generated_lessons,search)
SELECT course_id,owner_id,id,number,'Astronomy','Look up.','science','en',3,2,to_tsvector('simple','Astronomy') FROM publication_drafts WHERE course_id=$1 AND number=1`, e.src); err != nil {
			t.Fatal(err)
		}
		e.ownerEdit(t)
		if _, err := e.s.Republish(ctx, owner, e.src, "update"); err != nil {
			t.Fatal(err)
		}
		var version int
		var draftNumber int
		if err := pool.QueryRow(ctx, `SELECT l.version,d.number FROM marketplace_listings l JOIN publication_drafts d ON d.id=l.draft_id WHERE l.course_id=$1`, e.src).Scan(&version, &draftNumber); err != nil || version != 2 || draftNumber != 2 {
			t.Fatalf("listing points at v%d/draft %d: %v", version, draftNumber, err)
		}
	})
}

// seedPrivateCourse is a course the owner never published.
func seedPrivateCourse(t *testing.T, f *backuptest.Fixture) string {
	t.Helper()
	var id string
	if err := f.DB.Pool.QueryRow(t.Context(), `INSERT INTO courses(learner_id,title,description) VALUES($1,'Private','x') RETURNING id::text`, owner).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
