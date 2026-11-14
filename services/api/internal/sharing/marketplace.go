package sharing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/datamonsterr/duolify/services/api/internal/social"
)

// E15-07: the public Marketplace. Postgres is the source of truth for listings, search and
// ranking; Redis only caches result pages and can be lost at any time.

var (
	// ErrMarketplaceDisabled is the neutral refusal while the release flag is off.
	ErrMarketplaceDisabled = errors.New("marketplace_unavailable")
	// ErrCourseUnavailable is the one neutral answer for an unlisted, delisted, unknown or block-hidden Course.
	ErrCourseUnavailable = errors.New("course_unavailable")
	// ErrInvalidQuery rejects a catalogue request whose parameters fall outside the closed vocabulary.
	ErrInvalidQuery = errors.New("invalid_query")
	// ErrInvalidCursor rejects a cursor that is malformed, forged or from another sort.
	ErrInvalidCursor = errors.New("invalid_cursor")
)

// Categories is the fixed taxonomy.
var Categories = []string{"science", "tech", "languages", "life_skills", "arts"}

var languagePattern = regexp.MustCompile(`^[a-z]{2,3}$`)

// ValidCategory reports whether c is in the fixed taxonomy.
func ValidCategory(c string) bool {
	for _, v := range Categories {
		if v == c {
			return true
		}
	}
	return false
}

// ValidLanguage reports whether l is a lowercase ISO 639 primary language tag such as "en".
func ValidLanguage(l string) bool { return languagePattern.MatchString(l) }

// NotEligibleError carries the closed reason codes that keep a Course out of the catalogue.
type NotEligibleError struct{ Reasons []string }

func (e *NotEligibleError) Error() string { return "not_eligible: " + strings.Join(e.Reasons, ",") }

// OwnerFacts are the inputs to the listing eligibility rule.
type OwnerFacts struct {
	EmailVerified    bool
	AccountAge       time.Duration
	GeneratedLessons int
	Title            string
	Description      string
	Category         string
	Language         string
	Tags             []string
	// OtherListings counts the owner's other active listings.
	OtherListings int
}

const maxTagRunes = 40

// EvaluateEligibility returns every failed condition as a closed reason code; none means eligible.
func EvaluateEligibility(f OwnerFacts, cfg Config) []string {
	var r []string
	if !f.EmailVerified {
		r = append(r, "email_not_verified")
	}
	if f.AccountAge < time.Duration(cfg.MarketplaceMinAccountAgeDays)*24*time.Hour {
		r = append(r, "account_too_new")
	}
	if f.GeneratedLessons < cfg.MarketplaceMinLessons {
		r = append(r, "too_few_lessons")
	}
	if strings.TrimSpace(f.Title) == "" {
		r = append(r, "missing_title")
	}
	if strings.TrimSpace(f.Description) == "" {
		r = append(r, "missing_description")
	}
	switch {
	case f.Category == "":
		r = append(r, "missing_category")
	case !ValidCategory(f.Category):
		r = append(r, "invalid_category")
	}
	switch {
	case f.Language == "":
		r = append(r, "missing_language")
	case !ValidLanguage(f.Language):
		r = append(r, "invalid_language")
	}
	if len(f.Tags) > cfg.MarketplaceMaxTags {
		r = append(r, "too_many_tags")
	}
	for _, t := range f.Tags {
		if n := utf8.RuneCountInString(strings.TrimSpace(t)); n == 0 || n > maxTagRunes || hasControl(t) {
			r = append(r, "invalid_tag")
			break
		}
	}
	if f.OtherListings >= cfg.MarketplaceMaxListingsPerOwner {
		r = append(r, "listing_cap")
	}
	return r
}

func hasControl(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return unicode.IsControl(r) }) >= 0
}

// BayesianRating is (C*m + sum) / (C + n): a course with no ratings scores the platform mean m.
func BayesianRating(sum, count int64, mean float64, minCount int) float64 {
	denom := float64(minCount) + float64(count)
	if denom == 0 {
		return mean
	}
	return (float64(minCount)*mean + float64(sum)) / denom
}

// BrowseQuery is a catalogue request. Zero values mean "no filter" and defaults.
type BrowseQuery struct {
	Q        string
	Category string
	Language string
	Sort     string
	Cursor   string
	Limit    int
}

// NormalizeBrowse trims, defaults and validates a request against the closed vocabulary.
func NormalizeBrowse(q BrowseQuery, cfg Config) (BrowseQuery, error) {
	q.Q = strings.TrimSpace(q.Q)
	if utf8.RuneCountInString(q.Q) > cfg.MarketplaceMaxQueryLength || hasControl(q.Q) || !utf8.ValidString(q.Q) {
		return q, ErrInvalidQuery
	}
	if q.Category != "" && !ValidCategory(q.Category) {
		return q, ErrInvalidQuery
	}
	q.Language = strings.ToLower(strings.TrimSpace(q.Language))
	if q.Language != "" && !ValidLanguage(q.Language) {
		return q, ErrInvalidQuery
	}
	switch q.Sort {
	case "":
		q.Sort = "popular"
	case "popular", "new", "rating":
	default:
		return q, ErrInvalidQuery
	}
	if q.Limit <= 0 {
		q.Limit = cfg.MarketplacePageSize
	}
	q.Limit = min(q.Limit, cfg.MarketplaceMaxPageSize)
	return q, nil
}

// Cursor is the keyset position after the last returned row. Every sort orders by
// (Key, Tie, publish time, id) descending; "new" uses Key and Tie 0.
type Cursor struct {
	Sort string
	Key  float64
	Tie  int64
	At   time.Time
	ID   string
}

type cursorWire struct {
	S string  `json:"s"`
	K float64 `json:"k"`
	T int64   `json:"t"`
	A int64   `json:"a"`
	I string  `json:"i"`
}

// EncodeCursor returns the opaque base64url form.
func EncodeCursor(c Cursor) string {
	raw, err := json.Marshal(cursorWire{S: c.Sort, K: c.Key, T: c.Tie, A: c.At.UnixMicro(), I: c.ID})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// DecodeCursor parses a cursor for the given sort; an empty cursor is the first page.
func DecodeCursor(raw, sort string) (Cursor, error) {
	if raw == "" {
		return Cursor{}, nil
	}
	if len(raw) > 512 {
		return Cursor{}, ErrInvalidCursor
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return Cursor{}, ErrInvalidCursor
	}
	var w cursorWire
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if dec.Decode(&w) != nil || dec.More() {
		return Cursor{}, ErrInvalidCursor
	}
	if w.S != sort || !validID(w.I) || math.IsNaN(w.K) || math.IsInf(w.K, 0) || w.T < 0 || w.A < 0 {
		return Cursor{}, ErrInvalidCursor
	}
	return Cursor{Sort: w.S, Key: w.K, Tie: w.T, At: time.UnixMicro(w.A).UTC(), ID: w.I}, nil
}

// CatalogueItem is one card: aggregates and allowlisted public identity only.
type CatalogueItem struct {
	CourseID        string      `json:"course_id"`
	Title           string      `json:"title"`
	Description     string      `json:"description"`
	Category        string      `json:"category"`
	Language        string      `json:"language"`
	Version         int         `json:"version"`
	PublishedAt     time.Time   `json:"published_at"`
	Creator         CreatorInfo `json:"creator"`
	LessonsReady    int         `json:"lessons_ready"`
	LessonsTotal    int         `json:"lessons_total"`
	EnrollmentCount int         `json:"enrollment_count"`
	RatingAverage   *float64    `json:"rating_average"`
	RatingCount     int         `json:"rating_count"`
}

// Page is one catalogue page; NextCursor is empty on the last page.
type Page struct {
	Items      []CatalogueItem `json:"items"`
	NextCursor string          `json:"next_cursor"`
}

// Listing is the owner's view of a listed Course.
type Listing struct {
	CourseID    string    `json:"course_id"`
	Version     int       `json:"version"`
	Category    string    `json:"category"`
	Language    string    `json:"language"`
	Tags        []string  `json:"tags"`
	PublishedAt time.Time `json:"published_at"`
}

// ListingInput is what the owner chooses; title and description come from the screened draft.
type ListingInput struct {
	Category string   `json:"category"`
	Language string   `json:"language"`
	Tags     []string `json:"tags"`
}

// MarketViewer is the signed-in viewer's relation to a Course page.
type MarketViewer struct {
	Enrolled bool   `json:"enrolled"`
	Owner    bool   `json:"owner"`
	CourseID string `json:"course_id,omitempty"`
}

// MarketCoursePage is the Course page: the E15-02 allowlisted preview plus listing facts.
type MarketCoursePage struct {
	CourseID        string              `json:"course_id"`
	Title           string              `json:"title"`
	Description     string              `json:"description"`
	Category        string              `json:"category"`
	Language        string              `json:"language"`
	Tags            []string            `json:"tags"`
	Version         int                 `json:"version"`
	PublishedAt     time.Time           `json:"published_at"`
	Creator         CreatorInfo         `json:"creator"`
	Outline         []PublicOutlineUnit `json:"outline"`
	SampleSlides    []PublicSlide       `json:"sample_slides"`
	LessonsReady    int                 `json:"lessons_ready"`
	LessonsTotal    int                 `json:"lessons_total"`
	EnrollmentCount int                 `json:"enrollment_count"`
	RatingAverage   *float64            `json:"rating_average"`
	RatingCount     int                 `json:"rating_count"`
	Viewer          MarketViewer        `json:"viewer"`
}

// WithMarketplaceCache returns a copy that caches catalogue pages in Redis. Redis is derived.
func (s *Service) WithMarketplaceCache(r *redis.Client) *Service { c := *s; c.cache = r; return &c }

// MarketplaceEnabled reports the release flag.
func (s *Service) MarketplaceEnabled() bool { return s.cfg.MarketplaceEnabled }

// anonymousID aggregates anonymous reads under the reserved operations identity (no visitor id).
const anonymousID = "00000000-0000-4000-8000-000000000011"

// ListCourse lists the owner's Course from its latest passing draft. Eligibility is re-checked
// on every call, so a relist after a delist needs the same conditions.
func (s *Service) ListCourse(ctx context.Context, owner, courseID string, in ListingInput) (Listing, error) {
	if !s.cfg.MarketplaceEnabled {
		return Listing{}, ErrMarketplaceDisabled
	}
	if !validID(owner) || !validID(courseID) {
		return Listing{}, ErrUnavailable
	}
	adult := false
	if s.age != nil {
		var err error
		if adult, err = s.age.MeetsMinAge(ctx, owner, s.cfg.MinPublisherAge); err != nil {
			return Listing{}, err
		}
	}
	if !adult {
		return Listing{}, ErrAdultRequired
	}
	in.Category, in.Language = strings.TrimSpace(in.Category), strings.ToLower(strings.TrimSpace(in.Language))
	tags := normalizeTags(in.Tags)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Listing{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = owns(ctx, tx, owner, courseID); err != nil {
		return Listing{}, err
	}
	if err = s.moderated(ctx, tx, owner, courseID); err != nil {
		return Listing{}, err
	}
	var (
		draftID, verdict string
		number, total    int
		raw              []byte
	)
	err = tx.QueryRow(ctx, `SELECT id::text, verdict, number, lesson_count, snapshot FROM publication_drafts WHERE course_id=$1 ORDER BY number DESC LIMIT 1`, courseID).Scan(&draftID, &verdict, &number, &total, &raw)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && verdict != VerdictPass) {
		return Listing{}, ErrPublishNotAllowed
	}
	if err != nil {
		return Listing{}, err
	}
	var snap Snapshot
	if err = json.Unmarshal(raw, &snap); err != nil {
		return Listing{}, err
	}
	generated := 0
	for _, u := range snap.Units {
		for _, l := range u.Lessons {
			if l.Generated {
				generated++
			}
		}
	}
	facts := OwnerFacts{GeneratedLessons: generated, Title: snap.Title, Description: snap.Description, Category: in.Category, Language: in.Language, Tags: in.Tags}
	var ageSeconds float64
	if err = tx.QueryRow(ctx, `SELECT
  (EXISTS (SELECT 1 FROM auth_identities WHERE learner_id=$1) OR EXISTS (SELECT 1 FROM auth_provider_identities WHERE learner_id=$1)),
  COALESCE(extract(epoch FROM now() - (SELECT created_at FROM learners WHERE id=$1)), 0),
  (SELECT count(*) FROM marketplace_listings WHERE owner_id=$1 AND course_id<>$2 AND delisted_at IS NULL)`, owner, courseID).Scan(&facts.EmailVerified, &ageSeconds, &facts.OtherListings); err != nil {
		return Listing{}, err
	}
	facts.AccountAge = time.Duration(ageSeconds * float64(time.Second))
	if reasons := EvaluateEligibility(facts, s.cfg); len(reasons) > 0 {
		return Listing{}, &NotEligibleError{Reasons: reasons}
	}
	var l Listing
	l.CourseID, l.Version, l.Category, l.Language, l.Tags = courseID, number, in.Category, in.Language, tags
	err = tx.QueryRow(ctx, `INSERT INTO marketplace_listings(course_id,owner_id,draft_id,version,title,description,category,language,tags,lesson_count,generated_lessons,search)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,
  setweight(to_tsvector('simple',$5),'A') || setweight(to_tsvector('simple',array_to_string($9::text[],' ')),'B') || setweight(to_tsvector('simple',$6),'C'))
ON CONFLICT (course_id) DO UPDATE SET draft_id=EXCLUDED.draft_id, version=EXCLUDED.version, title=EXCLUDED.title, description=EXCLUDED.description,
  category=EXCLUDED.category, language=EXCLUDED.language, tags=EXCLUDED.tags, lesson_count=EXCLUDED.lesson_count, generated_lessons=EXCLUDED.generated_lessons,
  search=EXCLUDED.search, published_at=CASE WHEN marketplace_listings.delisted_at IS NOT NULL THEN now() ELSE marketplace_listings.published_at END, delisted_at=NULL
RETURNING published_at`, courseID, owner, draftID, number, snap.Title, snap.Description, in.Category, in.Language, tags, total, generated).Scan(&l.PublishedAt)
	if err != nil {
		return Listing{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO marketplace_listing_stats(course_id) VALUES($1) ON CONFLICT DO NOTHING`, courseID); err != nil {
		return Listing{}, err
	}
	var oldVis string
	if err = tx.QueryRow(ctx, `SELECT visibility::text FROM courses WHERE id=$1`, courseID).Scan(&oldVis); err != nil {
		return Listing{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE courses SET visibility='public' WHERE id=$1`, courseID); err != nil {
		return Listing{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Listing{}, err
	}
	s.refreshListingStats(ctx, courseID)
	s.invalidateCatalogue(ctx)
	if oldVis != "public" {
		s.emit(owner, "course_visibility_changed", map[string]any{"course_id": courseID, "from": oldVis, "to": "public"})
		s.emit(owner, "course_published", map[string]any{"course_id": courseID, "version": number, "visibility": "public", "lesson_count": total})
	}
	return l, nil
}

// normalizeTags lowercases, trims and de-duplicates in order.
func normalizeTags(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// DelistCourse removes the owner's Course from the catalogue at once, including every cached
// page. It is idempotent; the share link keeps working.
func (s *Service) DelistCourse(ctx context.Context, owner, courseID string) error {
	if !s.cfg.MarketplaceEnabled {
		return ErrMarketplaceDisabled
	}
	if !validID(owner) || !validID(courseID) {
		return ErrUnavailable
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = owns(ctx, tx, owner, courseID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE marketplace_listings SET delisted_at=now() WHERE course_id=$1 AND delisted_at IS NULL`, courseID); err != nil {
		return err
	}
	var changed bool
	if err = tx.QueryRow(ctx, `WITH u AS (UPDATE courses SET visibility = CASE WHEN EXISTS (SELECT 1 FROM publication_links WHERE course_id=$1 AND revoked_at IS NULL) THEN 'link'::course_visibility ELSE 'private'::course_visibility END
WHERE id=$1 AND visibility='public' RETURNING 1) SELECT EXISTS (SELECT 1 FROM u)`, courseID).Scan(&changed); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	s.invalidateCatalogue(ctx)
	if changed {
		var now string
		_ = s.pool.QueryRow(ctx, `SELECT visibility::text FROM courses WHERE id=$1`, courseID).Scan(&now)
		s.emit(owner, "course_visibility_changed", map[string]any{"course_id": courseID, "from": "public", "to": now})
	}
	return nil
}

// ListingStatus reports the owner's listing, or ErrCourseUnavailable when there is none.
func (s *Service) ListingStatus(ctx context.Context, owner, courseID string) (Listing, error) {
	if !s.cfg.MarketplaceEnabled {
		return Listing{}, ErrMarketplaceDisabled
	}
	if !validID(owner) || !validID(courseID) {
		return Listing{}, ErrUnavailable
	}
	var l Listing
	err := s.pool.QueryRow(ctx, `SELECT l.course_id::text, l.version, l.category, l.language, l.tags, l.published_at FROM marketplace_listings l
JOIN courses c ON c.id=l.course_id WHERE l.course_id=$1 AND c.learner_id=$2 AND c.deleted_at IS NULL AND l.delisted_at IS NULL`, courseID, owner).
		Scan(&l.CourseID, &l.Version, &l.Category, &l.Language, &l.Tags, &l.PublishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Listing{}, ErrCourseUnavailable
	}
	return l, err
}

const (
	pageKeyPrefix = "marketplace:v1:page:"
	registryKey   = "marketplace:v1:keys"
)

// invalidateCatalogue deletes every cached page and the registry. A Redis failure only logs: the
// pages are derived, expire with the TTL, and Postgres already holds the truth.
func (s *Service) invalidateCatalogue(ctx context.Context) {
	if s.cache == nil {
		return
	}
	keys, err := s.cache.SMembers(ctx, registryKey).Result()
	if err != nil {
		slog.WarnContext(ctx, "marketplace cache invalidation failed", "error", err)
		return
	}
	if err = s.cache.Del(ctx, append(keys, registryKey)...).Err(); err != nil {
		slog.WarnContext(ctx, "marketplace cache invalidation failed", "error", err)
	}
}

func (s *Service) pageKey(q BrowseQuery) string {
	raw, _ := json.Marshal(q)
	sum := sha256.Sum256(raw)
	return pageKeyPrefix + hex.EncodeToString(sum[:])
}

// Browse serves one catalogue page. viewer may be empty (anonymous).
func (s *Service) Browse(ctx context.Context, viewer string, q BrowseQuery) (Page, error) {
	if !s.cfg.MarketplaceEnabled {
		return Page{}, ErrMarketplaceDisabled
	}
	q, err := NormalizeBrowse(q, s.cfg)
	if err != nil {
		return Page{}, err
	}
	cur, err := DecodeCursor(q.Cursor, q.Sort)
	if err != nil {
		return Page{}, err
	}
	if viewer != "" && !validID(viewer) {
		viewer = ""
	}
	// A viewer with blocks gets an exact, uncached page; everyone else shares the cache.
	cacheable := s.cache != nil
	if cacheable && viewer != "" {
		var blocks bool
		if err = s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM learner_blocks WHERE blocker_id=$1 OR blocked_id=$1)`, viewer).Scan(&blocks); err != nil {
			return Page{}, err
		}
		cacheable = !blocks
	}
	var page Page
	hit := false
	key := s.pageKey(q)
	if cacheable {
		if raw, gerr := s.cache.Get(ctx, key).Bytes(); gerr == nil && json.Unmarshal(raw, &page) == nil {
			hit = true
		} else if gerr != nil && !errors.Is(gerr, redis.Nil) {
			cacheable = false // Redis is down: serve from Postgres and do not try to fill it
		}
	}
	if !hit {
		if page, err = s.queryCatalogue(ctx, viewer, q, cur); err != nil {
			return Page{}, err
		}
		if cacheable {
			s.fillCache(ctx, key, page)
		}
	}
	s.emitBrowse(viewer, q, len(page.Items))
	return page, nil
}

func (s *Service) fillCache(ctx context.Context, key string, page Page) {
	raw, err := json.Marshal(page)
	if err != nil {
		return
	}
	ttl := time.Duration(s.cfg.MarketplaceCacheTTLSeconds) * time.Second
	pipe := s.cache.Pipeline()
	pipe.Set(ctx, key, raw, ttl)
	pipe.SAdd(ctx, registryKey, key)
	// The registry always outlives the pages it lists, so an invalidation can reach them all.
	pipe.Expire(ctx, registryKey, ttl+time.Minute)
	if _, err = pipe.Exec(ctx); err != nil {
		slog.WarnContext(ctx, "marketplace cache fill failed", "error", err)
	}
}

func (s *Service) emitBrowse(viewer string, q BrowseQuery, results int) {
	id := viewer
	if id == "" {
		id = anonymousID
	}
	props := map[string]any{"sort": q.Sort, "result_count": results}
	if q.Category != "" {
		props["category"] = q.Category
	}
	if q.Language != "" {
		props["language"] = q.Language
	}
	if q.Q != "" {
		// The text itself is never sent: only how long it was.
		props["query_length"] = utf8.RuneCountInString(q.Q)
		s.emit(id, "marketplace_searched", props)
		return
	}
	props["has_cursor"] = q.Cursor != ""
	s.emit(id, "marketplace_browsed", props)
}

// queryCatalogue is the Postgres read: filter, full-text match, block filter, ranking and keyset.
func (s *Service) queryCatalogue(ctx context.Context, viewer string, q BrowseQuery, cur Cursor) (Page, error) {
	args := []any{s.cfg.MarketplaceRatingPrior, s.cfg.MarketplaceRatingMinCount}
	arg := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
	var key, tie string
	switch q.Sort {
	case "popular":
		key, tie = "s.popular_score", "s.enrollments_30d::bigint"
	case "rating":
		key = "COALESCE((m.min_count::float8*m.mean + s.rating_sum::float8) / NULLIF(m.min_count::float8 + s.rating_count::float8, 0), m.mean)"
		tie = "s.rating_count::bigint"
	default:
		key, tie = "0::float8", "0::bigint"
	}
	var where strings.Builder
	where.WriteString(`l.delisted_at IS NULL AND l.hidden_at IS NULL AND c.deleted_at IS NULL AND c.visibility='public'`)
	if q.Category != "" {
		where.WriteString(" AND l.category=" + arg(q.Category))
	}
	if q.Language != "" {
		where.WriteString(" AND l.language=" + arg(q.Language))
	}
	if q.Q != "" {
		where.WriteString(" AND l.search @@ websearch_to_tsquery('simple', " + arg(q.Q) + ")")
	}
	if viewer != "" {
		where.WriteString(social.NotBlockedSQL(arg(viewer)+"::uuid", "l.owner_id"))
	}
	keyset := ""
	if q.Cursor != "" {
		keyset = fmt.Sprintf(" WHERE (r.sort_key, r.sort_tie, r.published_at, r.course_id) < (%s::float8, %s::bigint, %s::timestamptz, %s::uuid)", arg(cur.Key), arg(cur.Tie), arg(cur.At), arg(cur.ID))
	}
	sql := fmt.Sprintf(`WITH m AS (
  SELECT COALESCE(sum(st.rating_sum)::float8 / NULLIF(sum(st.rating_count), 0), $1::float8) AS mean, $2::int AS min_count
  FROM marketplace_listing_stats st JOIN marketplace_listings ml ON ml.course_id=st.course_id WHERE ml.delisted_at IS NULL AND ml.hidden_at IS NULL)
SELECT r.course_id::text, r.title, r.description, r.category, r.language, r.version, r.published_at, r.username, r.display_name,
  r.generated_lessons, r.lesson_count, (SELECT count(*) FROM course_copies cc WHERE cc.source_course_id=r.course_id), r.rating_sum, r.rating_count, r.sort_key, r.sort_tie
FROM (
  SELECT l.course_id, l.title, l.description, l.category, l.language, l.version, l.published_at, l.generated_lessons, l.lesson_count,
    COALESCE(lu.username,'') AS username, COALESCE(o.display_name,'') AS display_name, s.rating_sum, s.rating_count,
    (%s) AS sort_key, (%s) AS sort_tie
  FROM marketplace_listings l
  JOIN marketplace_listing_stats s ON s.course_id=l.course_id
  JOIN courses c ON c.id=l.course_id
  JOIN learners o ON o.id=l.owner_id
  LEFT JOIN learner_usernames lu ON lu.learner_id=l.owner_id
  CROSS JOIN m
  WHERE %s) r%s
ORDER BY r.sort_key DESC, r.sort_tie DESC, r.published_at DESC, r.course_id DESC
LIMIT %s`, key, tie, where.String(), keyset, arg(q.Limit+1))
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	page := Page{Items: []CatalogueItem{}}
	type position struct {
		key float64
		tie int64
	}
	var positions []position
	for rows.Next() {
		var it CatalogueItem
		var sum int64
		var p position
		if err = rows.Scan(&it.CourseID, &it.Title, &it.Description, &it.Category, &it.Language, &it.Version, &it.PublishedAt, &it.Creator.Username, &it.Creator.DisplayName,
			&it.LessonsReady, &it.LessonsTotal, &it.EnrollmentCount, &sum, &it.RatingCount, &p.key, &p.tie); err != nil {
			return Page{}, err
		}
		if len(page.Items) == q.Limit {
			// One row beyond the page proves there is a next page; the cursor is the last served row.
			last, at := page.Items[q.Limit-1], positions[q.Limit-1]
			page.NextCursor = EncodeCursor(Cursor{Sort: q.Sort, Key: at.key, Tie: at.tie, At: last.PublishedAt, ID: last.CourseID})
			break
		}
		if it.RatingCount > 0 {
			avg := float64(sum) / float64(it.RatingCount)
			it.RatingAverage = &avg
		}
		it.Description = clip(it.Description, 280)
		page.Items = append(page.Items, it)
		positions = append(positions, p)
	}
	return page, rows.Err()
}

// clip shortens s to at most n runes.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// MarketCourse is the Course page. Anything not listed, delisted, deleted or hidden by a block
// is the same ErrCourseUnavailable.
func (s *Service) MarketCourse(ctx context.Context, viewer, courseID string) (MarketCoursePage, error) {
	if !s.cfg.MarketplaceEnabled {
		return MarketCoursePage{}, ErrMarketplaceDisabled
	}
	if !validID(courseID) {
		return MarketCoursePage{}, ErrCourseUnavailable
	}
	if viewer != "" && !validID(viewer) {
		viewer = ""
	}
	var p MarketCoursePage
	var ownerID, draftID string
	var sum int64
	err := s.pool.QueryRow(ctx, `SELECT l.course_id::text, l.title, l.description, l.category, l.language, l.tags, l.version, l.published_at, l.owner_id::text, COALESCE(l.draft_id::text,''),
  l.generated_lessons, l.lesson_count, st.rating_sum, st.rating_count, COALESCE(lu.username,''), COALESCE(o.display_name,'')
FROM marketplace_listings l
JOIN marketplace_listing_stats st ON st.course_id=l.course_id
JOIN courses c ON c.id=l.course_id AND c.deleted_at IS NULL AND c.visibility='public'
JOIN learners o ON o.id=l.owner_id
LEFT JOIN learner_usernames lu ON lu.learner_id=l.owner_id
WHERE l.course_id=$1 AND l.delisted_at IS NULL AND l.hidden_at IS NULL`, courseID).
		Scan(&p.CourseID, &p.Title, &p.Description, &p.Category, &p.Language, &p.Tags, &p.Version, &p.PublishedAt, &ownerID, &draftID,
			&p.LessonsReady, &p.LessonsTotal, &sum, &p.RatingCount, &p.Creator.Username, &p.Creator.DisplayName)
	if errors.Is(err, pgx.ErrNoRows) {
		return MarketCoursePage{}, ErrCourseUnavailable
	}
	if err != nil {
		return MarketCoursePage{}, err
	}
	if viewer != "" {
		if blocked, berr := social.BlockedEitherWay(ctx, s.pool, viewer, ownerID); berr != nil || blocked {
			return MarketCoursePage{}, ErrCourseUnavailable
		}
	}
	if p.RatingCount > 0 {
		avg := float64(sum) / float64(p.RatingCount)
		p.RatingAverage = &avg
	}
	// The listed version's snapshot, through the same allowlisted builder as the share preview.
	var snapRaw []byte
	draft := Draft{CourseID: courseID, Number: p.Version, LessonCount: p.LessonsTotal}
	if err = s.pool.QueryRow(ctx, `SELECT snapshot FROM publication_drafts WHERE id=$1::uuid OR (course_id=$2 AND number=$3 AND verdict='pass') ORDER BY number DESC LIMIT 1`, nullUUID(draftID), courseID, p.Version).Scan(&snapRaw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MarketCoursePage{}, ErrCourseUnavailable
		}
		return MarketCoursePage{}, err
	}
	var snap Snapshot
	if err = json.Unmarshal(snapRaw, &snap); err != nil {
		return MarketCoursePage{}, err
	}
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM course_copies WHERE source_course_id=$1`, courseID).Scan(&p.EnrollmentCount); err != nil {
		return MarketCoursePage{}, err
	}
	preview := BuildPreviewPayload(snap, draft, p.Creator, p.EnrollmentCount)
	p.Outline, p.SampleSlides = preview.Outline, preview.SampleSlides
	if p.Tags == nil {
		p.Tags = []string{}
	}
	switch {
	case viewer == ownerID:
		p.Viewer.Owner = true
	case viewer != "":
		var copyID string
		err = s.pool.QueryRow(ctx, `SELECT cc.copy_course_id::text FROM course_copies cc JOIN courses c ON c.id=cc.copy_course_id
WHERE cc.learner_id=$1 AND cc.source_course_id=$2 AND c.deleted_at IS NULL`, viewer, courseID).Scan(&copyID)
		if err == nil {
			p.Viewer.Enrolled, p.Viewer.CourseID = true, copyID
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return MarketCoursePage{}, err
		}
	}
	id := viewer
	if id == "" {
		id = anonymousID
	}
	s.emit(id, "course_viewed", map[string]any{"course_id": courseID, "version": p.Version, "surface": "marketplace"})
	return p, nil
}

// nullUUID maps an empty id to a never-matching UUID so the lookup falls back to course and version.
func nullUUID(id string) string {
	if id == "" {
		return "00000000-0000-0000-0000-000000000000"
	}
	return id
}

// EnrollMarketplace is free enrollment from a Course page: the same pinned copy, daily cap and
// zero-credit reuse as a share link, recorded with source "marketplace".
func (s *Service) EnrollMarketplace(ctx context.Context, viewer, courseID string) (Enrollment, error) {
	if !s.cfg.MarketplaceEnabled {
		return Enrollment{}, ErrMarketplaceDisabled
	}
	if !validID(viewer) || !validID(courseID) {
		return Enrollment{}, ErrCourseUnavailable
	}
	var ownerID string
	err := s.pool.QueryRow(ctx, `SELECT l.owner_id::text FROM marketplace_listings l JOIN courses c ON c.id=l.course_id
WHERE l.course_id=$1 AND l.delisted_at IS NULL AND l.hidden_at IS NULL AND c.deleted_at IS NULL AND c.visibility='public'`, courseID).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Enrollment{}, ErrCourseUnavailable
	}
	if err != nil {
		return Enrollment{}, err
	}
	if blocked, berr := social.BlockedEitherWay(ctx, s.pool, viewer, ownerID); berr != nil || blocked {
		return Enrollment{}, ErrCourseUnavailable
	}
	e, err := s.enrollCourse(ctx, viewer, courseID, ownerID, "marketplace")
	if errors.Is(err, ErrLinkNotFound) {
		return Enrollment{}, ErrCourseUnavailable
	}
	return e, err
}

// statsSQL recomputes the derived ranking inputs; rating columns belong to the ratings slice and
// are never written here. Popularity is 30-day enrollments weighted by (0.5 + completion rate),
// where a copy's completion is its passed Lessons over its Lessons.
const statsSQL = `INSERT INTO marketplace_listing_stats(course_id, enrollments_30d, completion_rate, popular_score, refreshed_at)
SELECT l.course_id, COALESCE(a.n,0), COALESCE(a.rate,0), COALESCE(a.n,0) * (0.5 + COALESCE(a.rate,0)), now()
FROM marketplace_listings l
LEFT JOIN LATERAL (
  SELECT count(*)::int AS n, avg(p.done::float8 / NULLIF(p.total,0)) AS rate
  FROM course_copies cc
  CROSS JOIN LATERAL (
    SELECT count(*) FILTER (WHERE st.status='done') AS done, count(*) AS total
    FROM course_units u JOIN course_lesson_stubs st ON st.unit_id=u.id WHERE u.course_id=cc.copy_course_id) p
  WHERE cc.source_course_id=l.course_id AND cc.created_at > now() - interval '30 days') a ON true
WHERE l.delisted_at IS NULL AND ($1::uuid IS NULL OR l.course_id=$1::uuid)
ON CONFLICT (course_id) DO UPDATE SET enrollments_30d=EXCLUDED.enrollments_30d, completion_rate=EXCLUDED.completion_rate,
  popular_score=EXCLUDED.popular_score, refreshed_at=EXCLUDED.refreshed_at`

// RefreshStats recomputes ranking inputs for every active listing and clears cached pages.
func (s *Service) RefreshStats(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, statsSQL, nil); err != nil {
		return err
	}
	s.invalidateCatalogue(ctx)
	return nil
}

// refreshListingStats is the best-effort single-course refresh after an enrollment or listing.
func (s *Service) refreshListingStats(ctx context.Context, courseID string) {
	if _, err := s.pool.Exec(ctx, statsSQL, courseID); err != nil {
		slog.WarnContext(ctx, "marketplace stats refresh failed", "error", err)
	}
}
