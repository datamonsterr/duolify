package social

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// ErrQueryTooShort is returned for queries under MinLength characters.
var ErrQueryTooShort = errors.New("search query too short")

// Query is a normalized search term. Length is the normalized character count
// (the only thing analytics may see); Searchable is false for terms that cannot
// match any username, which return an empty list instead of an error.
type Query struct {
	Text       string
	Length     int
	Searchable bool
}

var searchable = regexp.MustCompile(`^[a-z0-9_.]+$`)

// ParseQuery applies NFKC, trims, drops one leading "@" and lowercases.
func ParseQuery(raw string) (Query, error) {
	text := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(norm.NFKC.String(raw)), "@"))
	q := Query{Length: utf8.RuneCountInString(text)}
	if q.Length < MinLength {
		return q, ErrQueryTooShort
	}
	if q.Length <= MaxLength && searchable.MatchString(text) {
		q.Text, q.Searchable = text, true
	}
	return q, nil
}

// likePrefix turns a term into a LIKE pattern with "\" as escape; "_" and "%" are data.
func likePrefix(text string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(text) + "%"
}

// blockedPairClause is the single seam for bilateral block hiding. It is empty
// until E6-04 creates the blocks table; that ticket sets it to one NOT EXISTS
// over blocks in both directions between $1 (the viewer) and u.learner_id.
// Search and Profile both append it, so no other query changes.
const blockedPairClause = ""

// exactVisible: a nondiscoverable learner may still be reached by exact username
// when they accept requests from everyone.
const exactVisible = `(COALESCE(ss.discoverable,true) OR COALESCE(ss.request_policy,'everyone')='everyone')`

// Search returns up to the configured number of matches. $1 is always the viewer.
func (s *Service) Search(ctx context.Context, viewerID, raw string) ([]Identity, error) {
	q, err := ParseQuery(raw)
	if err != nil {
		return nil, &Rejection{Code: CodeTooShort}
	}
	cfg, err := s.config(ctx)
	if err != nil {
		return nil, err
	}
	if rej, err := s.allow(ctx, viewerID, bucketSearch, cfg.SearchPerMinute, searchWindow); rej != nil || err != nil {
		if rej != nil {
			return nil, rej
		}
		return nil, err
	}
	out := []Identity{}
	if q.Searchable {
		rows, err := s.pool.Query(ctx, `SELECT u.username,l.display_name
 FROM learner_usernames u JOIN learners l ON l.id=u.learner_id LEFT JOIN learner_social_settings ss ON ss.learner_id=u.learner_id
 WHERE u.learner_id<>$1::uuid AND ((u.canonical=$2 AND `+exactVisible+`) OR (u.canonical LIKE $3 ESCAPE '\' AND COALESCE(ss.discoverable,true)))`+blockedPairClause+`
 ORDER BY (u.canonical=$2) DESC, u.canonical LIMIT $4`, viewerID, q.Text, likePrefix(q.Text), cfg.SearchMax)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var r identityRow
			if err = rows.Scan(&r.Username, &r.DisplayName); err != nil {
				return nil, err
			}
			out = append(out, r.Project(nil, false))
		}
		if err = rows.Err(); err != nil {
			return nil, err
		}
	}
	s.emit(viewerID, "username_searched", map[string]any{"query_length": q.Length, "result_count": len(out)})
	return out, nil
}
