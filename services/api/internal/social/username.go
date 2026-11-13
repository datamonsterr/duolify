// Package social owns public usernames and the minimal public identity.
package social

import (
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Rejection codes are stable API and analytics reason codes.
const (
	CodeInvalid     = "username_invalid"
	CodeTaken       = "username_taken"
	CodeReserved    = "username_reserved"
	CodeNotAllowed  = "username_not_allowed"
	CodeAlreadySet  = "username_already_set"
	CodeNotSet      = "username_not_set"
	CodeCooldown    = "username_cooldown"
	CodeRateLimited = "rate_limited"
	CodeTooShort    = "search_too_short"
)

// Username length bounds shared with the client rules response.
const (
	MinLength = 3
	MaxLength = 20
)

// Rejection is a neutral refusal. Taken, held, reserved and filtered names share
// one message so a response never reveals why a name is unavailable.
type Rejection struct {
	Code string
	// NextChangeAt is when the rename cooldown ends (CodeCooldown only).
	NextChangeAt time.Time
	// RetryAfter is the wait until the limiter window resets (CodeRateLimited only).
	RetryAfter time.Duration
}

// RetryAfterSeconds is the whole-second Retry-After header value, at least one.
func (r *Rejection) RetryAfterSeconds() int {
	return max(1, int(math.Ceil(r.RetryAfter.Seconds())))
}

func (r *Rejection) Error() string {
	switch r.Code {
	case CodeInvalid:
		return "Use 3–20 characters: letters, numbers, _ or . — start with a letter."
	case CodeAlreadySet:
		return "You already have a username."
	case CodeNotSet:
		return "Choose a username first."
	case CodeCooldown:
		return "You can change your username again on " + r.NextChangeAt.UTC().Format("January 2, 2006") + "."
	case CodeRateLimited:
		return "Too many tries. Wait a moment and try again."
	case CodeTooShort:
		return "Type at least 3 characters."
	case CodeRequestUnavailable:
		return "We couldn't send that request."
	case CodeInviteUnavailable:
		return "This invite has expired. Ask for a new one."
	case CodePendingLimit:
		return "You have too many requests waiting. Cancel one or wait for a reply."
	case CodeFriendLimit:
		return "A friends list is full, so this can't be completed."
	case CodeNotPending:
		return "That request can't be changed any more."
	case CodeBlockUnavailable:
		return "We couldn't block that person."
	case CodeBlockLimit:
		return "Your blocked list is full. Unblock someone first."
	}
	return "That username isn't available. Try another."
}

// Status maps a rejection to its HTTP status.
func (r *Rejection) Status() int {
	switch r.Code {
	case CodeTaken, CodeAlreadySet, CodeNotSet, CodeCooldown, CodePendingLimit, CodeFriendLimit, CodeNotPending, CodeBlockLimit:
		return 409
	case CodeRequestUnavailable, CodeInviteUnavailable, CodeBlockUnavailable:
		return 404
	case CodeRateLimited:
		return 429
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
	// Limits come from username_config so operators tune them without a deploy.
	HoldDays, RenameCooldownDays, SearchMax, SearchPerMinute, AttemptsPerHour int
	// Friend limits (E6-03).
	FriendPendingMax, FriendSendsPerDay, FriendMax, DeclineCooldownDays, LinkValidDays, LinkPerHour int
	// BlockMax caps how many learners one learner can block (E6-04).
	BlockMax int
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
