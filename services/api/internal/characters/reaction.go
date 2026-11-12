package characters

// Reaction is the deterministic Character reaction to an authoritative graded
// Lesson answer. It is derived only from the grading result: persona text,
// Character names and learner answers never reach it, and it never feeds back
// into grading, Retry variants, XP, completion or unlocks.
type Reaction struct {
	Trigger    string `json:"trigger"`
	Expression string `json:"expression"`
	Copy       string `json:"copy"`
}

// Approved supportive lines. Keep in sync with the client and engine lists.
const (
	reactionCopyCorrect   = "Nice work — that's right."
	reactionCopyPassed    = "You passed! Great work."
	reactionCopyIncorrect = "Good try — read the explanation, then try the next one."
)

var approvedReactionCopy = map[string]bool{reactionCopyCorrect: true, reactionCopyPassed: true, reactionCopyIncorrect: true}

// ReactionFor maps a graded result to a reaction. passed without correct is
// never treated as a pass.
func ReactionFor(correct, passed, hasRetry bool) Reaction {
	switch {
	case correct && passed:
		return Reaction{"test_passed", "celebrate", reactionCopyPassed}
	case correct:
		return Reaction{"answer_correct", "happy", reactionCopyCorrect}
	case hasRetry:
		return Reaction{"answer_incorrect", "thinking", reactionCopyIncorrect}
	}
	return Reaction{"answer_incorrect", "sad", reactionCopyIncorrect}
}

// ApprovedReactionCopy reports whether line is one of the approved lines.
func ApprovedReactionCopy(line string) bool { return approvedReactionCopy[line] }

// ReactionEventProps are the only analytics properties a reaction may carry.
func ReactionEventProps(r Reaction) map[string]any {
	return map[string]any{"reaction_trigger": r.Trigger, "expression": r.Expression}
}
