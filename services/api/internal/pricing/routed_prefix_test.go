package pricing

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// action -> engine skills file whose budget_tokens the routed prefix must cover.
var routedSkillFiles = map[string]string{
	"preview":        "course_preview",
	"outline":        "course_outline",
	"outline_replan": "outline_replan",
	"lesson_content": "lesson_slides",
	"tutor_short":    "tutor_chat",
	"tutor_long":     "tutor_chat",
}

// Instruction tokens that move from the monolithic prompt into skills (already in the quote).
var movedIntoSkills = map[string]int64{"tutor_short": 175, "tutor_long": 175}

func TestRoutedPrefixCoversSkillBudgets(t *testing.T) {
	re := regexp.MustCompile(`(?m)^budget_tokens\s*=\s*(\d+)`)
	for action, file := range routedSkillFiles {
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "engine", "config", "skills", file+".toml")) //nolint:gosec // fixed repo path, table keys only
		if err != nil {
			t.Fatal(err)
		}
		m := re.FindSubmatch(raw)
		if m == nil {
			t.Fatalf("%s has no budget_tokens", file)
		}
		budget, _ := strconv.ParseInt(string(m[1]), 10, 64)
		want := budget - movedIntoSkills[action]
		if file == "course_outline" {
			want = 0 // no skills: the routed prompt is the monolithic one split in two
		}
		if routedPrefixTokens[action] < want {
			t.Fatalf("%s prefix %d does not cover skill budget %d", action, routedPrefixTokens[action], want)
		}
	}
	for action := range routedPrefixTokens {
		if _, ok := routedSkillFiles[action]; !ok {
			t.Fatalf("%s has a prefix but no skills file mapping", action)
		}
	}
}

func TestRoutedWorstCaseInputStaysInsideTheProtectedBudget(t *testing.T) {
	route := QuotePolicy{
		MaxAttempts:    2,
		ActionModels:   map[string]string{"lesson_content": "primary"},
		FallbackModels: map[string]string{"primary": "fallback"},
		Models: map[string]ModelRate{
			"primary":  {Provider: "openrouter", Model: "tencent/hy4-flash", Input: 35, Output: 141},
			"fallback": {Provider: "openrouter", Model: "z-ai/glm-5.3-flash", Input: 40, Output: 79},
		},
	}
	// A routed lesson at the quote's 6,000-token output cap with 4,000 quoted input tokens.
	in := int64(4000) + routedPrefixTokens["lesson_content"]
	routed, err := routeProtectedCost(route, "lesson_content", in, 6000)
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := routeProtectedCost(route, "lesson_content", 4000, 6000)
	if routed <= plain {
		t.Fatalf("routed bound %g must exceed the plain bound %g", routed, plain)
	}
	if err := enforceRouteBudget(route, "lesson_content", in, 6000, 25, 0.0015); err != nil {
		t.Fatalf("routed worst case leaves the protected budget: %v", err)
	}
}
