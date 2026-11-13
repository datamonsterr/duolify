package sharing_test

import (
	"context"
	"encoding/base64"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
)

const sampleID = "a1111111-1111-4111-8111-111111111111"

func TestCursorRoundTripsAndRejectsForgery(t *testing.T) {
	at := time.Date(2026, 11, 2, 10, 30, 0, 123456000, time.UTC)
	c := sharing.Cursor{Sort: "rating", Key: 4.5769230769, Tie: 12, At: at, ID: sampleID}
	raw := sharing.EncodeCursor(c)
	back, err := sharing.DecodeCursor(raw, "rating")
	if err != nil || back.Key != c.Key || back.Tie != 12 || !back.At.Equal(at) || back.ID != sampleID {
		t.Fatalf("round trip = %+v, %v", back, err)
	}
	bad := map[string]string{
		"other sort":  raw,
		"garbage":     "%%%",
		"not base64":  "not-a-cursor",
		"empty json":  base64.RawURLEncoding.EncodeToString([]byte(`{}`)),
		"bad uuid":    base64.RawURLEncoding.EncodeToString([]byte(`{"s":"rating","k":1,"t":0,"a":0,"i":"x'; DROP TABLE x"}`)),
		"nan":         base64.RawURLEncoding.EncodeToString([]byte(`{"s":"rating","k":"NaN","t":0,"a":0,"i":"` + sampleID + `"}`)),
		"huge":        strings.Repeat("A", 4096),
		"extra field": base64.RawURLEncoding.EncodeToString([]byte(`{"s":"rating","k":1,"t":0,"a":0,"i":"` + sampleID + `","sql":"1=1"}`)),
	}
	for name, v := range bad {
		sort := "rating"
		if name == "other sort" {
			sort = "new"
		}
		if _, err := sharing.DecodeCursor(v, sort); !errors.Is(err, sharing.ErrInvalidCursor) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := sharing.DecodeCursor("", "new"); err != nil {
		t.Fatalf("an empty cursor is the first page: %v", err)
	}
}

func TestBayesianRatingAcceptsZeroRatings(t *testing.T) {
	cases := []struct {
		name       string
		sum, count int64
		mean       float64
		min        int
		want       float64
	}{
		{"zero ratings rank at the platform mean", 0, 0, 4.2, 10, 4.2},
		{"one perfect rating is pulled to the mean", 5, 1, 4.0, 10, (10*4.0 + 5) / 11},
		{"many ratings approach their own average", 450, 100, 3.0, 10, (10*3.0 + 450) / 110},
		{"zero minimum is the raw average", 9, 2, 3.0, 0, 4.5},
		{"zero minimum with no ratings falls back to the mean", 0, 0, 3.5, 0, 3.5},
	}
	for _, tc := range cases {
		if got := sharing.BayesianRating(tc.sum, tc.count, tc.mean, tc.min); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("%s = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestNormalizeBrowseDefaultsClampsAndValidates(t *testing.T) {
	cfg := sharing.DefaultConfig()
	got, err := sharing.NormalizeBrowse(sharing.BrowseQuery{Q: "  rust  ", Category: "tech", Language: "EN"}, cfg)
	if err != nil || got.Q != "rust" || got.Sort != "popular" || got.Language != "en" || got.Limit != cfg.MarketplacePageSize {
		t.Fatalf("defaults = %+v, %v", got, err)
	}
	if got, _ := sharing.NormalizeBrowse(sharing.BrowseQuery{Limit: 10_000}, cfg); got.Limit != cfg.MarketplaceMaxPageSize {
		t.Fatalf("limit not clamped: %d", got.Limit)
	}
	if got, _ := sharing.NormalizeBrowse(sharing.BrowseQuery{Limit: -3}, cfg); got.Limit != cfg.MarketplacePageSize {
		t.Fatalf("negative limit: %d", got.Limit)
	}
	runes := strings.Repeat("é", cfg.MarketplaceMaxQueryLength)
	if _, err := sharing.NormalizeBrowse(sharing.BrowseQuery{Q: runes}, cfg); err != nil {
		t.Fatalf("a query at the limit counts runes, not bytes: %v", err)
	}
	for name, q := range map[string]sharing.BrowseQuery{
		"too long":          {Q: runes + "é"},
		"control character": {Q: "ru\x00st"},
		"bad sort":          {Sort: "random"},
		"bad category":      {Category: "Tech!"},
		"bad language":      {Language: "english"},
		"language region":   {Language: "en-US"},
	} {
		if _, err := sharing.NormalizeBrowse(q, cfg); !errors.Is(err, sharing.ErrInvalidQuery) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if len(sharing.Categories) != 5 || !sharing.ValidCategory("life_skills") || sharing.ValidCategory("other") {
		t.Fatalf("taxonomy = %v", sharing.Categories)
	}
}

func TestEvaluateEligibilityNamesEveryFailedCondition(t *testing.T) {
	cfg := sharing.DefaultConfig()
	ok := sharing.OwnerFacts{EmailVerified: true, AccountAge: 3 * 24 * time.Hour, GeneratedLessons: 3, Title: "T", Description: "D", Category: "tech", Language: "en", Tags: []string{"a"}}
	if r := sharing.EvaluateEligibility(ok, cfg); len(r) != 0 {
		t.Fatalf("exactly at every threshold must pass: %v", r)
	}
	cases := []struct {
		name   string
		mutate func(*sharing.OwnerFacts)
		want   string
	}{
		{"unverified", func(f *sharing.OwnerFacts) { f.EmailVerified = false }, "email_not_verified"},
		{"young", func(f *sharing.OwnerFacts) { f.AccountAge = 3*24*time.Hour - time.Second }, "account_too_new"},
		{"two lessons", func(f *sharing.OwnerFacts) { f.GeneratedLessons = 2 }, "too_few_lessons"},
		{"no title", func(f *sharing.OwnerFacts) { f.Title = "  " }, "missing_title"},
		{"no description", func(f *sharing.OwnerFacts) { f.Description = "" }, "missing_description"},
		{"no category", func(f *sharing.OwnerFacts) { f.Category = "" }, "missing_category"},
		{"bad category", func(f *sharing.OwnerFacts) { f.Category = "memes" }, "invalid_category"},
		{"no language", func(f *sharing.OwnerFacts) { f.Language = "" }, "missing_language"},
		{"bad language", func(f *sharing.OwnerFacts) { f.Language = "klingon!" }, "invalid_language"},
		{"six tags", func(f *sharing.OwnerFacts) { f.Tags = []string{"a", "b", "c", "d", "e", "f"} }, "too_many_tags"},
		{"empty tag", func(f *sharing.OwnerFacts) { f.Tags = []string{" "} }, "invalid_tag"},
		{"long tag", func(f *sharing.OwnerFacts) { f.Tags = []string{strings.Repeat("x", 41)} }, "invalid_tag"},
		{"listing cap", func(f *sharing.OwnerFacts) { f.OtherListings = cfg.MarketplaceMaxListingsPerOwner }, "listing_cap"},
	}
	for _, tc := range cases {
		f := ok
		tc.mutate(&f)
		if r := sharing.EvaluateEligibility(f, cfg); len(r) != 1 || r[0] != tc.want {
			t.Errorf("%s = %v, want [%s]", tc.name, r, tc.want)
		}
	}
	// The caps are configuration, not constants.
	loose := cfg
	loose.MarketplaceMinLessons, loose.MarketplaceMinAccountAgeDays = 1, 0
	young := ok
	young.GeneratedLessons, young.AccountAge = 1, time.Hour
	if r := sharing.EvaluateEligibility(young, loose); len(r) != 0 {
		t.Fatalf("configured caps ignored: %v", r)
	}
}

func TestMarketplaceConfigDefaultsOffAndIgnoresBadValues(t *testing.T) {
	def := sharing.DefaultConfig()
	if def.MarketplaceEnabled {
		t.Fatal("the release flag must default to OFF")
	}
	if def.MarketplaceMinAccountAgeDays != 3 || def.MarketplaceMinLessons != 3 || def.MarketplaceMaxTags != 5 || def.MarketplacePageSize != 20 || def.MarketplaceMaxPageSize != 50 {
		t.Fatalf("defaults = %+v", def)
	}
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if c := sharing.ConfigFromEnv(env(map[string]string{})); c.MarketplaceEnabled {
		t.Fatal("unset flag must stay off")
	}
	for _, v := range []string{"1", "yes", "TRUE", "on", ""} {
		if c := sharing.ConfigFromEnv(env(map[string]string{"MARKETPLACE_ENABLED": v})); c.MarketplaceEnabled {
			t.Errorf("%q must not enable the flag", v)
		}
	}
	c := sharing.ConfigFromEnv(env(map[string]string{
		"MARKETPLACE_ENABLED": "true", "MARKETPLACE_MIN_ACCOUNT_AGE_DAYS": "7", "MARKETPLACE_MIN_LESSONS": "5",
		"MARKETPLACE_PAGE_SIZE": "100", "MARKETPLACE_MAX_PAGE_SIZE": "40", "MARKETPLACE_RATING_MIN_COUNT": "-1",
		"MARKETPLACE_RATING_PRIOR": "9", "MARKETPLACE_CACHE_TTL_SECONDS": "abc",
	}))
	if !c.MarketplaceEnabled || c.MarketplaceMinAccountAgeDays != 7 || c.MarketplaceMinLessons != 5 {
		t.Fatalf("valid values ignored: %+v", c)
	}
	if c.MarketplaceMaxPageSize != 40 || c.MarketplacePageSize > c.MarketplaceMaxPageSize {
		t.Fatalf("page size must never exceed the maximum: %d/%d", c.MarketplacePageSize, c.MarketplaceMaxPageSize)
	}
	if c.MarketplaceRatingMinCount != def.MarketplaceRatingMinCount || c.MarketplaceRatingPrior != def.MarketplaceRatingPrior || c.MarketplaceCacheTTLSeconds != def.MarketplaceCacheTTLSeconds {
		t.Fatalf("invalid values must keep the default: %+v", c)
	}
}

// fakeCatalogue records calls so the handler tests prove what is (not) reached.
type fakeCatalogue struct {
	enabled bool
	limit   error
	browse  func(sharing.BrowseQuery) (sharing.Page, error)
	page    error
	enroll  error
	calls   []string
	viewer  string
	query   sharing.BrowseQuery
}

func (f *fakeCatalogue) MarketplaceEnabled() bool            { return f.enabled }
func (f *fakeCatalogue) AllowPublicRead(*http.Request) error { return f.limit }
func (f *fakeCatalogue) RetryAfterSeconds() int              { return 60 }
func (f *fakeCatalogue) Browse(_ context.Context, viewer string, q sharing.BrowseQuery) (sharing.Page, error) {
	f.calls, f.viewer, f.query = append(f.calls, "browse"), viewer, q
	if f.browse != nil {
		return f.browse(q)
	}
	return sharing.Page{Items: []sharing.CatalogueItem{}}, nil
}
func (f *fakeCatalogue) MarketCourse(_ context.Context, viewer, id string) (sharing.MarketCoursePage, error) {
	f.calls, f.viewer = append(f.calls, "course"), viewer
	return sharing.MarketCoursePage{CourseID: id}, f.page
}
func (f *fakeCatalogue) EnrollMarketplace(_ context.Context, viewer, id string) (sharing.Enrollment, error) {
	f.calls, f.viewer = append(f.calls, "enroll"), viewer
	return sharing.Enrollment{CourseID: "c", Created: true}, f.enroll
}

func marketCall(h http.Handler, actor, method, path string) *httptest.ResponseRecorder {
	ctx := context.Background()
	if actor != "" {
		ctx = accounts.WithLearnerID(ctx, actor)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, method, path, strings.NewReader(`{"learner_id":"someone-else"}`)))
	return rec
}

func marketMux(c sharing.Catalogue) *http.ServeMux {
	m := http.NewServeMux()
	h := sharing.NewMarketHandler(c)
	m.Handle("GET /v1/marketplace/courses", h)
	m.Handle("GET /v1/marketplace/courses/{id}", h)
	m.Handle("POST /v1/marketplace/courses/{id}/enroll", h)
	return m
}

func TestMarketHandlerFlagOffIsNeutralAndNeverReachesTheCatalogue(t *testing.T) {
	f := &fakeCatalogue{enabled: false}
	m := marketMux(f)
	for _, c := range [][2]string{{"GET", "/v1/marketplace/courses"}, {"GET", "/v1/marketplace/courses/" + sampleID}, {"POST", "/v1/marketplace/courses/" + sampleID + "/enroll"}} {
		rec := marketCall(m, sampleID, c[0], c[1])
		if rec.Code != 404 || strings.TrimSpace(rec.Body.String()) != `{"error":"marketplace_unavailable"}` {
			t.Errorf("%v = %d %s", c, rec.Code, rec.Body)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("a disabled marketplace reached the catalogue: %v", f.calls)
	}
}

func TestMarketHandlerAnonymousReadsAreLimitedNoindexAndPassQueryThrough(t *testing.T) {
	f := &fakeCatalogue{enabled: true}
	m := marketMux(f)
	rec := marketCall(m, "", "GET", "/v1/marketplace/courses?q=rust%20ownership&category=tech&language=es&sort=rating&cursor=abc&limit=7")
	if rec.Code != 200 || rec.Header().Get("X-Robots-Tag") != "noindex" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("browse = %d %v", rec.Code, rec.Header())
	}
	if want := (sharing.BrowseQuery{Q: "rust ownership", Category: "tech", Language: "es", Sort: "rating", Cursor: "abc", Limit: 7}); f.query != want || f.viewer != "" {
		t.Fatalf("query/viewer = %+v / %q", f.query, f.viewer)
	}
	f.limit = sharing.ErrRateLimited
	f.calls = nil
	rec = marketCall(m, "", "GET", "/v1/marketplace/courses")
	if rec.Code != 429 || rec.Header().Get("Retry-After") != "60" || len(f.calls) != 0 {
		t.Fatalf("limited = %d %v calls=%v", rec.Code, rec.Header(), f.calls)
	}
}

func TestMarketHandlerMapsErrorsNeutrallyAndTakesTheActorFromTheSession(t *testing.T) {
	f := &fakeCatalogue{enabled: true}
	m := marketMux(f)
	for err, want := range map[error][2]string{
		sharing.ErrInvalidCursor:                 {"400", "invalid_cursor"},
		sharing.ErrInvalidQuery:                  {"400", "invalid_query"},
		errors.New("pq: secret password in dsn"): {"503", "sharing_unavailable"},
	} {
		f.browse = func(sharing.BrowseQuery) (sharing.Page, error) { return sharing.Page{}, err }
		rec := marketCall(m, "", "GET", "/v1/marketplace/courses?q=secretword")
		if got := rec.Code; got != map[string]int{"400": 400, "503": 503}[want[0]] || !strings.Contains(rec.Body.String(), want[1]) {
			t.Errorf("%v = %d %s", err, rec.Code, rec.Body)
		}
		if strings.Contains(rec.Body.String(), "password") || strings.Contains(rec.Body.String(), "secretword") {
			t.Errorf("error leaked internals or the query: %s", rec.Body)
		}
	}
	// A missing, delisted, or block-hidden course is the same 404; a bad id never reaches the catalogue.
	f.calls = nil
	f.page = sharing.ErrCourseUnavailable
	if rec := marketCall(m, "", "GET", "/v1/marketplace/courses/"+sampleID); rec.Code != 404 || strings.TrimSpace(rec.Body.String()) != `{"error":"course_unavailable"}` {
		t.Fatalf("unavailable = %d %s", rec.Code, rec.Body)
	}
	f.calls = nil
	if rec := marketCall(m, "", "GET", "/v1/marketplace/courses/not-a-uuid"); rec.Code != 404 || len(f.calls) != 0 {
		t.Fatalf("bad id = %d calls=%v", rec.Code, f.calls)
	}
	// Enroll: signed in only, and the actor is the session's, never the body's.
	if rec := marketCall(m, "", "POST", "/v1/marketplace/courses/"+sampleID+"/enroll"); rec.Code != 401 {
		t.Fatalf("anonymous enroll = %d", rec.Code)
	}
	f.calls = nil
	if rec := marketCall(m, sampleID, "POST", "/v1/marketplace/courses/"+sampleID+"/enroll"); rec.Code != 201 || f.viewer != sampleID {
		t.Fatalf("enroll = %d viewer=%q", rec.Code, f.viewer)
	}
	f.enroll = sharing.ErrRateLimited
	if rec := marketCall(m, sampleID, "POST", "/v1/marketplace/courses/"+sampleID+"/enroll"); rec.Code != 429 {
		t.Fatalf("daily cap = %d", rec.Code)
	}
	f.enroll = sharing.ErrCourseUnavailable
	if rec := marketCall(m, sampleID, "POST", "/v1/marketplace/courses/"+sampleID+"/enroll"); rec.Code != 404 {
		t.Fatalf("delisted enroll = %d", rec.Code)
	}
}
