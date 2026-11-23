package accounts

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidGuidedAnswers(t *testing.T) {
	big := `{"goal":{"value":"` + strings.Repeat("x", 100) + `"}}`
	cases := []struct {
		name string
		in   string
		ok   bool
	}{
		{"string", `{"goal":{"value":"learn Go"}}`, true},
		{"skip", `{"level":{"value":null,"skipped":true}}`, true},
		{"list", `{"goals":{"value":["speak","read"]}}`, true},
		{"number and bool", `{"minutes":{"value":20},"remind":{"value":true}}`, true},
		{"empty", `{}`, true},
		{"large but valid", big, true},
		{"not an object", `[1]`, false},
		{"null", `null`, false},
		{"missing value", `{"goal":{}}`, false},
		{"nested object value", `{"goal":{"value":{"a":1}}}`, false},
		{"mixed list", `{"goal":{"value":["a",1]}}`, false},
		{"unknown field", `{"goal":{"value":"x","extra":1}}`, false},
		{"bad step id", `{"Goal!":{"value":"x"}}`, false},
	}
	for _, c := range cases {
		if got := validGuidedAnswers(json.RawMessage(c.in)); got != c.ok {
			t.Errorf("%s: got %v want %v", c.name, got, c.ok)
		}
	}
	many := map[string]any{}
	for i := 0; i < 33; i++ {
		many["s"+strings.Repeat("a", i)] = map[string]any{"value": "x"}
	}
	raw, _ := json.Marshal(many)
	if validGuidedAnswers(raw) {
		t.Error("more than 32 answers must be rejected")
	}
}
