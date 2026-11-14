package steering

import (
	"slices"
	"strings"
	"unicode/utf8"

	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
)

// Closed reasons for the sanitized review signal on generated output.
const (
	ReasonPromptLeak      = "prompt_leak"
	ReasonTestWeakened    = "test_weakened"
	ReasonMalformedOutput = "malformed_output"
	minVerbatimLeakRunes  = 24
)

var leakMarkers = []string{"duolify_prompt", "course_steering", "learner_instructions", "learner instructions are untrusted"}

// OutputViolation re-checks an engine-validated Lesson at the API trust boundary. It returns a
// closed reason when generated text echoes the prompt or an instruction verbatim, or when the
// lesson test was weakened; "" means deliver.
func OutputViolation(l *pb.Lesson, in []*pb.CourseInstruction) string {
	if l == nil {
		return ReasonMalformedOutput
	}
	var texts []string
	for _, s := range l.Slides {
		if s != nil {
			texts = append(texts, s.Narration, s.Html)
			for _, seg := range s.Segments {
				if seg != nil {
					texts = append(texts, seg.Text)
				}
			}
		}
	}
	questions := slices.Clone(l.Test)
	for _, p := range l.VariantPool {
		if p != nil {
			questions = append(questions, p.Variants...)
		}
	}
	for _, q := range questions {
		if q != nil {
			texts = append(texts, q.Prompt, q.Explanation)
			texts = append(texts, q.Choices...)
		}
	}
	for _, t := range texts {
		lower := strings.ToLower(t)
		for _, m := range leakMarkers {
			if strings.Contains(lower, m) {
				return ReasonPromptLeak
			}
		}
		for _, i := range in {
			if i == nil {
				continue
			}
			text := strings.ToLower(strings.TrimSpace(i.Text))
			if utf8.RuneCountInString(text) >= minVerbatimLeakRunes && strings.Contains(lower, text) {
				return ReasonPromptLeak
			}
		}
	}
	if len(l.Test) == 0 {
		return ReasonTestWeakened
	}
	for _, q := range l.Test {
		if q == nil || len(q.Choices) < 2 || int(q.AnswerIndex) >= len(q.Choices) {
			return ReasonTestWeakened
		}
	}
	return ""
}

// FlagOutput emits the review signal for rejected generated output: a closed reason and the
// Course id only, never learner text, instruction text or generated text.
func (s *Service) FlagOutput(owner, course, reason string) {
	if !slices.Contains([]string{ReasonPromptLeak, ReasonTestWeakened, ReasonMalformedOutput}, reason) {
		return
	}
	s.emit(owner, "course_instruction_output_flagged", map[string]any{"course_id": course, "reason_code": reason})
}
