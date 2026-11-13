package lessontest_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
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
				TopicIds:   []string{"top-sky"},
			},
		},
		Test: []*enginev1.Question{
			{
				QuestionId:  "q1",
				TopicId:     "top-sky",
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

	t.Run("Review161RejectDuplicateTopics", func(t *testing.T) {
		duplicate := &enginev1.Lesson{Slides: validLesson.Slides, Test: []*enginev1.Question{validLesson.Test[0], {
			QuestionId: "q-other", TopicId: "top-sky", Prompt: "Another question?", Choices: []string{"A", "B"}, Explanation: "Because A",
		}}}
		if err := lessontest.ValidateGeneratedLesson(duplicate); err == nil {
			t.Fatal("duplicate topics would let one correct answer pass multiple questions")
		}
	})

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
				TopicIds:   []string{"top-sky"},
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
				TopicIds:   []string{"top-sky"},
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
				TopicId:     "top-sky",
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
				TopicId:     "top-sky",
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

func TestLessonTestTracerDisabled_Returns404(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := httpapi.New(httpapi.Dependencies{
		LessonTestTracerEnabled: false,
	}, logger)

	paths := []struct {
		method string
		path   string
	}{
		{"GET", "/v1/lessons/123"},
		{"POST", "/v1/lessons/123/prepare"},
		{"GET", "/v1/lessons/123/test"},
		{"POST", "/v1/lessons/123/test/answer"},
		{"GET", "/v1/lesson-generations/123"},
		{"GET", "/v1/lesson-generations/123/events"},
	}

	for _, p := range paths {
		req := httptest.NewRequestWithContext(t.Context(), p.method, p.path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404 for %s %s when tracer disabled, got %d", p.method, p.path, rec.Code)
		}
	}

	// Verify /v1/features exposes false
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/v1/features", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from GET /v1/features, got %d", rec.Code)
	}
	var feat map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &feat); err != nil {
		t.Fatalf("unmarshal /v1/features: %v", err)
	}
	if feat["lesson_test_tracer_enabled"] != false {
		t.Fatalf("expected lesson_test_tracer_enabled false, got %v", feat["lesson_test_tracer_enabled"])
	}
}

func TestLessonTestTracerEnabled_Features(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := httpapi.New(httpapi.Dependencies{
		LessonTestTracerEnabled: true,
	}, logger)

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/v1/features", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from GET /v1/features, got %d", rec.Code)
	}
	var feat map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &feat); err != nil {
		t.Fatalf("unmarshal /v1/features: %v", err)
	}
	if feat["lesson_test_tracer_enabled"] != true {
		t.Fatalf("expected lesson_test_tracer_enabled true, got %v", feat["lesson_test_tracer_enabled"])
	}
}

func TestLessonTestQuestionExcludesAnswerAndExplanation(t *testing.T) {
	// A Question serialized for test taking must never leak answer_index or explanation
	engineQ := &enginev1.Question{
		QuestionId:  "q1",
		Prompt:      "Which star is brightest?",
		Choices:     []string{"Sirius", "Vega", "Polaris"},
		AnswerIndex: 0,
		Explanation: "Sirius is the brightest star in Earth's night sky.",
	}
	publicQ := lessontest.ToPublicQuestion(engineQ, "top-stars")

	raw, err := json.Marshal(publicQ)
	if err != nil {
		t.Fatalf("marshal public question: %v", err)
	}

	rawStr := string(raw)
	if strings.Contains(rawStr, "answer_index") {
		t.Fatalf("public question leaked answer_index: %s", rawStr)
	}
	if strings.Contains(rawStr, "explanation") {
		t.Fatalf("public question leaked explanation: %s", rawStr)
	}
	if strings.Contains(rawStr, "Sirius is the brightest") {
		t.Fatalf("public question leaked explanation text: %s", rawStr)
	}
}
