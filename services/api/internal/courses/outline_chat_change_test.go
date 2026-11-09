package courses_test

import (
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"testing"
)

func TestOutlineChangeTypedOperationsPreserveManualEdits(t *testing.T) {
	base := courses.Outline{{ID: "u1", Title: "Manually authored", Lessons: []courses.OutlineLesson{{ID: "l1", Title: "Keep this", Goal: "Keep goal"}, {ID: "l2", Title: "Remove", Goal: "Old"}}}, {ID: "u2", Title: "Second", Lessons: []courses.OutlineLesson{}}}
	changes := []courses.OutlineOperation{
		{Kind: "retitled", ID: "u2", Title: "Renamed"},
		{Kind: "removed", ID: "l2"},
		{Kind: "added", ID: "l3", ParentID: "u1", Position: 1, Title: "New", Goal: "New goal"},
		{Kind: "moved", ID: "u2", Position: 0},
	}
	result, err := courses.ApplyOutlineOperations(base, changes)
	if err != nil {
		t.Fatal(err)
	}
	if result[0].ID != "u2" || result[0].Title != "Renamed" || result[1].Title != base[0].Title || result[1].Lessons[0] != base[0].Lessons[0] || len(result[1].Lessons) != 2 {
		t.Fatalf("lost manual edits: %#v", result)
	}
	if base[1].Title != "Second" || len(base[0].Lessons) != 2 {
		t.Fatal("mutated immutable base")
	}
	for _, op := range []courses.OutlineOperation{
		{Kind: "retitled", ID: "unknown", Title: "Bad"}, {Kind: "added", ID: "l1", ParentID: "u1", Title: "Duplicate", Goal: "Bad"},
		{Kind: "moved", ID: "l1", ParentID: "missing", Position: 0}, {Kind: "moved", ID: "u1", Position: 99},
		{Kind: "retitled", ID: "u1", Title: " "}, {Kind: "removed", ID: "unknown"}, {Kind: "replace", ID: "u1"},
		{Kind: "removed", ID: "u1", Title: "unexpected"},
	} {
		if _, err := courses.ApplyOutlineOperations(base, []courses.OutlineOperation{op}); err == nil {
			t.Fatalf("accepted invalid operation: %+v", op)
		}
	}
	if _, err := courses.ApplyOutlineOperations(base, nil); err == nil {
		t.Fatal("empty proposal accepted")
	}
	if _, err := courses.ApplyOutlineOperations(base, []courses.OutlineOperation{{Kind: "removed", ID: "l1"}, {Kind: "added", ID: "l1", ParentID: "u1", Title: "reused", Goal: "bad"}}); err == nil {
		t.Fatal("reused stable ID")
	}
}
