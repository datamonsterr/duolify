package lessontest_test

import (
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/config"
	"github.com/datamonsterr/duolify/services/api/internal/lessontest"
)

func TestReorderQuestions_Permutation(t *testing.T) {
	questions := []lessontest.PublicQuestion{
		{QuestionID: "q1", Prompt: "Prompt 1"},
		{QuestionID: "q2", Prompt: "Prompt 2"},
		{QuestionID: "q3", Prompt: "Prompt 3"},
	}

	reordered := lessontest.ReorderQuestions(questions)
	if len(reordered) != len(questions) {
		t.Fatalf("expected len %d, got %d", len(questions), len(reordered))
	}
	if reordered[0].QuestionID == questions[0].QuestionID {
		t.Fatalf("expected first question to be reordered, got same %s", reordered[0].QuestionID)
	}

	// Single question stays unchanged
	single := []lessontest.PublicQuestion{{QuestionID: "q1", Prompt: "Prompt 1"}}
	reorderedSingle := lessontest.ReorderQuestions(single)
	if len(reorderedSingle) != 1 || reorderedSingle[0].QuestionID != "q1" {
		t.Fatalf("expected single question unchanged, got %v", reorderedSingle)
	}

	// Empty stays empty
	if len(lessontest.ReorderQuestions(nil)) != 0 {
		t.Fatalf("expected empty reordered for nil")
	}
}

func TestValidateOperationKey(t *testing.T) {
	cases := []struct {
		key   string
		valid bool
	}{
		{"op-123_abc", true},
		{"op123", true},
		{"a", true},
		{"", false},
		{"bad key with spaces", false},
		{"bad$char", false},
		{string(make([]byte, 101)), false}, // > 100 chars
	}

	for _, c := range cases {
		err := lessontest.ValidateOperationKey(c.key)
		if c.valid && err != nil {
			t.Errorf("expected key %q to be valid, got err: %v", c.key, err)
		}
		if !c.valid && err == nil {
			t.Errorf("expected key %q to be invalid, got nil err", c.key)
		}
	}
}

func TestSessionTTLConfig(t *testing.T) {
	// Default TTL should be 7 days (168h)
	envDefault := func(key string) string {
		switch key {
		case "DATABASE_URL":
			return "postgres://user:pass@localhost:5432/db"
		case "REDIS_URL":
			return "redis://localhost:6379"
		case "ENGINE_ADDR":
			return "http://localhost:50051"
		default:
			return ""
		}
	}

	cfgDefault, err := config.Load(envDefault)
	if err != nil {
		t.Fatal(err)
	}
	if cfgDefault.LessonTestSessionTTL != 7*24*time.Hour {
		t.Fatalf("expected default session TTL 168h, got %v", cfgDefault.LessonTestSessionTTL)
	}

	// Custom duration
	envCustom := func(key string) string {
		if key == "LESSON_TEST_SESSION_TTL" {
			return "72h"
		}
		return envDefault(key)
	}
	cfgCustom, err := config.Load(envCustom)
	if err != nil {
		t.Fatal(err)
	}
	if cfgCustom.LessonTestSessionTTL != 72*time.Hour {
		t.Fatalf("expected custom session TTL 72h, got %v", cfgCustom.LessonTestSessionTTL)
	}

	// Days shorthand e.g. "7d"
	envDays := func(key string) string {
		if key == "LESSON_TEST_SESSION_TTL" {
			return "7d"
		}
		return envDefault(key)
	}
	cfgDays, err := config.Load(envDays)
	if err != nil {
		t.Fatal(err)
	}
	if cfgDays.LessonTestSessionTTL != 7*24*time.Hour {
		t.Fatalf("expected 7d session TTL 168h, got %v", cfgDays.LessonTestSessionTTL)
	}
}
