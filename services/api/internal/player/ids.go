package player

import (
	"regexp"
	"strings"
)

var canonicalUUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// CanonicalUUID returns the lower-case hyphenated form of a UUID path or body
// value. Handlers call it at the boundary so a malformed id is rejected with a
// client error before it reaches a typed `$n::uuid` bind in Postgres.
func CanonicalUUID(value string) (string, bool) {
	if !canonicalUUIDPattern.MatchString(value) {
		return "", false
	}
	return strings.ToLower(value), true
}
