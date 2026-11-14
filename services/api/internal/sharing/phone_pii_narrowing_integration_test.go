//go:build integration

package sharing_test

import (
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/sharing"
)

// Issue #199: a UUID/year/ISBN in a course title must not trip the phone-PII rule, while a real
// phone number still fails closed to needs_review.
func TestPhoneRuleNarrowingInPublicationDraft(t *testing.T) {
	f := newSharingFixture(t)
	ctx := t.Context()
	// Minimal course: the full backup seed is not needed to exercise the text scanner.
	for _, q := range []string{
		`INSERT INTO learners(id,display_name) VALUES('` + owner + `','Owner')`,
		`INSERT INTO learner_profiles(learner_id,locale,time_zone,analytics_opt_out) VALUES('` + owner + `','en-US','UTC',true)`,
		`INSERT INTO courses(id,learner_id,title,description) VALUES('` + course + `','` + owner + `','Seed','')`,
		`INSERT INTO course_units(id,learner_id,course_id,authored_id,position,title) VALUES('f2222222-2222-4222-8222-222222222222','` + owner + `','` + course + `','u1',0,'Unit')`,
		`INSERT INTO course_lesson_stubs(id,learner_id,unit_id,authored_id,position,title,goal,status) VALUES('` + stub + `','` + owner + `','f2222222-2222-4222-8222-222222222222','l1',0,'Lesson','Goal','ready_to_generate')`,
	} {
		if _, err := f.DB.Pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	addSlide(t, f, 0, "<p>Plain slide</p>")
	s := newService(t, f, startEngine(t), &capture{})

	build := func(title string) sharing.Draft {
		t.Helper()
		if _, err := f.DB.Pool.Exec(ctx, `UPDATE courses SET title=$1 WHERE id=$2`, title, course); err != nil {
			t.Fatal(err)
		}
		d, err := s.BuildDraft(ctx, owner, course)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}

	for _, title := range []string{
		"Course 550e8400-e29b-41d4-a716-446655440000",
		"Fixture 3412-5678-9012 2026-11-14 v1.2.3 ISBN 978-3-16-148410-0",
	} {
		if d := build(title); d.Verdict != sharing.VerdictPass || len(d.Findings) != 0 {
			t.Fatalf("%q: verdict=%s findings=%+v", title, d.Verdict, d.Findings)
		}
	}
	d := build("Call +1 415 555 2671")
	if d.Verdict != sharing.VerdictReview || len(d.Findings) != 1 || d.Findings[0].Kind != sharing.KindPersonalData {
		t.Fatalf("phone title: verdict=%s findings=%+v", d.Verdict, d.Findings)
	}
}
