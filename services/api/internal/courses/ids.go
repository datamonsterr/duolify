package courses

import "github.com/datamonsterr/duolify/services/api/internal/player"

// validIDs reports whether every id is a canonical UUID. Queries bind ids as `$n::uuid`
// against native columns so the indexes apply; Postgres rejects anything else with 22P02,
// so a handler or lookup calls this first and answers a malformed id like an unknown one.
func validIDs(ids ...string) bool {
	for _, id := range ids {
		if _, ok := player.CanonicalUUID(id); !ok {
			return false
		}
	}
	return true
}
