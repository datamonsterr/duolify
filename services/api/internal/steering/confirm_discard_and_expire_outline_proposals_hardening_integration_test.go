//go:build integration

package steering

import (
	"sync"
	"testing"

	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
)

// A model-supplied lesson id that is a real lesson of another course must be treated as a new lesson.
func TestConfirmNeverMovesAnotherCoursesLessonIntegration(t *testing.T) {
	const foreign = "f0f0f0f0-0000-4000-8000-000000000001"
	f, s, p := setupConfirmHarnessWith(t, func(o *pb.CourseOutline) {
		o.Units[0].Lessons = append(o.Units[0].Lessons, &pb.LessonRef{LessonId: foreign, Title: "Hijacked", Goal: "Hijack"})
	})
	var otherUnit string
	if err := f.db.Pool.QueryRow(f.ctx, `WITH cr AS (INSERT INTO course_creations(learner_id,draft_id,revision,quote_id,request,state) VALUES($1::uuid,gen_random_uuid(),1,$2::uuid,'{}','enrolled') RETURNING id), c AS (INSERT INTO courses(learner_id,creation_id,title) SELECT $1::uuid,id,'Other' FROM cr RETURNING id), u AS (INSERT INTO course_units(learner_id,course_id,authored_id,position,title) SELECT $1::uuid,id,'ou',0,'Other unit' FROM c RETURNING id) SELECT id::text FROM u`, f.owner, f.quote(t)).Scan(&otherUnit); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool.Exec(f.ctx, `INSERT INTO course_lesson_stubs(id,learner_id,unit_id,authored_id,position,title,goal,status) VALUES($1::uuid,$2::uuid,$3::uuid,'ol',0,'Mine','My goal','locked')`, foreign, f.owner, otherUnit); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfirmReplan(f.ctx, f.owner, f.course, p.ID); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	var unit, title string
	if err := f.db.Pool.QueryRow(f.ctx, `SELECT unit_id::text,title FROM course_lesson_stubs WHERE id=$1::uuid`, foreign).Scan(&unit, &title); err != nil || unit != otherUnit || title != "Mine" {
		t.Fatalf("other course lesson modified: unit=%s title=%s %v", unit, title, err)
	}
}

func TestConfirmRemovesLockedStubsRenumbersWithoutGapsIntegration(t *testing.T) {
	f, s, p := setupConfirmHarnessWith(t, func(o *pb.CourseOutline) {
		o.Units[0].Lessons = o.Units[0].Lessons[:1]
		o.Units[0].Lessons = append(o.Units[0].Lessons, &pb.LessonRef{LessonId: "brand-new", Title: "Brand new", Goal: "New goal"})
	})
	var protectedAuthored string
	if err := f.db.Pool.QueryRow(f.ctx, `SELECT authored_id FROM course_lesson_stubs WHERE unit_id=$1::uuid AND position=0`, f.unit).Scan(&protectedAuthored); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfirmReplan(f.ctx, f.owner, f.course, p.ID); err != nil {
		t.Fatal(err)
	}
	var n, maxPos, big int
	if err := f.db.Pool.QueryRow(f.ctx, `SELECT count(*),max(position),count(*) FILTER (WHERE position>=500000) FROM course_lesson_stubs WHERE unit_id=$1::uuid`, f.unit).Scan(&n, &maxPos, &big); err != nil {
		t.Fatal(err)
	}
	if n != 2 || maxPos != 1 || big != 0 {
		t.Fatalf("positions not contiguous: n=%d max=%d big=%d", n, maxPos, big)
	}
	var after string
	if err := f.db.Pool.QueryRow(f.ctx, `SELECT authored_id FROM course_lesson_stubs WHERE unit_id=$1::uuid AND position=0`, f.unit).Scan(&after); err != nil || after != protectedAuthored {
		t.Fatalf("protected lesson row rewritten: %q != %q (%v)", after, protectedAuthored, err)
	}
}

// A new unit whose id equals an existing unit's authored_id must not hit the unique index.
func TestConfirmNewUnitIDCollidingWithExistingAuthoredIDIntegration(t *testing.T) {
	f, s, p := setupConfirmHarnessWith(t, func(o *pb.CourseOutline) {
		fresh := &pb.Unit{UnitId: "u", Title: "Fresh unit", Lessons: []*pb.LessonRef{{LessonId: "fresh-1", Title: "Fresh", Goal: "Fresh goal"}}}
		o.Units[0].Lessons[1].Title = "Retitled"
		o.Units = append(o.Units, fresh)
	})
	if _, err := s.ConfirmReplan(f.ctx, f.owner, f.course, p.ID); err != nil {
		t.Fatalf("confirm failed: %v", err)
	}
	var units int
	if err := f.db.Pool.QueryRow(f.ctx, `SELECT count(*) FROM course_units WHERE course_id=$1::uuid`, f.course).Scan(&units); err != nil || units != 2 {
		t.Fatalf("units=%d %v", units, err)
	}
}

// A lesson that already has any generation row must count as protected: removing it would cascade-delete generation records.
func TestConfirmRefusesWhenGenerationAppearedAfterProposalIntegration(t *testing.T) {
	f, s, p := setupConfirmHarness(t)
	if _, err := f.db.Pool.Exec(f.ctx, `INSERT INTO lesson_generations(learner_id,course_id,lesson_id,quote_id,state) SELECT $1::uuid,$2::uuid,id,$3::uuid,'failed' FROM course_lesson_stubs WHERE unit_id=$4::uuid AND position=1`, f.owner, f.course, f.quote(t), f.unit); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfirmReplan(f.ctx, f.owner, f.course, p.ID); Code(err) != "version_conflict" {
		t.Fatalf("want version_conflict, got %v", err)
	}
	var title string
	if err := f.db.Pool.QueryRow(f.ctx, `SELECT title FROM course_lesson_stubs WHERE unit_id=$1::uuid AND position=1`, f.unit).Scan(&title); err != nil || title != "History" {
		t.Fatalf("outline changed: %q %v", title, err)
	}
}

func TestConcurrentConfirmAndDiscardHaveOneWinnerIntegration(t *testing.T) {
	f, s, p := setupConfirmHarness(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var wins, codes = 0, map[string]int{}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(discard bool) {
			defer wg.Done()
			var got ReplanProposal
			var err error
			if discard {
				got, err = s.DiscardReplan(f.ctx, f.owner, f.course, p.ID)
			} else {
				got, err = s.ConfirmReplan(f.ctx, f.owner, f.course, p.ID)
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				codes[Code(err)]++
			} else {
				codes[got.Decision]++
				wins++
			}
		}(i%2 == 0)
	}
	wg.Wait()
	var dec string
	var version int64
	if err := f.db.Pool.QueryRow(f.ctx, `SELECT p.decision,c.outline_version FROM course_replan_proposals p JOIN courses c ON c.id=p.course_id WHERE p.id=$1::uuid`, p.ID).Scan(&dec, &version); err != nil {
		t.Fatal(err)
	}
	if dec == "confirmed" && (version != 2 || f.events.count("course_replan_confirmed") != 1 || f.events.count("course_replan_discarded") != 0 || codes["discarded"] != 0) {
		t.Fatalf("confirm won inconsistently: v=%d codes=%v", version, codes)
	}
	if dec == "discarded" && (version != 1 || f.events.count("course_replan_confirmed") != 0 || f.events.count("course_replan_discarded") != 1 || codes["confirmed"] != 0) {
		t.Fatalf("discard won inconsistently: v=%d codes=%v", version, codes)
	}
	if dec != "confirmed" && dec != "discarded" {
		t.Fatalf("no winner: %q", dec)
	}
}

func TestConfirmRefusesUnforkedPublishedCopyIntegration(t *testing.T) {
	f, s, p := setupConfirmHarness(t)
	if _, err := f.db.Pool.Exec(f.ctx, `INSERT INTO course_copies(copy_course_id,learner_id,version,content_hash) VALUES($1::uuid,$2::uuid,1,$3)`, f.course, f.owner, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfirmReplan(f.ctx, f.owner, f.course, p.ID); Code(err) != "fork_confirmation_required" {
		t.Fatalf("want fork_confirmation_required, got %v", err)
	}
}

func TestConfirmedEventCarriesNewVersionAndCountsOnlyIntegration(t *testing.T) {
	f, s, p := setupConfirmHarness(t)
	if _, err := s.ConfirmReplan(f.ctx, f.owner, f.course, p.ID); err != nil {
		t.Fatal(err)
	}
	f.events.mu.Lock()
	defer f.events.mu.Unlock()
	for _, ev := range f.events.events {
		if ev["event"] == "course_replan_confirmed" {
			if ev["outline_version"] != int64(2) || ev["lessons_added"] != 1 || ev["lessons_changed"] != 1 || ev["lessons_removed"] != 0 {
				t.Fatalf("event: %v", ev)
			}
			if _, leaked := ev["title"]; leaked {
				t.Fatalf("text leaked: %v", ev)
			}
			return
		}
	}
	t.Fatal("no confirmed event")
}
