package lessontest_test

import (
	"testing"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/lessontest"
)

func TestValidateGeneratedLesson(t *testing.T) {
	validLesson := &enginev1.Lesson{
		Slides: []*enginev1.Slide{
			{
				SlideId:    "s1",
				Html:       `<div id="root"><h1>Night Sky</h1></div>`,
				DurationMs: 4000,
				Narration:  "Observing the night sky.",
			},
		},
		Test: []*enginev1.Question{
			{
				QuestionId:  "q1",
				Prompt:      "What is the North Star?",
				Choices:     []string{"Polaris", "Sirius", "Vega"},
				AnswerIndex: 0,
				Explanation: "Polaris points towards the northern celestial pole.",
			},
		},
	}

	if err := lessontest.ValidateGeneratedLesson(validLesson); err != nil {
		t.Fatalf("expected valid lesson to pass validation, got %v", err)
	}

	// Rejects nil lesson or empty slides
	if err := lessontest.ValidateGeneratedLesson(nil); err == nil {
		t.Fatal("expected error for nil lesson")
	}
	if err := lessontest.ValidateGeneratedLesson(&enginev1.Lesson{Test: validLesson.Test}); err == nil {
		t.Fatal("expected error for empty slides")
	}

	// Rejects invalid HTML
	badSlide := &enginev1.Lesson{
		Slides: []*enginev1.Slide{
			{
				SlideId:    "s1",
				Html:       `<div>missing root</div>`,
				DurationMs: 3000,
				Narration:  "Narration",
			},
		},
		Test: validLesson.Test,
	}
	if err := lessontest.ValidateGeneratedLesson(badSlide); err == nil {
		t.Fatal("expected error for slide missing root element")
	}

	// Rejects missing narration
	noNarration := &enginev1.Lesson{
		Slides: []*enginev1.Slide{
			{
				SlideId:    "s1",
				Html:       `<div id="root">Hello</div>`,
				DurationMs: 3000,
				Narration:  "   ",
			},
		},
		Test: validLesson.Test,
	}
	if err := lessontest.ValidateGeneratedLesson(noNarration); err == nil {
		t.Fatal("expected error for missing narration")
	}

	// Rejects empty test
	noTest := &enginev1.Lesson{
		Slides: validLesson.Slides,
		Test:   []*enginev1.Question{},
	}
	if err := lessontest.ValidateGeneratedLesson(noTest); err == nil {
		t.Fatal("expected error for empty test")
	}

	// Rejects choices < 2
	fewChoices := &enginev1.Lesson{
		Slides: validLesson.Slides,
		Test: []*enginev1.Question{
			{
				QuestionId:  "q1",
				Prompt:      "Prompt",
				Choices:     []string{"Only One"},
				AnswerIndex: 0,
				Explanation: "Exp",
			},
		},
	}
	if err := lessontest.ValidateGeneratedLesson(fewChoices); err == nil {
		t.Fatal("expected error for fewer than 2 choices")
	}

	// Rejects answer_index out of bounds
	oobAnswer := &enginev1.Lesson{
		Slides: validLesson.Slides,
		Test: []*enginev1.Question{
			{
				QuestionId:  "q1",
				Prompt:      "Prompt",
				Choices:     []string{"A", "B"},
				AnswerIndex: 2,
				Explanation: "Exp",
			},
		},
	}
	if err := lessontest.ValidateGeneratedLesson(oobAnswer); err == nil {
		t.Fatal("expected error for answer_index out of bounds")
	}
}

func TestTopicMappingTracer(t *testing.T) {
	// Tracer maps each original question to one topic
	questions := []*enginev1.Question{
		{QuestionId: "q1", Prompt: "P1", Choices: []string{"A", "B"}, AnswerIndex: 0},
		{QuestionId: "q2", Prompt: "P2", Choices: []string{"C", "D"}, AnswerIndex: 1},
	}
	topics := lessontest.MapQuestionsToTopics(questions)
	if len(topics) != 2 {
		t.Fatalf("expected 2 topics, got %d", len(topics))
	}
	if topics["q1"] == "" || topics["q2"] == "" || topics["q1"] == topics["q2"] {
		t.Fatalf("expected distinct topic IDs, got %v", topics)
	}
}

func TestReorderChoicesFallback(t *testing.T) {
	// Wrong answer fallback reorders choices while updating answer_index
	choices := []string{"Choice A", "Choice B", "Choice C"}
	answerIndex := 0
	reorderedChoices, newIndex := lessontest.ReorderChoices(choices, answerIndex)

	if len(reorderedChoices) != 3 {
		t.Fatalf("expected 3 choices, got %d", len(reorderedChoices))
	}
	if reorderedChoices[newIndex] != choices[answerIndex] {
		t.Fatalf("newIndex points to %q, expected %q", reorderedChoices[newIndex], choices[answerIndex])
	}
}
