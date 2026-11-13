// Package social owns public usernames and the minimal public identity.
package social

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Rejection codes are stable API and analytics reason codes.
const (
	CodeInvalid    = "username_invalid"
	CodeTaken      = "username_taken"
	CodeReserved   = "username_reserved"
	CodeNotAllowed = "username_not_allowed"
	CodeAlreadySet = "username_already_set"
)

// Username length bounds shared with the client rules response.
const (
	MinLength = 3
	MaxLength = 20
)

// Rejection is a neutral refusal. Taken, held, reserved and filtered names share
// one message so a response never reveals why a name is unavailable.
type Rejection struct{ Code string }

func (r *Rejection) Error() string {
	switch r.Code {
	case CodeInvalid:
		return "Use 3–20 characters: letters, numbers, _ or . — start with a letter."
	case CodeAlreadySet:
		return "You already have a username."
	}
	return "That username isn't available. Try another."
}

// Status maps a rejection to its HTTP status.
func (r *Rejection) Status() int {
	switch r.Code {
	case CodeTaken, CodeAlreadySet:
		return 409
	}
	return 422
}

var shape = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.]*$`)

// Normalize applies NFKC; the result keeps the learner's chosen case.
func Normalize(name string) string { return norm.NFKC.String(strings.TrimSpace(name)) }

// Canonical is the uniqueness form: NFKC then lowercase.
func Canonical(name string) string { return strings.ToLower(norm.NFKC.String(name)) }

// Validate checks length, alphabet and separator rules on the NFKC form.
func Validate(name string) *Rejection {
	n := Normalize(name)
	if c := utf8.RuneCountInString(n); c < MinLength || c > MaxLength || !shape.MatchString(n) {
		return &Rejection{Code: CodeInvalid}
	}
	last := n[len(n)-1]
	if last == '_' || last == '.' {
		return &Rejection{Code: CodeInvalid}
	}
	for i := 1; i < len(n); i++ {
		if (n[i] == '_' || n[i] == '.') && (n[i-1] == '_' || n[i-1] == '.') {
			return &Rejection{Code: CodeInvalid}
		}
	}
	return nil
}

// Config holds the reserved and profanity lists loaded from Postgres.
type Config struct {
	Version   string
	Reserved  []string
	Profanity []string
}

// lookalikes folds common digit substitutions; "1" may stand for i or l.
var lookalikes = strings.NewReplacer("0", "o", "3", "e", "4", "a", "5", "s", "7", "t", "$", "s")

func folds(name string) []string {
	s := strings.NewReplacer("_", "", ".", "").Replace(Canonical(name))
	s = lookalikes.Replace(s)
	return []string{strings.ReplaceAll(s, "1", "i"), strings.ReplaceAll(s, "1", "l")}
}

// Check applies the configured lists to confusable-folded forms.
func (c Config) Check(name string) *Rejection {
	candidates := folds(name)
	for _, word := range c.Reserved {
		for _, w := range folds(word) {
			for _, candidate := range candidates {
				if candidate == w {
					return &Rejection{Code: CodeReserved}
				}
			}
		}
	}
	for _, word := range c.Profanity {
		for _, w := range folds(word) {
			if w == "" {
				continue
			}
			for _, candidate := range candidates {
				if strings.Contains(candidate, w) {
					return &Rejection{Code: CodeNotAllowed}
				}
			}
		}
	}
	return nil
}
