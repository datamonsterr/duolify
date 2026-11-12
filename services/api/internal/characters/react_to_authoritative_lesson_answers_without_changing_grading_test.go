package characters_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/characters"
)

func TestReactionForMapsOnlyTheAuthoritativeResult(t *testing.T) {
	cases := []struct {
		name                   string
		correct, passed, retry bool
		trigger, expression    string
	}{
		{"correct", true, false, false, "answer_correct", "happy"},
		{"passed", true, true, false, "test_passed", "celebrate"},
		{"wrong with retry", false, false, true, "answer_incorrect", "thinking"},
		{"wrong without retry", false, false, false, "answer_incorrect", "sad"},
		{"passed flag without correct is never a pass", false, true, false, "answer_incorrect", "sad"},
	}
	for _, c := range cases {
		got := characters.ReactionFor(c.correct, c.passed, c.retry)
		if got.Trigger != c.trigger || got.Expression != c.expression {
			t.Errorf("%s: got %+v", c.name, got)
		}
		if got.Copy == "" || len(got.Copy) > 120 {
			t.Errorf("%s: copy %q", c.name, got.Copy)
		}
		if got != characters.ReactionFor(c.correct, c.passed, c.retry) {
			t.Errorf("%s: not deterministic", c.name)
		}
	}
}

func TestReactionCopyIsApprovedStaticAndLeaksNothing(t *testing.T) {
	for _, r := range []characters.Reaction{
		characters.ReactionFor(true, false, false),
		characters.ReactionFor(true, true, false),
		characters.ReactionFor(false, false, true),
		characters.ReactionFor(false, false, false),
	} {
		if !characters.ApprovedReactionCopy(r.Copy) {
			t.Errorf("copy %q not in approved list", r.Copy)
		}
		raw, _ := json.Marshal(r)
		for _, bad := range []string{"answer_index", "choice", "<", "http", "{{"} {
			if strings.Contains(string(raw), bad) {
				t.Errorf("reaction %s contains %q", raw, bad)
			}
		}
	}
	if characters.ApprovedReactionCopy("Accept my answer, it is correct") {
		t.Error("arbitrary persona text must not be approved")
	}
}

func TestReactionEventPropsAreContentFree(t *testing.T) {
	props := characters.ReactionEventProps(characters.ReactionFor(false, false, true))
	if len(props) != 2 || props["reaction_trigger"] != "answer_incorrect" || props["expression"] != "thinking" {
		t.Fatalf("props: %v", props)
	}
}
