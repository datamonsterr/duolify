// Package steering stores screened instructions and immutable generation inputs.
package steering

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var urlPattern = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*:|www\.|\b[a-z0-9-]+\.(com|org|net|io|dev|test)(\b|/))`)

// ValidateText runs before any provider call; limits count Unicode characters.
func ValidateText(text string, maximum int) string {
	if strings.TrimSpace(text) == "" {
		return "empty"
	}
	if utf8.RuneCountInString(text) > maximum {
		return "too_long"
	}
	for _, r := range text {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == 0x2028 || r == 0x2029 {
			return "control_characters"
		}
	}
	if urlPattern.MatchString(text) {
		return "urls_not_allowed"
	}
	return injection(text)
}

// Keep in sync with engine/crates/engine-core/src/steering/mod.rs (same checks on adds and edits).
var delimiters = []string{"<learner", "</learner", "learner_instructions", "duolify_prompt", "course_steering", "<system", "</system", "<|", "|>", "[inst", "[/inst", "<<sys", "```", "<assistant", "<developer"}

var phrases = []string{
	"ignoreprevious", "ignoreall", "systemprompt", "developermessage", "hiddenprompt", "hiddeninstructions", "noquiz", "skiptest", "skipthetest", "skipgrading", "skipthegrading", "changegrading", "changethegrading", "makeallanswers", "makeeveryanswer", "allanswerscorrect", "everyanswercorrect", "freecredits",
	"bỏquahướngdẫn", "bỏquachỉdẫn", "đừngcóbàikiểmtra", "khôngcóbàikiểmtra", "bỏquabàikiểmtra", "tấtcảcáccâutrảlờiđềuđúng",
	"ignoralasinstrucciones", "ignoratodas", "ignoralasreglas", "sinexamen", "nohagasexamen",
	"ignorelesinstructions", "ignoretouteles", "ignorelesrègles", "sansquiz",
	"ignorierealle", "ignoriereanweisungen", "ignorieredievorherigen", "ohnetest", "keinquiz", "keintest",
	"忽略之前", "忽略以上", "忽略所有", "忽略上述", "不要测验", "不要测试", "不要考试", "跳过测验", "跳过测试", "所有答案都正确", "所有答案都是正确",
	"以前の指示を無視", "指示を無視", "テストをスキップ", "テストなし", "すべての答えを正解",
	"игнорируйпредыдущие", "игнорируйвсе", "игнорируйинструкции", "безтеста", "пропуститетест",
}

// injection folds lookalike brackets, lowercases and drops everything but letters and digits so
// spacing, dots and zero-width tricks cannot split a phrase.
func injection(text string) string {
	lower := strings.NewReplacer("\uFF1C", "<", "\u2039", "<", "\uFF1E", ">", "\u203A", ">").Replace(strings.ToLower(text))
	for _, d := range delimiters {
		if strings.Contains(lower, d) {
			return "prompt_injection"
		}
	}
	var compact strings.Builder
	for _, r := range lower {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			compact.WriteRune(r)
		}
	}
	c := compact.String()
	for _, p := range phrases {
		if strings.Contains(c, p) {
			return "prompt_injection"
		}
	}
	for _, verb := range []string{"ignore", "reveal", "show", "print"} {
		if strings.Contains(c, verb+"your") && (strings.Contains(c, "prompt") || strings.Contains(c, "instructions") || strings.Contains(c, "rules")) {
			return "prompt_injection"
		}
	}
	return ""
}

// LengthBucket is the only text-derived analytics property.
func LengthBucket(n int) string {
	if n <= 100 {
		return "1_100"
	}
	if n <= 250 {
		return "101_250"
	}
	return "251_500"
}
