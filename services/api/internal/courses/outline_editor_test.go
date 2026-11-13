package courses_test

import (
	"encoding/json"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"strings"
	"testing"
)

func TestOutlineValidationAndStableIdentity(t *testing.T) {
	valid := courses.Outline{{ID: "unit-1", Title: "Moon", Lessons: []courses.OutlineLesson{{ID: "lesson-1", Title: "Phases", Goal: "Recognize phases"}}}}
	if err := valid.Validate(true); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(valid)
	var restored courses.Outline
	_ = json.Unmarshal(raw, &restored)
	if valid.Hash() != restored.Hash() || restored[0].Lessons[0].ID != "lesson-1" {
		t.Fatal("unstable snapshot")
	}
	restored[0].Title = "Edited"
	if valid.Hash() == restored.Hash() {
		t.Fatal("hash does not bind content")
	}
	cases := []courses.Outline{
		{{ID: "same", Title: "A", Lessons: []courses.OutlineLesson{{ID: "same", Title: "B", Goal: "C"}}}},
		{{ID: "bad id", Title: "A", Lessons: []courses.OutlineLesson{}}},
		{{ID: "u", Title: " ", Lessons: []courses.OutlineLesson{}}},
		{{ID: "u", Title: "A", Lessons: []courses.OutlineLesson{{ID: "l", Title: "B", Goal: " "}}}},
		{{ID: "u", Title: strings.Repeat("a", 501), Lessons: []courses.OutlineLesson{}}},
	}
	for i, v := range cases {
		if v.Validate(false) == nil {
			t.Fatalf("invalid case %d accepted", i)
		}
	}
	if (courses.Outline{}).Validate(false) != nil {
		t.Fatal("empty working draft rejected")
	}
	if (courses.Outline{}).Validate(true) == nil {
		t.Fatal("empty confirmation accepted")
	}
	if (courses.Outline{{ID: "u", Title: "A", Lessons: []courses.OutlineLesson{}}}).Validate(true) == nil {
		t.Fatal("empty unit confirmation accepted")
	}
}
