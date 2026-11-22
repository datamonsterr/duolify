package examples

import enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"

// Proto returns the Lesson in the shape the engine returns for a generated lesson, so authored
// and generated lessons pass the same production validator.
func (l Lesson) Proto() *enginev1.Lesson {
	out := &enginev1.Lesson{}
	for _, s := range l.Slides {
		out.Slides = append(out.Slides, &enginev1.Slide{SlideId: s.ID, Html: s.HTML, Narration: s.Narration, DurationMs: uint32(s.DurationMS), TopicIds: s.TopicIDs}) //nolint:gosec // authored range checked by tests (3000 to 60000)
	}
	for _, q := range l.Test {
		out.Test = append(out.Test, q.proto())
	}
	for _, p := range l.VariantPool {
		tv := &enginev1.TopicVariants{TopicId: p.TopicID}
		for _, v := range p.Variants {
			v.TopicID = p.TopicID
			tv.Variants = append(tv.Variants, v.proto())
		}
		out.VariantPool = append(out.VariantPool, tv)
	}
	return out
}

func (q Question) proto() *enginev1.Question {
	return &enginev1.Question{QuestionId: q.ID, TopicId: q.TopicID, Prompt: q.Prompt, Choices: q.Choices, AnswerIndex: uint32(q.AnswerIndex), Explanation: q.Explanation, VariantOf: q.VariantOf} //nolint:gosec // authored index checked by tests
}
