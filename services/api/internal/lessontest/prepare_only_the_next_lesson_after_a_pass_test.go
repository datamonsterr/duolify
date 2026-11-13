package lessontest_test

import (
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/lessontest"
)

func TestFormatCompletedLessonSummary(t *testing.T) {
	// Summary with weak topics
	summary := lessontest.FormatLessonSummary("Intro to Celestial Navigation", []string{"polaris", "latitude"}, 2, 0)
	if !strings.Contains(summary, "Intro to Celestial Navigation") {
		t.Errorf("summary missing title: %s", summary)
	}
	if !strings.Contains(summary, "polaris") || !strings.Contains(summary, "latitude") {
		t.Errorf("summary missing weak topics: %s", summary)
	}

	// Summary with no weak topics (perfect first try)
	cleanSummary := lessontest.FormatLessonSummary("Star Constellations", nil, 3, 0)
	if !strings.Contains(cleanSummary, "Star Constellations") {
		t.Errorf("summary missing title: %s", cleanSummary)
	}
	if strings.Contains(cleanSummary, "Weak topics:") {
		t.Errorf("clean summary should not have weak topics line: %s", cleanSummary)
	}
}

func TestOrderHistoryOldestFirst(t *testing.T) {
	items := []lessontest.LessonHistoryEntry{
		{LessonTitle: "Lesson 3", CompletedUnix: 300, Summary: "Summary 3"},
		{LessonTitle: "Lesson 1", CompletedUnix: 100, Summary: "Summary 1"},
		{LessonTitle: "Lesson 2", CompletedUnix: 200, Summary: "Summary 2"},
	}
	ordered := lessontest.OrderHistoryOldestFirst(items)
	if len(ordered) != 3 {
		t.Fatalf("expected 3 items, got %d", len(ordered))
	}
	if ordered[0] != "Summary 1" || ordered[1] != "Summary 2" || ordered[2] != "Summary 3" {
		t.Errorf("history not ordered oldest first: %v", ordered)
	}
}

func TestExtractWeakTopics(t *testing.T) {
	answers := []lessontest.AnswerRecord{
		{TopicID: "top-coords", Correct: false},
		{TopicID: "top-stars", Correct: true},
		{TopicID: "top-coords", Correct: true},
		{TopicID: "top-planets", Correct: false},
	}
	weak := lessontest.ExtractWeakTopics(answers)
	if len(weak) != 2 {
		t.Fatalf("expected 2 weak topics, got %d: %v", len(weak), weak)
	}
	if weak[0] != "top-coords" || weak[1] != "top-planets" {
		t.Errorf("unexpected weak topics: %v", weak)
	}
}

func TestExtractWeakTopicsNoFailures(t *testing.T) {
	answers := []lessontest.AnswerRecord{
		{TopicID: "top-coords", Correct: true},
		{TopicID: "top-stars", Correct: true},
	}
	weak := lessontest.ExtractWeakTopics(answers)
	if len(weak) != 0 {
		t.Errorf("expected 0 weak topics, got %v", weak)
	}
}
