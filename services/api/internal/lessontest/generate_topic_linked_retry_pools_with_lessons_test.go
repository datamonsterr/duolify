package lessontest_test

import (
	"testing"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/lessontest"
)

func TestValidateGeneratedLessonWithTopicLinkedPools(t *testing.T) {
	validSlide := &enginev1.Slide{
		SlideId:    "s1",
		Html:       `<div id="root"><h1>Night Sky</h1><p>Stars are luminous balls of gas.</p></div>`,
		DurationMs: 3000,
		Narration:  "Stars are luminous spheres of plasma.",
		TopicIds:   []string{"top-stars"},
	}

	validQuestion := &enginev1.Question{
		QuestionId:  "q1",
		TopicId:     "top-stars",
		Prompt:      "What is a star?",
		Choices:     []string{"Luminous plasma", "Reflected rock", "Space dust"},
		AnswerIndex: 0,
		Explanation: "A star is an astronomical object comprising a luminous spheroid of plasma.",
	}

	validVariant1 := &enginev1.Question{
		QuestionId:  "v1-q1",
		TopicId:     "top-stars",
		VariantOf:   "q1",
		Prompt:      "Which best describes the physical nature of a star?",
		Choices:     []string{"Frozen gas cloud", "Luminous plasma sphere", "Solid iron core"},
		AnswerIndex: 1,
		Explanation: "Stars consist of luminous plasma.",
	}

	validVariant2 := &enginev1.Question{
		QuestionId:  "v2-q1",
		TopicId:     "top-stars",
		VariantOf:   "q1",
		Prompt:      "Astronomers define stars primarily as:",
		Choices:     []string{"Orbital debris", "Reflective ice mirrors", "Self-luminous plasma spheres"},
		AnswerIndex: 2,
		Explanation: "Self-luminous plasma spheres undergoing nuclear fusion.",
	}

	t.Run("valid lesson with topic-linked retry pools passes", func(t *testing.T) {
		lesson := &enginev1.Lesson{
			Slides: []*enginev1.Slide{validSlide},
			Test:   []*enginev1.Question{validQuestion},
			VariantPool: []*enginev1.TopicVariants{
				{
					TopicId:  "top-stars",
					Variants: []*enginev1.Question{validVariant1, validVariant2},
				},
			},
		}
		if err := lessontest.ValidateGeneratedLesson(lesson); err != nil {
			t.Fatalf("expected valid lesson to pass, got: %v", err)
		}
	})

	t.Run("slide missing topic_ids is rejected", func(t *testing.T) {
		slideWithoutTopics := &enginev1.Slide{
			SlideId:    "s1",
			Html:       `<div id="root"><h1>Sky</h1></div>`,
			DurationMs: 3000,
			Narration:  "Looking at the sky.",
			TopicIds:   nil,
		}
		lesson := &enginev1.Lesson{
			Slides: []*enginev1.Slide{slideWithoutTopics},
			Test:   []*enginev1.Question{validQuestion},
		}
		if err := lessontest.ValidateGeneratedLesson(lesson); err == nil {
			t.Fatal("expected error when slide has no topic_ids")
		}
	})

	t.Run("question missing topic_id is rejected", func(t *testing.T) {
		qWithoutTopic := &enginev1.Question{
			QuestionId:  "q1",
			TopicId:     "",
			Prompt:      "What is a star?",
			Choices:     []string{"Plasma", "Dust"},
			AnswerIndex: 0,
			Explanation: "Plasma",
		}
		lesson := &enginev1.Lesson{
			Slides: []*enginev1.Slide{validSlide},
			Test:   []*enginev1.Question{qWithoutTopic},
		}
		if err := lessontest.ValidateGeneratedLesson(lesson); err == nil {
			t.Fatal("expected error when question has empty topic_id")
		}
	})

	t.Run("taught topic coverage violation is rejected", func(t *testing.T) {
		qUntaught := &enginev1.Question{
			QuestionId:  "q2",
			TopicId:     "top-unmentioned-blackholes",
			Prompt:      "What is a black hole?",
			Choices:     []string{"Singularity", "Planet"},
			AnswerIndex: 0,
			Explanation: "Singularity",
		}
		lesson := &enginev1.Lesson{
			Slides: []*enginev1.Slide{validSlide}, // only teaches "top-stars"
			Test:   []*enginev1.Question{qUntaught},
		}
		if err := lessontest.ValidateGeneratedLesson(lesson); err == nil {
			t.Fatal("expected error when question tests an untaught topic")
		}
	})

	t.Run("variant with unchanged prompt is rejected", func(t *testing.T) {
		unparaphrasedVariant := &enginev1.Question{
			QuestionId:  "v1-q1",
			TopicId:     "top-stars",
			VariantOf:   "q1",
			Prompt:      validQuestion.Prompt, // Identical prompt!
			Choices:     []string{"A", "B"},
			AnswerIndex: 0,
			Explanation: "Exp",
		}
		lesson := &enginev1.Lesson{
			Slides: []*enginev1.Slide{validSlide},
			Test:   []*enginev1.Question{validQuestion},
			VariantPool: []*enginev1.TopicVariants{
				{
					TopicId:  "top-stars",
					Variants: []*enginev1.Question{unparaphrasedVariant, validVariant2},
				},
			},
		}
		if err := lessontest.ValidateGeneratedLesson(lesson); err == nil {
			t.Fatal("expected error when variant prompt is not paraphrased")
		}
	})

	t.Run("variant with duplicate choices is rejected", func(t *testing.T) {
		dupChoicesVariant := &enginev1.Question{
			QuestionId:  "v1-q1",
			TopicId:     "top-stars",
			VariantOf:   "q1",
			Prompt:      "Paraphrased prompt?",
			Choices:     []string{"Plasma", "plasma"},
			AnswerIndex: 0,
			Explanation: "Exp",
		}
		lesson := &enginev1.Lesson{
			Slides: []*enginev1.Slide{validSlide},
			Test:   []*enginev1.Question{validQuestion},
			VariantPool: []*enginev1.TopicVariants{
				{
					TopicId:  "top-stars",
					Variants: []*enginev1.Question{dupChoicesVariant, validVariant2},
				},
			},
		}
		if err := lessontest.ValidateGeneratedLesson(lesson); err == nil {
			t.Fatal("expected error when variant choices contain duplicates")
		}
	})
}
