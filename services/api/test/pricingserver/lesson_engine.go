package main

import (
	"context"
	"errors"
	"strings"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
)

// failingLessonMarker in a Lesson goal makes every provider fail, like an outage (E15-04).
const failingLessonMarker = "e15-generation-down"

// fixtureLessonEngine is an offline stand-in for the Engine's GenerateLesson: no provider call,
// deterministic content, usage inside the quote's route so settlement succeeds.
type fixtureLessonEngine struct{}

func (fixtureLessonEngine) GenerateLesson(_ context.Context, r *enginev1.GenerateLessonRequest) (*enginev1.Lesson, []*enginev1.Usage, error) {
	if strings.Contains(r.GetLesson().GetGoal(), failingLessonMarker) {
		return nil, nil, errors.New("all providers failed")
	}
	provider, model := "mock", "claude-sonnet-5-5"
	if l := r.GetLimits(); l.GetProvider() != "" {
		provider, model = l.GetProvider(), l.GetModel()
	}
	lesson := &enginev1.Lesson{
		Slides: []*enginev1.Slide{{SlideId: "g1", Html: `<div id="root"><h1>Eclipses</h1><p>Shadows line up.</p></div>`, DurationMs: 3000, Narration: "Shadows line up.", TopicIds: []string{"eclipse"}}},
		Test:   []*enginev1.Question{{QuestionId: "gq1", TopicId: "eclipse", Prompt: "What lines up?", Choices: []string{"Shadows", "Clouds"}, AnswerIndex: 0, Explanation: "Shadows line up."}},
		VariantPool: []*enginev1.TopicVariants{{TopicId: "eclipse", Variants: []*enginev1.Question{
			{QuestionId: "gv1", TopicId: "eclipse", VariantOf: "gq1", Prompt: "Alignment of?", Choices: []string{"Shadows", "Wind"}, AnswerIndex: 0, Explanation: "Shadows line up."},
			{QuestionId: "gv2", TopicId: "eclipse", VariantOf: "gq1", Prompt: "Eclipses need?", Choices: []string{"Alignment", "Rain"}, AnswerIndex: 0, Explanation: "Bodies align."},
		}}},
	}
	for _, in := range r.Instructions {
		if in.Text == "More examples in Go" {
			lesson.Slides[0].Narration = "Go example: func eclipse() returns aligned shadows."
			lesson.Slides[0].Html = `<div id="root"><h1>Go example</h1><p>func eclipse() returns aligned shadows.</p></div>`
		}
	}
	return lesson, []*enginev1.Usage{{Provider: provider, Model: model, Attempt: 1, Delivered: true, InputTokens: 500, OutputTokens: 200, LatencyMs: 40, Component: "content"}}, nil
}
