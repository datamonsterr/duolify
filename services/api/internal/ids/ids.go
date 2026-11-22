// Package ids holds the one helper for binding an id that may not be a UUID.
package ids

import "github.com/jackc/pgx/v5/pgtype"

// Arg binds an id from outside the database (a payload, a client body, a fixture) as a `$n::uuid`
// parameter. Anything that is not a UUID binds NULL, so it matches no row, exactly as the old
// `col::text = $n` comparison did, instead of failing the statement with 22P02.
func Arg(id string) any {
	var u pgtype.UUID
	if u.Scan(id) != nil {
		return nil
	}
	return u
}
