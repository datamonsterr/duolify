//go:build integration

package steering

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
)

// copyFixture builds a published copy: L1 unlocked (reused), L2 and L3 locked, L2 still reused.
type copyFixture struct {
	*fixture
	copy string
	unit string
	l    [3]string
}

func newCopyFixture(t *testing.T) *copyFixture {
	t.Helper()
	f := newFixture(t)
	c := &copyFixture{fixture: f}
	if err := f.db.Pool.QueryRow(f.ctx, `WITH c AS (INSERT INTO courses(learner_id,title) VALUES($1::uuid,'Go (shared)') RETURNING id), e AS (INSERT INTO course_enrollments(learner_id,course_id) SELECT $1::uuid,id FROM c), u AS (INSERT INTO course_units(learner_id,course_id,authored_id,position,title) SELECT $1::uuid,id,'u1',0,'Basics' FROM c RETURNING id,course_id) SELECT course_id::text,id::text FROM u`, f.owner).Scan(&c.copy, &c.unit); err != nil {
		t.Fatal(err)
	}
	for i, status := range []string{"current", "locked", "locked"} {
		reused := i < 2
		if err := f.db.Pool.QueryRow(f.ctx, `INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status,reused_from_snapshot) VALUES($1::uuid,$2::uuid,'l'||$3::int,$3::int,'Lesson '||$3::int,'Goal',$4,$5) RETURNING id::text`, f.owner, c.unit, i+1, status, reused).Scan(&c.l[i]); err != nil {
			t.Fatal(err)
		}
		if reused {
			if _, err := f.db.Pool.Exec(f.ctx, `INSERT INTO generated_lessons(lesson_stub_id,course_id,learner_id,title,manifest) VALUES($1::uuid,$2::uuid,$3::uuid,'Lesson','{"source":"published"}')`, c.l[i], c.copy, f.owner); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := f.db.Pool.Exec(f.ctx, `INSERT INTO course_copies(copy_course_id,learner_id,version,content_hash) VALUES($1::uuid,$2::uuid,1,$3)`, c.copy, f.owner, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	return c
}

func (c *copyFixture) marker(t *testing.T) (forked bool, at string) {
	t.Helper()
	var p *string
	if err := c.db.Pool.QueryRow(c.ctx, `SELECT forked_at IS NOT NULL, forked_at_lesson::text FROM course_copies WHERE copy_course_id=$1::uuid`, c.copy).Scan(&forked, &p); err != nil {
		t.Fatal(err)
	}
	if p != nil {
		at = *p
	}
	return
}

func (c *copyFixture) content(t *testing.T, lesson string) (generated, reused bool) {
	t.Helper()
	if err := c.db.Pool.QueryRow(c.ctx, `SELECT EXISTS(SELECT 1 FROM generated_lessons WHERE lesson_stub_id=$1::uuid), (SELECT reused_from_snapshot FROM course_lesson_stubs WHERE id=$1::uuid)`, lesson).Scan(&generated, &reused); err != nil {
		t.Fatal(err)
	}
	return
}

func TestForkAPublishedCopyBeforeSteering(t *testing.T) {
	in := Input{Text: "More examples in Go", Scope: "next_lesson"}

	t.Run("unconfirmed_first_instruction_saves_nothing_and_skips_screening", func(t *testing.T) {
		c := newCopyFixture(t)
		if _, err := c.s.Add(c.ctx, c.owner, c.copy, in); Code(err) != "fork_confirmation_required" {
			t.Fatalf("code %q", Code(err))
		}
		if forked, _ := c.marker(t); forked {
			t.Fatal("declined fork persisted")
		}
		var n int
		_ = c.db.Pool.QueryRow(c.ctx, `SELECT count(*) FROM course_instructions WHERE course_id=$1::uuid`, c.copy).Scan(&n)
		if n != 0 || c.screen.calls != 0 {
			t.Fatalf("instructions=%d screening calls=%d", n, c.screen.calls)
		}
		list, err := c.s.List(c.ctx, c.owner, c.copy)
		if err != nil || list.Fork == nil || !list.Fork.Required || list.Fork.Forked || list.Fork.KeptLessons != 1 {
			t.Fatalf("fork info %+v %v", list.Fork, err)
		}
		if g, r := c.content(t, c.l[1]); !g || !r {
			t.Fatal("snapshot reuse lost after decline")
		}
	})

	t.Run("confirmed_fork_marks_first_locked_lesson_and_stops_reuse_beyond", func(t *testing.T) {
		c := newCopyFixture(t)
		in := in
		in.ConfirmFork = true
		rec, err := c.s.Add(c.ctx, c.owner, c.copy, in)
		if err != nil || rec.Status != "active" {
			t.Fatalf("add %+v %v", rec, err)
		}
		if forked, at := c.marker(t); !forked || at != c.l[1] {
			t.Fatalf("marker %v %q want %q", forked, at, c.l[1])
		}
		if g, r := c.content(t, c.l[0]); !g || !r {
			t.Fatal("unlocked earlier lesson changed")
		}
		if g, r := c.content(t, c.l[1]); g || r {
			t.Fatalf("reuse continues past the marker: generated=%v reused=%v", g, r)
		}
		if c.events.count("course_forked_from_published") != 1 {
			t.Fatal("fork event count")
		}
		// Later adds need no confirmation and never move the marker.
		if _, err = c.s.Add(c.ctx, c.owner, c.copy, Input{Text: "Shorter lessons", Scope: "rest_of_course"}); err != nil {
			t.Fatal(err)
		}
		if _, at := c.marker(t); at != c.l[1] || c.events.count("course_forked_from_published") != 1 {
			t.Fatal("marker moved or fork re-announced")
		}
		// Deleting every instruction does not rejoin the published course.
		list, _ := c.s.List(c.ctx, c.owner, c.copy)
		for _, r := range list.Instructions {
			if err = c.s.Delete(c.ctx, c.owner, c.copy, r.ID); err != nil {
				t.Fatal(err)
			}
		}
		list, _ = c.s.List(c.ctx, c.owner, c.copy)
		if list.Fork == nil || !list.Fork.Forked || list.Fork.Required {
			t.Fatalf("fork undone %+v", list.Fork)
		}
		if forked, _ := c.marker(t); !forked {
			t.Fatal("deleting instructions rejoined")
		}
	})

	t.Run("an_already_unlocked_lesson_stays_and_the_marker_moves_on", func(t *testing.T) {
		c := newCopyFixture(t)
		if _, err := c.db.Pool.Exec(c.ctx, `UPDATE course_lesson_stubs SET status='current' WHERE id=$1::uuid`, c.l[1]); err != nil {
			t.Fatal(err)
		}
		in := in
		in.ConfirmFork = true
		if _, err := c.s.Add(c.ctx, c.owner, c.copy, in); err != nil {
			t.Fatal(err)
		}
		if _, at := c.marker(t); at != c.l[2] {
			t.Fatalf("marker %q want %q", at, c.l[2])
		}
		if g, r := c.content(t, c.l[1]); !g || !r {
			t.Fatal("unlocked reused lesson was removed")
		}
	})

	t.Run("fully_unlocked_copy_forks_without_a_lesson_marker", func(t *testing.T) {
		c := newCopyFixture(t)
		if _, err := c.db.Pool.Exec(c.ctx, `UPDATE course_lesson_stubs SET status='current' WHERE unit_id=$1::uuid`, c.unit); err != nil {
			t.Fatal(err)
		}
		in := in
		in.ConfirmFork = true
		if _, err := c.s.Add(c.ctx, c.owner, c.copy, in); err != nil {
			t.Fatal(err)
		}
		if forked, at := c.marker(t); !forked || at != "" {
			t.Fatalf("marker %v %q", forked, at)
		}
	})

	t.Run("a_rejected_add_rolls_the_fork_back", func(t *testing.T) {
		c := newCopyFixture(t)
		if _, err := c.db.Pool.Exec(c.ctx, `UPDATE steering_config SET max_active=1 WHERE singleton`); err != nil {
			t.Fatal(err)
		}
		if _, err := c.db.Pool.Exec(c.ctx, `INSERT INTO course_instructions(learner_id,course_id,text,scope,screening_verdict,screening_version) VALUES($1::uuid,$2::uuid,'Existing','rest_of_course','allow','steering-v1')`, c.owner, c.copy); err != nil {
			t.Fatal(err)
		}
		in := in
		in.ConfirmFork = true
		if _, err := c.s.Add(c.ctx, c.owner, c.copy, in); Code(err) != "active_limit" {
			t.Fatalf("code %q", Code(err))
		}
		if forked, _ := c.marker(t); forked {
			t.Fatal("rejected add left a fork")
		}
		if g, r := c.content(t, c.l[1]); !g || !r {
			t.Fatal("reuse removed by a rolled back fork")
		}
	})

	t.Run("concurrent_confirmed_adds_fork_once", func(t *testing.T) {
		c := newCopyFixture(t)
		var wg sync.WaitGroup
		errs := make([]error, 4)
		for i := range errs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, errs[i] = c.s.Add(c.ctx, c.owner, c.copy, Input{Text: "Example " + strconv.Itoa(i), Scope: "rest_of_course", ConfirmFork: true})
			}()
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		if c.events.count("course_forked_from_published") != 1 {
			t.Fatalf("fork events %d", c.events.count("course_forked_from_published"))
		}
		if _, at := c.marker(t); at != c.l[1] {
			t.Fatalf("marker %q", at)
		}
	})

	t.Run("fork_waits_for_an_unlock_holding_the_copy_lock", func(t *testing.T) {
		c := newCopyFixture(t)
		tx, err := c.db.Pool.Begin(c.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(c.ctx) }()
		// What lessontest takes when it unlocks the next lesson of a copy.
		if err = LockCopyTx(c.ctx, tx, c.l[0]); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() {
			_, err := c.s.Add(c.ctx, c.owner, c.copy, Input{Text: "Go", Scope: "next_lesson", ConfirmFork: true})
			done <- err
		}()
		select {
		case err = <-done:
			t.Fatalf("fork did not wait for the unlock: %v", err)
		default:
		}
		// The unlock opens L2 (still has reused content) and commits.
		if _, err = tx.Exec(c.ctx, `UPDATE course_lesson_stubs SET status='current' WHERE id=$1::uuid`, c.l[1]); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(c.ctx); err != nil {
			t.Fatal(err)
		}
		if err = <-done; err != nil {
			t.Fatal(err)
		}
		if _, at := c.marker(t); at != c.l[2] {
			t.Fatalf("marker %q want the lesson after the unlocked one", at)
		}
		if g, _ := c.content(t, c.l[1]); !g {
			t.Fatal("unlocked lesson lost its content")
		}
	})

	t.Run("another_learner_cannot_fork_or_read", func(t *testing.T) {
		c := newCopyFixture(t)
		other := "b3333333-3333-4333-8333-333333333333"
		if _, err := c.db.Pool.Exec(c.ctx, `INSERT INTO learners(id,display_name) VALUES($1::uuid,'Other')`, other); err != nil {
			t.Fatal(err)
		}
		if _, err := c.s.Add(c.ctx, other, c.copy, Input{Text: "Go", Scope: "next_lesson", ConfirmFork: true}); Code(err) != "not_found" {
			t.Fatalf("code %q", Code(err))
		}
		if forked, _ := c.marker(t); forked {
			t.Fatal("foreign fork")
		}
	})
}

type priceDouble struct {
	credits int64
	err     error
}

func (p priceDouble) LessonCredits(context.Context) (int64, error) { return p.credits, p.err }

func TestForkWarningCarriesTheActiveLessonPrice(t *testing.T) {
	c := newCopyFixture(t)
	listed := func() *ForkInfo {
		t.Helper()
		l, err := c.s.List(c.ctx, c.owner, c.copy)
		if err != nil || l.Fork == nil {
			t.Fatalf("list %+v %v", l.Fork, err)
		}
		return l.Fork
	}
	// No price source configured: the field is absent so the client uses its fallback wording.
	if got := listed().LessonCredits; got != 0 {
		t.Fatalf("price without a source: %d", got)
	}
	c.s = New(Options{Pool: c.db.Pool, Engine: c.screen, Events: c.events, Price: priceDouble{err: errors.New("no active config")}})
	if got := listed().LessonCredits; got != 0 {
		t.Fatalf("price on error: %d", got)
	}
	c.s = New(Options{Pool: c.db.Pool, Engine: c.screen, Events: c.events, Price: priceDouble{credits: 190}})
	if got := listed().LessonCredits; got != 190 {
		t.Fatalf("price %d", got)
	}
	// The real source equals what a lesson_content quote with narration would show.
	quotes := pricing.NewQuotes(c.db.Pool)
	row, err := db.New(c.db.Pool).ActiveQuoteConfig(c.ctx)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := pricing.Parse(row.Config)
	if err != nil {
		t.Fatal(err)
	}
	var limits map[string]json.RawMessage
	var lesson map[string]int64
	if err = json.Unmarshal(cfg.Limits, &limits); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(limits["lesson_content"], &lesson)
	chars := lesson["narration_characters"]
	q, err := quotes.Create(c.ctx, c.owner, pricing.QuoteInput{Action: "lesson_content", NarrationCharacters: chars})
	if err != nil {
		t.Fatal(err)
	}
	got, err := quotes.LessonCredits(c.ctx)
	if err != nil || got != q.ContentCeiling+q.AudioCeiling || got <= 0 {
		t.Fatalf("lesson credits %d (%v), quote %d+%d", got, err, q.ContentCeiling, q.AudioCeiling)
	}
	c.s = New(Options{Pool: c.db.Pool, Engine: c.screen, Events: c.events, Price: quotes})
	if listed().LessonCredits != got {
		t.Fatal("list does not carry the quote price")
	}
}
