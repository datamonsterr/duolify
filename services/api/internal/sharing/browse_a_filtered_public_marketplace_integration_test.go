//go:build integration

package sharing_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/cache"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
)

// marketOwner is a signed-up learner who passes every listing check: verified email, 10 days old.
func marketOwner(t *testing.T, f *backuptest.Fixture, name string) string {
	t.Helper()
	id := newLearner(t, f, name)
	pool := f.DB.Pool
	if _, err := pool.Exec(t.Context(), `INSERT INTO auth_identities(email,learner_id) VALUES($1,$2)`, name+"-"+id+"@example.test", id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE learners SET created_at = now() - interval '10 days' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	return id
}

// marketCourse gives owner a course with `generated` generated lessons and one still to generate.
func marketCourse(t *testing.T, f *backuptest.Fixture, ownerID, title, description string, generated int) string {
	t.Helper()
	ctx, pool := t.Context(), f.DB.Pool
	var quote, creation, courseID, unit string
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO pricing_quotes(learner_id,owner_reference,pricing_version,snapshot,created_at,expires_at) SELECT $1::uuid,encode(sha256(convert_to($1::text,'UTF8')),'hex'),version,'{}',now(),now()+interval '1 hour' FROM pricing_quote_policies ORDER BY version LIMIT 1 RETURNING id::text`, ownerID).Scan(&quote))
	must(pool.QueryRow(ctx, `INSERT INTO course_creations(learner_id,draft_id,revision,quote_id,request,state) VALUES($1,gen_random_uuid(),1,$2,'{}','enrolled') RETURNING id::text`, ownerID, quote).Scan(&creation))
	must(pool.QueryRow(ctx, `INSERT INTO courses(learner_id,creation_id,title,description) VALUES($1,$2,$3,$4) RETURNING id::text`, ownerID, creation, title, description).Scan(&courseID))
	_, err := pool.Exec(ctx, `INSERT INTO course_enrollments(learner_id,course_id) VALUES($1,$2)`, ownerID, courseID)
	must(err)
	must(pool.QueryRow(ctx, `INSERT INTO course_units(learner_id,course_id,authored_id,position,title) VALUES($1,$2,'u1',0,'Unit one') RETURNING id::text`, ownerID, courseID).Scan(&unit))
	for i := 0; i < generated; i++ {
		var stub string
		must(pool.QueryRow(ctx, `INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status) VALUES($1,$2,$3,$4,$5,'Goal','done') RETURNING id::text`, ownerID, unit, fmt.Sprintf("l%d", i), i, fmt.Sprintf("Lesson %d", i+1)).Scan(&stub))
		_, err = pool.Exec(ctx, `INSERT INTO generated_lessons(lesson_stub_id,course_id,learner_id,title,manifest) VALUES($1,$2,$3,$4,'{}'::jsonb)`, stub, courseID, ownerID, fmt.Sprintf("Lesson %d", i+1))
		must(err)
		slides := 1
		if i == 0 {
			slides = 4 // the page shows at most three
		}
		for s := 0; s < slides; s++ {
			_, err = pool.Exec(ctx, `INSERT INTO lesson_slides(lesson_stub_id,learner_id,position,slide_id,html,duration_ms,narration) VALUES($1,$2,$3,$4,$5,3000,'Narration.')`, stub, ownerID, s, fmt.Sprintf("s%d-%d", i, s), fmt.Sprintf("<h1>Slide %d.%d</h1><p>Fact.</p>", i, s))
			must(err)
		}
		_, err = pool.Exec(ctx, `INSERT INTO lesson_questions(lesson_stub_id,learner_id,position,topic_id,prompt,choices,answer_index,explanation) VALUES($1,$2,0,'t','Which one?','["Right","Wrong"]'::jsonb,0,'SECRET-EXPLANATION')`, stub, ownerID)
		must(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status) VALUES($1,$2,'lx',$3,'Later lesson','Goal','locked')`, ownerID, unit, generated)
	must(err)
	return courseID
}

// listCourse screens a fresh draft and lists the course; it fails the test unless that works.
func listCourse(t *testing.T, s *sharing.Service, ownerID, courseID, category, language string, tags ...string) sharing.Listing {
	t.Helper()
	if d, err := s.BuildDraft(t.Context(), ownerID, courseID); err != nil || d.Verdict != sharing.VerdictPass {
		t.Fatalf("draft: %v %+v", err, d)
	}
	l, err := s.ListCourse(t.Context(), ownerID, courseID, sharing.ListingInput{Category: category, Language: language, Tags: tags})
	if err != nil {
		t.Fatalf("list %s: %v", courseID, err)
	}
	return l
}

func marketService(t *testing.T, f *backuptest.Fixture, ev *capture, redisClient *redis.Client, mutate func(*sharing.Config)) *sharing.Service {
	t.Helper()
	s, _ := enrollService(t, f, ev, func(c *sharing.Config) {
		c.MarketplaceEnabled = true
		c.MarketplacePageSize = 3
		c.MarketplaceRatingMinCount = 10
		if mutate != nil {
			mutate(c)
		}
	})
	if redisClient != nil {
		s = s.WithMarketplaceCache(redisClient)
	}
	return s
}

func startRedis(t *testing.T) *redis.Client {
	t.Helper()
	ctx := t.Context()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{Image: "redis:7-alpine", ExposedPorts: []string{"6379/tcp"}, WaitingFor: wait.ForLog("Ready to accept connections")},
		Started:          true,
	})
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)
	endpoint, err := container.Endpoint(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	client, err := cache.New("redis://" + endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func browseTitles(t *testing.T, s *sharing.Service, viewer string, q sharing.BrowseQuery) ([]string, string) {
	t.Helper()
	page, err := s.Browse(t.Context(), viewer, q)
	if err != nil {
		t.Fatalf("browse %+v: %v", q, err)
	}
	out := make([]string, 0, len(page.Items))
	for _, it := range page.Items {
		out = append(out, it.Title)
	}
	return out, page.NextCursor
}

// walk follows every cursor and returns the titles in the order they were served.
func walk(t *testing.T, s *sharing.Service, viewer string, q sharing.BrowseQuery) []string {
	t.Helper()
	var all []string
	for i := 0; i < 20; i++ {
		titles, next := browseTitles(t, s, viewer, q)
		all = append(all, titles...)
		if next == "" {
			return all
		}
		q.Cursor = next
	}
	t.Fatal("cursor never ended")
	return nil
}

func TestBrowseAFilteredPublicMarketplace(t *testing.T) {
	f := backuptest.New(t)
	ctx := t.Context()
	ev := &capture{}
	s := marketService(t, f, ev, nil, nil)

	t.Run("flag_off_lists_nothing_and_hides_every_route", func(t *testing.T) {
		off, _ := enrollService(t, f, ev, nil) // DefaultConfig: flag OFF
		o := marketOwner(t, f, "flagoff")
		c := marketCourse(t, f, o, "Flag off course", "d", 3)
		if _, err := off.BuildDraft(ctx, o, c); err != nil {
			t.Fatal(err)
		}
		if _, err := off.ListCourse(ctx, o, c, sharing.ListingInput{Category: "tech", Language: "en"}); !errors.Is(err, sharing.ErrMarketplaceDisabled) {
			t.Fatalf("list with the flag off = %v", err)
		}
		if _, err := off.Browse(ctx, "", sharing.BrowseQuery{}); !errors.Is(err, sharing.ErrMarketplaceDisabled) {
			t.Fatalf("browse with the flag off = %v", err)
		}
		if n := count(t, f, `SELECT count(*) FROM marketplace_listings WHERE course_id=$1`, c); n != 0 {
			t.Fatalf("flag off wrote %d listings", n)
		}
		if off.MarketplaceEnabled() || !s.MarketplaceEnabled() {
			t.Fatal("MarketplaceEnabled does not follow the config")
		}
	})

	t.Run("eligibility_requires_every_condition_with_closed_reason_codes", func(t *testing.T) {
		reasons := func(owner, course string, in sharing.ListingInput) []string {
			t.Helper()
			var ne *sharing.NotEligibleError
			_, err := s.ListCourse(ctx, owner, course, in)
			if !errors.As(err, &ne) {
				t.Fatalf("want NotEligibleError, got %v", err)
			}
			return ne.Reasons
		}
		ok := sharing.ListingInput{Category: "tech", Language: "en", Tags: []string{"go"}}

		young := marketOwner(t, f, "young")
		if _, err := f.DB.Pool.Exec(ctx, `UPDATE learners SET created_at = now() - interval '2 days' WHERE id=$1`, young); err != nil {
			t.Fatal(err)
		}
		c := marketCourse(t, f, young, "Young owner", "d", 3)
		if _, err := s.BuildDraft(ctx, young, c); err != nil {
			t.Fatal(err)
		}
		if got := reasons(young, c, ok); strings.Join(got, ",") != "account_too_new" {
			t.Fatalf("young = %v", got)
		}

		unverified := newLearner(t, f, "unverified")
		if _, err := f.DB.Pool.Exec(ctx, `UPDATE learners SET created_at = now() - interval '10 days' WHERE id=$1`, unverified); err != nil {
			t.Fatal(err)
		}
		c = marketCourse(t, f, unverified, "Unverified owner", "d", 3)
		if _, err := s.BuildDraft(ctx, unverified, c); err != nil {
			t.Fatal(err)
		}
		if got := reasons(unverified, c, ok); strings.Join(got, ",") != "email_not_verified" {
			t.Fatalf("unverified = %v", got)
		}

		o := marketOwner(t, f, "few")
		few := marketCourse(t, f, o, "Two lessons only", "d", 2)
		if _, err := s.BuildDraft(ctx, o, few); err != nil {
			t.Fatal(err)
		}
		if got := reasons(o, few, ok); strings.Join(got, ",") != "too_few_lessons" {
			t.Fatalf("few lessons = %v", got)
		}

		o2 := marketOwner(t, f, "fields")
		c = marketCourse(t, f, o2, "Fields course", "", 3)
		if _, err := s.BuildDraft(ctx, o2, c); err != nil {
			t.Fatal(err)
		}
		got := reasons(o2, c, sharing.ListingInput{Category: "nonsense", Language: "english please", Tags: []string{"a", "b", "c", "d", "e", "f"}})
		for _, want := range []string{"missing_description", "invalid_category", "invalid_language", "too_many_tags"} {
			if !strings.Contains(strings.Join(got, ","), want) {
				t.Fatalf("missing %s in %v", want, got)
			}
		}

		// No passing draft, or a stranger, is the neutral owner-only refusal; nothing was listed.
		o3 := marketOwner(t, f, "nodraft")
		c = marketCourse(t, f, o3, "No draft", "d", 3)
		if _, err := s.ListCourse(ctx, o3, c, ok); !errors.Is(err, sharing.ErrPublishNotAllowed) {
			t.Fatalf("no draft = %v", err)
		}
		if _, err := s.ListCourse(ctx, o, c, ok); !errors.Is(err, sharing.ErrUnavailable) {
			t.Fatalf("stranger = %v", err)
		}
		if n := count(t, f, `SELECT count(*) FROM marketplace_listings WHERE owner_id IN ($1,$2,$3,$4,$5)`, young, unverified, o, o2, o3); n != 0 {
			t.Fatalf("ineligible owners were listed: %d", n)
		}
	})

	t.Run("owner_listing_cap_is_configurable", func(t *testing.T) {
		capped := marketService(t, f, ev, nil, func(c *sharing.Config) { c.MarketplaceMaxListingsPerOwner = 1 })
		o := marketOwner(t, f, "capped")
		first := marketCourse(t, f, o, "Cap one", "d", 3)
		second := marketCourse(t, f, o, "Cap two", "d", 3)
		listCourse(t, capped, o, first, "tech", "en")
		if _, err := capped.BuildDraft(ctx, o, second); err != nil {
			t.Fatal(err)
		}
		var ne *sharing.NotEligibleError
		if _, err := capped.ListCourse(ctx, o, second, sharing.ListingInput{Category: "tech", Language: "en"}); !errors.As(err, &ne) || strings.Join(ne.Reasons, ",") != "listing_cap" {
			t.Fatalf("cap = %v", err)
		}
	})

	t.Run("full_text_search_filters_and_tags", func(t *testing.T) {
		o1, o2, o3 := marketOwner(t, f, "s1"), marketOwner(t, f, "s2"), marketOwner(t, f, "s3")
		listCourse(t, s, o1, marketCourse(t, f, o1, "Rust ownership basics fts1", "Borrowing and lifetimes", 3), "tech", "en", "rust")
		listCourse(t, s, o2, marketCourse(t, f, o2, "Aprender programacion fts1", "Propiedad y prestamos de rust", 3), "tech", "es")
		listCourse(t, s, o3, marketCourse(t, f, o3, "Cooking pasta fts1", "Kitchen basics", 3), "life_skills", "en", "food", "Rust-free")

		got, _ := browseTitles(t, s, "", sharing.BrowseQuery{Q: "fts1 rust", Sort: "new"})
		if len(got) != 3 {
			t.Fatalf("title, description and tag matches = %v", got)
		}
		got, _ = browseTitles(t, s, "", sharing.BrowseQuery{Q: "fts1 rust", Language: "en", Sort: "new"})
		if len(got) != 2 {
			t.Fatalf("language en = %v", got)
		}
		got, _ = browseTitles(t, s, "", sharing.BrowseQuery{Q: "fts1", Category: "life_skills"})
		if len(got) != 1 || got[0] != "Cooking pasta fts1" {
			t.Fatalf("category = %v", got)
		}
		// A tag only: not in the title or description.
		got, _ = browseTitles(t, s, "", sharing.BrowseQuery{Q: "food"})
		if len(got) != 1 || got[0] != "Cooking pasta fts1" {
			t.Fatalf("tag match = %v", got)
		}
		// websearch syntax is safe: exclusion works, unbalanced quotes and operators never error.
		got, _ = browseTitles(t, s, "", sharing.BrowseQuery{Q: "fts1 -pasta", Sort: "new"})
		if len(got) != 2 {
			t.Fatalf("exclusion = %v", got)
		}
		for _, hostile := range []string{`"unterminated`, `'; DROP TABLE marketplace_listings; --`, `& | ! ( ) :*`, "fts1 \x00"} {
			if _, err := s.Browse(ctx, "", sharing.BrowseQuery{Q: hostile}); err != nil && !errors.Is(err, sharing.ErrInvalidQuery) {
				t.Fatalf("hostile %q = %v", hostile, err)
			}
		}
		if _, err := s.Browse(ctx, "", sharing.BrowseQuery{Q: strings.Repeat("x", 101)}); !errors.Is(err, sharing.ErrInvalidQuery) {
			t.Fatalf("overlong query = %v", err)
		}
		if _, err := s.Browse(ctx, "", sharing.BrowseQuery{Category: "bogus"}); !errors.Is(err, sharing.ErrInvalidQuery) {
			t.Fatalf("bogus category = %v", err)
		}
		if n := count(t, f, `SELECT count(*) FROM marketplace_listings`); n < 3 {
			t.Fatal("table lost rows to hostile input")
		}
	})

	t.Run("sorts_cursors_and_bayesian_ranking_are_total_and_stable", func(t *testing.T) {
		names := []string{"A", "B", "C", "D", "E", "F", "G"}
		ids := map[string]string{}
		for _, n := range names {
			o := marketOwner(t, f, "rank"+n)
			ids[n] = listCourse(t, s, o, marketCourse(t, f, o, "Ranked "+n+" srt1", "ranking fixture", 3), "science", "en").CourseID
		}
		rate := func(n string, sum, cnt int) {
			if _, err := f.DB.Pool.Exec(ctx, `UPDATE marketplace_listing_stats SET rating_sum=$2, rating_count=$3 WHERE course_id=$1`, ids[n], sum, cnt); err != nil {
				t.Fatal(err)
			}
		}
		// With C=10 and platform mean m = 238/52 = 4.577: C scores 4.789, unrated D..G score m
		// (newest first), A (4.5 over 40) scores 4.515 and B (4.0 over 2) scores 4.481.
		rate("A", 180, 40)
		rate("B", 8, 2)
		rate("C", 50, 10)
		q := sharing.BrowseQuery{Q: "srt1", Sort: "rating", Limit: 3}
		got := walk(t, s, "", q)
		want := []string{"Ranked C srt1", "Ranked G srt1", "Ranked F srt1", "Ranked E srt1", "Ranked D srt1", "Ranked A srt1", "Ranked B srt1"}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Fatalf("rating order\n got %v\nwant %v", got, want)
		}
		// The same order twice: deterministic, and every course exactly once.
		if again := walk(t, s, "", q); strings.Join(again, "|") != strings.Join(got, "|") {
			t.Fatal("rating order changed between walks")
		}

		got = walk(t, s, "", sharing.BrowseQuery{Q: "srt1", Sort: "new", Limit: 2})
		for i, n := range []string{"G", "F", "E", "D", "C", "B", "A"} {
			if got[i] != "Ranked "+n+" srt1" {
				t.Fatalf("new order = %v", got)
			}
		}

		// Popular: 30-day enrollments weighted by completion rate. B has 2 copies with no
		// progress, D has 1 copy that finished everything; E has an old copy outside 30 days.
		learners := []string{newLearner(t, f, "pop1"), newLearner(t, f, "pop2"), newLearner(t, f, "pop3"), newLearner(t, f, "pop4")}
		enroll := func(learner, name string) string {
			e, err := s.EnrollMarketplace(ctx, learner, ids[name])
			if err != nil {
				t.Fatal(err)
			}
			return e.CourseID
		}
		enroll(learners[0], "B")
		enroll(learners[1], "B")
		doneCopy := enroll(learners[2], "D")
		if _, err := f.DB.Pool.Exec(ctx, `UPDATE course_lesson_stubs SET status='done' WHERE unit_id IN (SELECT id FROM course_units WHERE course_id=$1)`, doneCopy); err != nil {
			t.Fatal(err)
		}
		oldCopy := enroll(learners[3], "E")
		if _, err := f.DB.Pool.Exec(ctx, `UPDATE course_copies SET created_at = now() - interval '45 days' WHERE copy_course_id=$1`, oldCopy); err != nil {
			t.Fatal(err)
		}
		if err := s.RefreshStats(ctx); err != nil {
			t.Fatal(err)
		}
		got = walk(t, s, "", sharing.BrowseQuery{Q: "srt1", Sort: "popular", Limit: 3})
		// B: 2 copies * (0.5 + 0) = 1.0; D: 1 copy * (0.5 + 1.0) = 1.5.
		if got[0] != "Ranked D srt1" || got[1] != "Ranked B srt1" {
			t.Fatalf("popular order = %v", got)
		}
		if got[len(got)-1] != "Ranked A srt1" || len(got) != 7 {
			t.Fatalf("popular tail (zero scores newest first, old enrollment ignored) = %v", got)
		}
		var thirty int
		if err := f.DB.Pool.QueryRow(ctx, `SELECT enrollments_30d FROM marketplace_listing_stats WHERE course_id=$1`, ids["E"]).Scan(&thirty); err != nil || thirty != 0 {
			t.Fatalf("enrollments_30d for an old copy = %d, %v", thirty, err)
		}

		// A cursor belongs to its sort and cannot be forged or truncated.
		_, next := browseTitles(t, s, "", sharing.BrowseQuery{Q: "srt1", Sort: "rating", Limit: 2})
		for _, bad := range []string{"not-a-cursor", next[:len(next)/2], next + "x"} {
			if _, err := s.Browse(ctx, "", sharing.BrowseQuery{Q: "srt1", Sort: "rating", Cursor: bad}); !errors.Is(err, sharing.ErrInvalidCursor) {
				t.Fatalf("cursor %q = %v", bad, err)
			}
		}
		if _, err := s.Browse(ctx, "", sharing.BrowseQuery{Q: "srt1", Sort: "new", Cursor: next}); !errors.Is(err, sharing.ErrInvalidCursor) {
			t.Fatalf("rating cursor on new sort = %v", err)
		}
		// Items carry aggregates only.
		page, _ := s.Browse(ctx, "", sharing.BrowseQuery{Q: "srt1", Sort: "rating", Limit: 1})
		raw, _ := json.Marshal(page)
		for _, banned := range []string{"learner_id", "owner_id", "email", "SECRET"} {
			if strings.Contains(string(raw), banned) {
				t.Fatalf("page leaks %s: %s", banned, raw)
			}
		}
		if it := page.Items[0]; it.EnrollmentCount != 0 || it.RatingCount != 10 || it.RatingAverage == nil || *it.RatingAverage != 5 || it.LessonsReady != 3 || it.LessonsTotal != 4 {
			t.Fatalf("item = %+v", it)
		}
	})

	t.Run("blocked_pairs_are_hidden_from_each_other_when_signed_in", func(t *testing.T) {
		owner1, owner2 := marketOwner(t, f, "blk1"), marketOwner(t, f, "blk2")
		listCourse(t, s, owner1, marketCourse(t, f, owner1, "Blocked owner course blk1", "d", 3), "arts", "en")
		listCourse(t, s, owner2, marketCourse(t, f, owner2, "Visible course blk1", "d", 3), "arts", "en")
		viewer, bystander := newLearner(t, f, "blkviewer"), newLearner(t, f, "blkby")
		insertBlock := func(blocker, blocked string) {
			if _, err := f.DB.Pool.Exec(ctx, `INSERT INTO learner_blocks(blocker_id,blocked_id) VALUES($1,$2)`, blocker, blocked); err != nil {
				t.Fatal(err)
			}
		}
		insertBlock(viewer, owner1) // the viewer blocked the owner
		insertBlock(owner2, bystander)
		q := sharing.BrowseQuery{Q: "blk1", Sort: "new"}
		if got, _ := browseTitles(t, s, viewer, q); strings.Join(got, "|") != "Visible course blk1" {
			t.Fatalf("viewer blocked owner1 = %v", got)
		}
		if got, _ := browseTitles(t, s, bystander, q); strings.Join(got, "|") != "Blocked owner course blk1" {
			t.Fatalf("owner2 blocked the bystander = %v", got)
		}
		if got, _ := browseTitles(t, s, "", q); len(got) != 2 {
			t.Fatalf("anonymous = %v", got)
		}
		var id string
		if err := f.DB.Pool.QueryRow(ctx, `SELECT course_id::text FROM marketplace_listings WHERE owner_id=$1`, owner1).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := s.MarketCourse(ctx, viewer, id); !errors.Is(err, sharing.ErrCourseUnavailable) {
			t.Fatalf("blocked course page = %v", err)
		}
		if _, err := s.EnrollMarketplace(ctx, viewer, id); !errors.Is(err, sharing.ErrCourseUnavailable) {
			t.Fatalf("blocked enroll = %v", err)
		}
		if _, err := s.MarketCourse(ctx, "", id); err != nil {
			t.Fatalf("anonymous page = %v", err)
		}
	})

	t.Run("course_page_has_attribution_safe_preview_and_ready_versus_total", func(t *testing.T) {
		o := marketOwner(t, f, "page")
		if _, err := f.DB.Pool.Exec(ctx, `UPDATE learners SET display_name='Mina Page' WHERE id=$1`, o); err != nil {
			t.Fatal(err)
		}
		course := marketCourse(t, f, o, "Page course pg1", "A page description", 3)
		l := listCourse(t, s, o, course, "tech", "en", "go", "queues")
		ev.reset()
		viewer := newLearner(t, f, "pageviewer")
		p, err := s.MarketCourse(ctx, viewer, l.CourseID)
		if err != nil {
			t.Fatal(err)
		}
		if p.Title != "Page course pg1" || p.Description != "A page description" || p.Category != "tech" || p.Language != "en" || p.Version != 1 || p.PublishedAt.IsZero() {
			t.Fatalf("page = %+v", p)
		}
		if p.Creator.DisplayName != "Mina Page" || p.LessonsReady != 3 || p.LessonsTotal != 4 || p.EnrollmentCount != 0 || p.RatingCount != 0 || p.RatingAverage != nil {
			t.Fatalf("page facts = %+v", p)
		}
		if len(p.SampleSlides) != 3 || len(p.Outline) != 1 || len(p.Outline[0].Lessons) != 4 {
			t.Fatalf("preview/outline = %d slides, %+v", len(p.SampleSlides), p.Outline)
		}
		if p.Viewer.Enrolled {
			t.Fatal("not enrolled yet")
		}
		raw, _ := json.Marshal(p)
		for _, banned := range []string{"SECRET-EXPLANATION", "answer_index", "narration", "learner_id", "owner_id", "Which one?"} {
			if strings.Contains(string(raw), banned) {
				t.Fatalf("page leaks %q", banned)
			}
		}
		views := ev.named("course_viewed")
		if len(views) != 1 || views[0]["course_id"] != l.CourseID || views[0]["version"] != 1 || views[0]["surface"] != "marketplace" {
			t.Fatalf("course_viewed = %+v", views)
		}
		// Enrollment from the page: one pinned copy, source marketplace, zero credits.
		e, err := s.EnrollMarketplace(ctx, viewer, l.CourseID)
		if err != nil || !e.Created || e.ReadyLessons != 3 || e.TotalLessons != 4 || e.NeedsGenerationLessons != 1 {
			t.Fatalf("enroll = %+v, %v", e, err)
		}
		if again, err := s.EnrollMarketplace(ctx, viewer, l.CourseID); err != nil || again.Created || again.CourseID != e.CourseID {
			t.Fatalf("repeat enroll = %+v, %v", again, err)
		}
		if n := count(t, f, `SELECT count(*) FROM course_copies WHERE learner_id=$1 AND source='marketplace' AND source_course_id=$2`, viewer, l.CourseID); n != 1 {
			t.Fatalf("marketplace copies = %d", n)
		}
		if n := count(t, f, `SELECT count(*) FROM credit_entries WHERE learner_id=$1`, viewer); n != 0 {
			t.Fatalf("free enrollment wrote %d credit entries", n)
		}
		if ev := ev.named("course_enrolled_published"); len(ev) != 1 || ev[0]["source"] != "marketplace" {
			t.Fatalf("enroll event = %+v", ev)
		}
		p, _ = s.MarketCourse(ctx, viewer, l.CourseID)
		if !p.Viewer.Enrolled || p.Viewer.CourseID != e.CourseID || p.EnrollmentCount != 1 {
			t.Fatalf("viewer state = %+v count %d", p.Viewer, p.EnrollmentCount)
		}
		// The owner sees their own page as enrolled (no copy of your own course).
		if own, err := s.MarketCourse(ctx, o, l.CourseID); err != nil || !own.Viewer.Owner {
			t.Fatalf("owner page = %+v, %v", own, err)
		}
	})

	t.Run("delist_removes_course_everywhere_and_restores_link_visibility", func(t *testing.T) {
		o := marketOwner(t, f, "delist")
		course := marketCourse(t, f, o, "Delist me dl1", "d", 3)
		l := listCourse(t, s, o, course, "arts", "en")
		if v := visibility(t, f, course); v != "public" {
			t.Fatalf("listed visibility = %s", v)
		}
		token, err := s.PublishLink(ctx, o, course)
		if err != nil || token == "" {
			t.Fatal(err)
		}
		if err := s.DelistCourse(ctx, o, course); err != nil {
			t.Fatal(err)
		}
		if got, _ := browseTitles(t, s, "", sharing.BrowseQuery{Q: "dl1"}); len(got) != 0 {
			t.Fatalf("delisted course still listed: %v", got)
		}
		if _, err := s.MarketCourse(ctx, "", l.CourseID); !errors.Is(err, sharing.ErrCourseUnavailable) {
			t.Fatalf("delisted page = %v", err)
		}
		viewer := newLearner(t, f, "delviewer")
		if _, err := s.EnrollMarketplace(ctx, viewer, l.CourseID); !errors.Is(err, sharing.ErrCourseUnavailable) {
			t.Fatalf("delisted enroll = %v", err)
		}
		if v := visibility(t, f, course); v != "link" {
			t.Fatalf("delisted with a live link should be link, got %s", v)
		}
		if _, err := s.GetPreview(ctx, token, ""); err != nil {
			t.Fatalf("the share link keeps working: %v", err)
		}
		// Idempotent, owner-only, and relist works after an eligible republish.
		if err := s.DelistCourse(ctx, o, course); err != nil {
			t.Fatalf("second delist = %v", err)
		}
		if err := s.DelistCourse(ctx, newLearner(t, f, "delother"), course); !errors.Is(err, sharing.ErrUnavailable) {
			t.Fatalf("stranger delist = %v", err)
		}
		listCourse(t, s, o, course, "arts", "en")
		if got, _ := browseTitles(t, s, "", sharing.BrowseQuery{Q: "dl1"}); len(got) != 1 {
			t.Fatalf("relisted = %v", got)
		}
	})

	t.Run("analytics_carry_enums_ids_and_query_length_never_text", func(t *testing.T) {
		secret := "zebra-secret-query"
		optedOut := newLearner(t, f, "optout")
		consent(t, f, optedOut, true)
		viewer := newLearner(t, f, "evviewer")
		ev.reset()
		if _, err := s.Browse(ctx, viewer, sharing.BrowseQuery{Category: "tech", Language: "en", Sort: "popular"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Browse(ctx, viewer, sharing.BrowseQuery{Q: secret, Sort: "rating"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Browse(ctx, optedOut, sharing.BrowseQuery{Q: secret}); err != nil {
			t.Fatal(err)
		}
		browsed, searched := ev.named("marketplace_browsed"), ev.named("marketplace_searched")
		if len(browsed) != 1 || browsed[0]["sort"] != "popular" || browsed[0]["category"] != "tech" || browsed[0]["_id"] != viewer {
			t.Fatalf("browsed = %+v", browsed)
		}
		if len(searched) != 1 || searched[0]["query_length"] != len(secret) || searched[0]["sort"] != "rating" {
			t.Fatalf("searched = %+v (the opted-out search must send nothing)", searched)
		}
		raw, _ := json.Marshal(ev.snapshot())
		if strings.Contains(string(raw), secret) {
			t.Fatalf("query text reached analytics: %s", raw)
		}
	})

	t.Run("owner_deletion_cascades_listing_and_stats", func(t *testing.T) {
		o := marketOwner(t, f, "gone")
		l := listCourse(t, s, o, marketCourse(t, f, o, "Gone course gn1", "d", 3), "arts", "en", "x")
		if n := count(t, f, `SELECT count(*) FROM marketplace_listing_stats WHERE course_id=$1`, l.CourseID); n != 1 {
			t.Fatalf("stats row = %d", n)
		}
		if _, err := f.DB.Pool.Exec(ctx, `DELETE FROM learners WHERE id=$1`, o); err != nil {
			t.Fatal(err)
		}
		for _, table := range []string{"marketplace_listings", "marketplace_listing_stats"} {
			if n := count(t, f, `SELECT count(*) FROM `+table+` WHERE course_id=$1`, l.CourseID); n != 0 {
				t.Fatalf("%s kept %d rows after the owner was deleted", table, n)
			}
		}
		if got, _ := browseTitles(t, s, "", sharing.BrowseQuery{Q: "gn1"}); len(got) != 0 {
			t.Fatalf("deleted owner still listed: %v", got)
		}
	})
}

func (c *capture) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = nil
}

func visibility(t *testing.T, f *backuptest.Fixture, course string) string {
	t.Helper()
	var v string
	if err := f.DB.Pool.QueryRow(t.Context(), `SELECT visibility::text FROM courses WHERE id=$1`, course).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func pageKeys(t *testing.T, r *redis.Client) []string {
	t.Helper()
	keys, err := r.Keys(t.Context(), "marketplace:v1:*").Result()
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

func TestMarketplaceRedisIsDerivedAndInvalidatedAtOnce(t *testing.T) {
	f := backuptest.New(t)
	ctx := t.Context()
	r := startRedis(t)
	ev := &capture{}
	s := marketService(t, f, ev, r, nil)
	o1, o2 := marketOwner(t, f, "rc1"), marketOwner(t, f, "rc2")
	a := listCourse(t, s, o1, marketCourse(t, f, o1, "Cached alpha rc1", "d", 3), "tech", "en")
	listCourse(t, s, o2, marketCourse(t, f, o2, "Cached beta rc1", "d", 3), "tech", "en")
	q := sharing.BrowseQuery{Q: "rc1", Sort: "new"}

	t.Run("a_miss_fills_the_cache_and_a_hit_is_served_from_it", func(t *testing.T) {
		if keys := pageKeys(t, r); len(keys) != 0 {
			t.Fatalf("listing must have cleared the cache, found %v", keys)
		}
		if got, _ := browseTitles(t, s, "", q); len(got) != 2 {
			t.Fatalf("first read = %v", got)
		}
		if len(pageKeys(t, r)) < 2 { // one page key + the registry
			t.Fatalf("cache not filled: %v", pageKeys(t, r))
		}
		// Prove the second read is the cache: change Postgres underneath and still see the old title.
		if _, err := f.DB.Pool.Exec(ctx, `UPDATE marketplace_listings SET title='Changed behind the cache rc1' WHERE course_id=$1`, a.CourseID); err != nil {
			t.Fatal(err)
		}
		got, _ := browseTitles(t, s, "", q)
		if strings.Contains(strings.Join(got, "|"), "Changed behind") {
			t.Fatalf("second read bypassed the cache: %v", got)
		}
		if _, err := f.DB.Pool.Exec(ctx, `UPDATE marketplace_listings SET title='Cached alpha rc1' WHERE course_id=$1`, a.CourseID); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("losing_redis_rebuilds_from_postgres", func(t *testing.T) {
		if err := r.FlushAll(ctx).Err(); err != nil {
			t.Fatal(err)
		}
		if got, _ := browseTitles(t, s, "", q); len(got) != 2 {
			t.Fatalf("after FLUSHALL = %v", got)
		}
		if len(pageKeys(t, r)) < 2 {
			t.Fatal("cache did not rebuild")
		}
	})

	t.Run("an_unreachable_redis_never_fails_a_read", func(t *testing.T) {
		dead, err := cache.New("redis://127.0.0.1:1")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = dead.Close() }()
		down := marketService(t, f, ev, dead, nil)
		if got, _ := browseTitles(t, down, "", q); len(got) != 2 {
			t.Fatalf("with Redis down = %v", got)
		}
	})

	t.Run("delisting_deletes_every_cached_page_before_the_next_read", func(t *testing.T) {
		browseTitles(t, s, "", q)
		browseTitles(t, s, "", sharing.BrowseQuery{Sort: "popular"})
		if len(pageKeys(t, r)) == 0 {
			t.Fatal("setup: nothing cached")
		}
		if err := s.DelistCourse(ctx, o1, a.CourseID); err != nil {
			t.Fatal(err)
		}
		if keys := pageKeys(t, r); len(keys) != 0 {
			t.Fatalf("cached pages survived the delist: %v", keys)
		}
		got, _ := browseTitles(t, s, "", q)
		if len(got) != 1 || strings.Contains(got[0], "alpha") {
			t.Fatalf("after delist = %v", got)
		}
	})

	t.Run("a_viewer_with_blocks_bypasses_the_shared_cache", func(t *testing.T) {
		viewer := newLearner(t, f, "rcviewer")
		if _, err := f.DB.Pool.Exec(ctx, `INSERT INTO learner_blocks(blocker_id,blocked_id) VALUES($1,$2)`, viewer, o2); err != nil {
			t.Fatal(err)
		}
		browseTitles(t, s, "", q) // an anonymous read fills the cache with beta
		if got, _ := browseTitles(t, s, viewer, q); len(got) != 0 {
			t.Fatalf("blocked viewer read the shared cache: %v", got)
		}
	})
}

// The handler layer: anonymous-safe, flag-gated, rate limited and neutral.
func TestMarketplaceHTTPIsAnonymousSafeFlaggedAndLimited(t *testing.T) {
	f := backuptest.New(t)
	ctx := t.Context()
	ev := &capture{}
	s := marketService(t, f, ev, nil, func(c *sharing.Config) { c.PreviewRateLimitPerIP = 5 })
	off, _ := enrollService(t, f, ev, nil)
	o := marketOwner(t, f, "http")
	l := listCourse(t, s, o, marketCourse(t, f, o, "HTTP course ht1", "d", 3), "tech", "en")

	mux := func(svc *sharing.Service) *http.ServeMux {
		m := http.NewServeMux()
		pass := func(h http.Handler) http.Handler { return h }
		sharing.Mount(m, svc, pass, pass)
		return m
	}
	call := func(m *http.ServeMux, actor, method, path, body string) *httptest.ResponseRecorder {
		c := ctx
		if actor != "" {
			c = accounts.WithLearnerID(c, actor)
		}
		req := httptest.NewRequestWithContext(c, method, path, strings.NewReader(body))
		req.RemoteAddr = "203.0.113.9:4000"
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, req)
		return rec
	}
	on := mux(s)
	rec := call(on, "", http.MethodGet, "/v1/marketplace/courses?q=ht1&sort=new", "")
	if rec.Code != 200 || rec.Header().Get("X-Robots-Tag") != "noindex" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("anonymous browse = %d %v", rec.Code, rec.Header())
	}
	var page sharing.Page
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || len(page.Items) != 1 {
		t.Fatalf("body = %s (%v)", rec.Body, err)
	}
	if rec := call(on, "", http.MethodGet, "/v1/marketplace/courses/"+l.CourseID, ""); rec.Code != 200 {
		t.Fatalf("anonymous page = %d", rec.Code)
	}
	if rec := call(on, "", http.MethodGet, "/v1/marketplace/courses?cursor=garbage", ""); rec.Code != 400 || !strings.Contains(rec.Body.String(), "invalid_cursor") {
		t.Fatalf("bad cursor = %d %s", rec.Code, rec.Body)
	}
	if rec := call(on, "", http.MethodGet, "/v1/marketplace/courses/not-a-uuid", ""); rec.Code != 404 {
		t.Fatalf("bad id = %d", rec.Code)
	}
	if rec := call(on, "", http.MethodPost, "/v1/marketplace/courses/"+l.CourseID+"/enroll", ""); rec.Code != 401 {
		t.Fatalf("anonymous enroll = %d", rec.Code)
	}
	viewer := newLearner(t, f, "httpviewer")
	if rec := call(on, viewer, http.MethodPost, "/v1/marketplace/courses/"+l.CourseID+"/enroll", `{"learner_id":"x"}`); rec.Code != 201 && rec.Code != 200 {
		t.Fatalf("enroll = %d %s", rec.Code, rec.Body)
	}
	// Owner listing routes take the actor from the session only.
	if rec := call(on, "", http.MethodPut, "/v1/courses/"+l.CourseID+"/marketplace-listing", `{}`); rec.Code != 401 {
		t.Fatalf("anonymous list = %d", rec.Code)
	}
	if rec := call(on, newLearner(t, f, "httpother"), http.MethodDelete, "/v1/courses/"+l.CourseID+"/marketplace-listing", ""); rec.Code != 404 {
		t.Fatalf("stranger delist = %d", rec.Code)
	}
	// Rate limit: the 6th anonymous read from one address in the window is 429 with Retry-After.
	var limited *httptest.ResponseRecorder
	for i := 0; i < 8 && limited == nil; i++ {
		if rec := call(on, "", http.MethodGet, "/v1/marketplace/courses", ""); rec.Code == http.StatusTooManyRequests {
			limited = rec
		}
	}
	if limited == nil || limited.Header().Get("Retry-After") == "" {
		t.Fatal("the public read was never rate limited")
	}

	// Flag off: every route is the same neutral 404, even for the owner and an enrolled learner.
	offMux := mux(off)
	for _, c := range []struct{ actor, method, path, body string }{
		{"", http.MethodGet, "/v1/marketplace/courses", ""},
		{"", http.MethodGet, "/v1/marketplace/courses/" + l.CourseID, ""},
		{viewer, http.MethodPost, "/v1/marketplace/courses/" + l.CourseID + "/enroll", "{}"},
		{o, http.MethodPut, "/v1/courses/" + l.CourseID + "/marketplace-listing", `{"category":"tech","language":"en"}`},
		{o, http.MethodDelete, "/v1/courses/" + l.CourseID + "/marketplace-listing", ""},
	} {
		rec := call(offMux, c.actor, c.method, c.path, c.body)
		if rec.Code != 404 || strings.TrimSpace(rec.Body.String()) != `{"error":"marketplace_unavailable"}` {
			t.Fatalf("flag off %s %s = %d %s", c.method, c.path, rec.Code, rec.Body)
		}
	}
}
