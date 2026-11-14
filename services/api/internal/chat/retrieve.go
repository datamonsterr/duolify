package chat

import (
	"context"
	"regexp"
	"strings"
	"unicode"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"google.golang.org/protobuf/proto"
)

const (
	maxSearchTerms  = 12
	minTermRunes    = 3
	maxExcerpts     = 6
	maxExcerptRunes = 1200
)

// stopwords are common question words that would match almost every slide.
var stopwords = map[string]bool{}

func init() {
	for _, w := range strings.Fields("what which who whom whose when where why how does did the and are was were you your about tell with from that this these those can could would should please explain give show mean means have has not for any all") {
		stopwords[w] = true
	}
}

// searchTerms reduces free text to up to maxSearchTerms distinct letter/digit words joined with " | " for
// to_tsquery. Nothing but letters and digits survives, so the result carries no tsquery syntax; it is still
// only ever passed to Postgres as a bind parameter.
func searchTerms(message string) string {
	words := strings.FieldsFunc(strings.ToLower(message), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	seen := map[string]bool{}
	var out []string
	for _, w := range words {
		if len([]rune(w)) < minTermRunes || stopwords[w] || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
		if len(out) == maxSearchTerms {
			break
		}
	}
	return strings.Join(out, " | ")
}

var tags = regexp.MustCompile(`<[^>]*>`)

// trimExcerpt strips markup, collapses whitespace and caps the excerpt at maxExcerptRunes.
func trimExcerpt(s string) string {
	r := []rune(strings.Join(strings.Fields(tags.ReplaceAllString(s, " ")), " "))
	if len(r) > maxExcerptRunes {
		r = r[:maxExcerptRunes]
	}
	return string(r)
}

// fitExcerpts keeps whole excerpts in rank order while they fit in roomBytes (prompt-size accounting as
// promptBytes). It stops at the first excerpt that does not fit so a lower-ranked one never jumps the queue.
func fitExcerpts(in []*enginev1.SourceExcerpt, roomBytes int) []*enginev1.SourceExcerpt {
	var out []*enginev1.SourceExcerpt
	for _, e := range in {
		roomBytes -= proto.Size(e) * 3 / 2
		if roomBytes < 0 {
			break
		}
		out = append(out, e)
	}
	return out
}

// retrieve finds source passages in the learner's own generated lessons of this course. It never reads another
// learner's or course's rows (every join is keyed by learner and course) and the query text is a bind parameter.
// The expression in both to_tsvector calls must match the lesson_slides_fts index.
func (s *Service) retrieve(ctx context.Context, owner, course, message string, roomBytes int) ([]*enginev1.SourceExcerpt, error) {
	terms := searchTerms(message)
	if terms == "" || roomBytes <= 0 {
		return nil, nil
	}
	const vector = `to_tsvector('simple'::regconfig, s.narration || ' ' || regexp_replace(s.html, '<[^>]*>', ' ', 'g'))`
	rows, err := s.opts.Pool.Query(ctx, `SELECT l.id::text,l.title,s.narration,s.html
FROM lesson_slides s
JOIN course_lesson_stubs l ON l.id=s.lesson_stub_id AND l.learner_id=$1::uuid
JOIN course_units u ON u.id=l.unit_id AND u.course_id=$2::uuid AND u.learner_id=$1::uuid
WHERE s.learner_id=$1::uuid AND l.status<>'locked' AND `+vector+` @@ to_tsquery('simple'::regconfig,$3)
ORDER BY ts_rank_cd(`+vector+`, to_tsquery('simple'::regconfig,$3)) DESC,u.position,l.position,s.position
LIMIT $4`, owner, course, terms, maxExcerpts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var found []*enginev1.SourceExcerpt
	for rows.Next() {
		var id, title, narration, html string
		if err = rows.Scan(&id, &title, &narration, &html); err != nil {
			return nil, err
		}
		text := trimExcerpt(narration)
		if text == "" {
			text = trimExcerpt(html)
		}
		if text != "" {
			found = append(found, &enginev1.SourceExcerpt{LessonId: id, LessonTitle: title, Text: text})
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return fitExcerpts(found, roomBytes), nil
}
