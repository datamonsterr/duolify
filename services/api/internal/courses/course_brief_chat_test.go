package courses_test

import (
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"testing"
)

func TestBriefValidationAndCanonicalHash(t *testing.T) {
	b := courses.Brief{Topic: "Astronomy", IncludeTopics: "Moon", SkipTopics: "History", Maths: "Light maths"}
	if err := b.Validate(true); err != nil {
		t.Fatal(err)
	}
	if b.Hash() == (courses.Brief{Topic: "Astronomy", IncludeTopics: "History", SkipTopics: "Moon", Maths: "Light maths"}).Hash() {
		t.Fatal("include/skip conflated")
	}
	copyOfBrief := b
	if b.Hash() != copyOfBrief.Hash() {
		t.Fatal("unstable revision hash")
	}
	if err := (courses.Brief{}).Validate(true); err == nil {
		t.Fatal("empty confirmed topic accepted")
	}
	if err := (courses.Brief{Topic: string(make([]byte, 9000))}).Validate(false); err == nil {
		t.Fatal("oversized text accepted")
	}
}
