//go:build integration

package sharing_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
)

const (
	// stranger2 is a learner that blocked owner, for preview block tests
	stranger2 = "a3333333-3333-4333-8333-333333333333"
)

// seedPublishableCourse seeds a safe course, builds a pass draft, and returns (courseID, draftID).
func seedPublishableCourse(t *testing.T, f *backuptest.Fixture, s *sharing.Service) (string, string) {
	t.Helper()
	ctx := t.Context()
	pool := f.DB.Pool

	// Insert a fresh course for publishing tests (separate from the fixture's shared one)
	var quote, creation, courseID, unit, stub string
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	steps := []func() error{
		func() error {
			return tx.QueryRow(ctx, `INSERT INTO pricing_quotes(learner_id,owner_reference,pricing_version,snapshot,created_at,expires_at)
SELECT $1::uuid,encode(sha256(convert_to($1::text,'UTF8')),'hex'),version,'{}',now(),now()+interval '1 hour'
FROM pricing_quote_policies ORDER BY version LIMIT 1 RETURNING id::text`, owner).Scan(&quote)
		},
		func() error {
			return tx.QueryRow(ctx, `INSERT INTO course_creations(learner_id,draft_id,revision,quote_id,request,state) VALUES($1,gen_random_uuid(),1,$2,'{}','enrolled') RETURNING id::text`, owner, quote).Scan(&creation)
		},
		func() error {
			return tx.QueryRow(ctx, `INSERT INTO courses(learner_id,creation_id,title,description) VALUES($1,$2,'Publish Test Course','A course for publish tests.') RETURNING id::text`, owner, creation).Scan(&courseID)
		},
		func() error {
			_, e := tx.Exec(ctx, `INSERT INTO course_enrollments(learner_id,course_id) VALUES($1,$2)`, owner, courseID)
			return e
		},
		func() error {
			return tx.QueryRow(ctx, `INSERT INTO course_units(learner_id,course_id,authored_id,position,title) VALUES($1,$2,'pu1',0,'Unit One') RETURNING id::text`, owner, courseID).Scan(&unit)
		},
		func() error {
			return tx.QueryRow(ctx, `INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status) VALUES($1,$2,'pl1',0,'Moon Lesson','Name the phases','done') RETURNING id::text`, owner, unit).Scan(&stub)
		},
		func() error {
			_, e := tx.Exec(ctx, `INSERT INTO generated_lessons(lesson_stub_id,course_id,learner_id,title,manifest) VALUES($1,$2,$3,'Moon Lesson','{}'::jsonb)`, stub, courseID, owner)
			return e
		},
		func() error {
			_, e := tx.Exec(ctx, `INSERT INTO lesson_slides(lesson_stub_id,learner_id,position,slide_id,html,duration_ms,narration) VALUES($1,$2,0,'ps1','<h1>Moon</h1>',3000,'narr')`, stub, owner)
			return e
		},
	}
	for _, step := range steps {
		if err = step(); err != nil {
			t.Fatalf("seedPublishableCourse: %v", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	// Build a draft and expect pass verdict
	d, err := s.BuildDraft(ctx, owner, courseID)
	if err != nil {
		t.Fatalf("BuildDraft: %v", err)
	}
	if d.Verdict != sharing.VerdictPass {
		t.Fatalf("expected pass verdict, got %q", d.Verdict)
	}
	return courseID, d.ID
}

func seedBlockedLearner(t *testing.T, f *backuptest.Fixture, blockerID, blockedID string) {
	t.Helper()
	ctx := t.Context()
	// Insert the stranger2 learner first if not exists
	_, err := f.DB.Pool.Exec(ctx, `INSERT INTO learners(id,display_name) VALUES($1::uuid,'Stranger Two') ON CONFLICT DO NOTHING`, blockedID)
	if err != nil {
		t.Fatal("seedBlockedLearner insert learner:", err)
	}
	_, err = f.DB.Pool.Exec(ctx, `INSERT INTO learner_blocks(blocker_id,blocked_id) VALUES($1::uuid,$2::uuid) ON CONFLICT DO NOTHING`, blockerID, blockedID)
	if err != nil {
		t.Fatal("seedBlockedLearner insert block:", err)
	}
}

func TestPublishRevocableLinksWithPublicPreviews(t *testing.T) {
	f := backuptest.New(t)
	f.Seed(t)
	ctx := t.Context()
	engineClient := startEngine(t)
	ev := &capture{}
	s := newService(t, f, engineClient, ev)

	// Enable analytics so events can be captured
	_, err := f.DB.Pool.Exec(ctx, `UPDATE learner_profiles SET analytics_opt_out=false WHERE learner_id=$1`, owner)
	if err != nil {
		// profile may not exist; create it
		_, _ = f.DB.Pool.Exec(ctx, `INSERT INTO learner_profiles(learner_id,locale,time_zone) VALUES($1,'en-US','UTC') ON CONFLICT DO NOTHING`, owner)
	}

	t.Run("owner_with_pass_draft_can_publish", func(t *testing.T) {
		courseID, _ := seedPublishableCourse(t, f, s)
		ev.mu.Lock()
		ev.events = nil
		ev.mu.Unlock()

		token, err := s.PublishLink(ctx, owner, courseID)
		if err != nil {
			t.Fatalf("PublishLink: %v", err)
		}
		if token == "" {
			t.Error("token must be non-empty")
		}

		// visibility must be 'link'
		var vis string
		if err = f.DB.Pool.QueryRow(ctx, `SELECT visibility FROM courses WHERE id=$1`, courseID).Scan(&vis); err != nil {
			t.Fatal(err)
		}
		if vis != "link" {
			t.Errorf("visibility: got %q, want link", vis)
		}

		// token hash must exist
		hash := sharing.HashToken(token)
		var cnt int
		if err = f.DB.Pool.QueryRow(ctx, `SELECT count(*) FROM publication_links WHERE token_hash=$1 AND revoked_at IS NULL`, hash).Scan(&cnt); err != nil {
			t.Fatal(err)
		}
		if cnt != 1 {
			t.Errorf("publication_links row not found for hash")
		}

		// Analytics events
		if len(ev.named("course_visibility_changed")) == 0 {
			t.Error("course_visibility_changed not emitted")
		}
		if len(ev.named("course_published")) == 0 {
			t.Error("course_published not emitted")
		}
		if len(ev.named("share_link_created")) == 0 {
			t.Error("share_link_created not emitted")
		}
	})

	t.Run("non_owner_cannot_publish", func(t *testing.T) {
		courseID, _ := seedPublishableCourse(t, f, s)
		_, err := s.PublishLink(ctx, stranger, courseID)
		if !errors.Is(err, sharing.ErrUnavailable) {
			t.Errorf("non-owner publish: got %v, want ErrUnavailable", err)
		}
	})

	t.Run("under_18_publisher_blocked", func(t *testing.T) {
		courseID, _ := seedPublishableCourse(t, f, s)
		youngService := s.WithAgeSource(adult(false))
		_, err := youngService.PublishLink(ctx, owner, courseID)
		if !errors.Is(err, sharing.ErrAdultRequired) {
			t.Errorf("under-18 publish: got %v, want ErrAdultRequired", err)
		}
	})

	t.Run("needs_review_verdict_cannot_publish", func(t *testing.T) {
		courseID, _ := seedPublishableCourse(t, f, s)
		// Force verdict to needs_review
		_, err := f.DB.Pool.Exec(ctx, `UPDATE publication_drafts SET verdict='needs_review',screened_at=now() WHERE course_id=$1`, courseID)
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.PublishLink(ctx, owner, courseID)
		if !errors.Is(err, sharing.ErrPublishNotAllowed) {
			t.Errorf("needs_review publish: got %v, want ErrPublishNotAllowed", err)
		}
	})

	t.Run("pending_screening_cannot_publish", func(t *testing.T) {
		courseID, _ := seedPublishableCourse(t, f, s)
		_, err := f.DB.Pool.Exec(ctx, `UPDATE publication_drafts SET verdict='pending_screening',screened_at=NULL WHERE course_id=$1`, courseID)
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.PublishLink(ctx, owner, courseID)
		if !errors.Is(err, sharing.ErrPublishNotAllowed) {
			t.Errorf("pending publish: got %v, want ErrPublishNotAllowed", err)
		}
	})

	t.Run("owner_can_revoke_link", func(t *testing.T) {
		courseID, _ := seedPublishableCourse(t, f, s)
		token, err := s.PublishLink(ctx, owner, courseID)
		if err != nil {
			t.Fatal(err)
		}
		ev.mu.Lock()
		ev.events = nil
		ev.mu.Unlock()

		if err = s.RevokeLink(ctx, owner, courseID); err != nil {
			t.Fatalf("RevokeLink: %v", err)
		}

		// old token must 404
		_, err = s.GetPreview(ctx, token, "")
		if !errors.Is(err, sharing.ErrLinkNotFound) {
			t.Errorf("revoked token: got %v, want ErrLinkNotFound", err)
		}

		// event
		if len(ev.named("share_link_revoked")) == 0 {
			t.Error("share_link_revoked not emitted")
		}
	})

	t.Run("owner_can_rotate_link", func(t *testing.T) {
		courseID, _ := seedPublishableCourse(t, f, s)
		oldToken, err := s.PublishLink(ctx, owner, courseID)
		if err != nil {
			t.Fatal(err)
		}
		ev.mu.Lock()
		ev.events = nil
		ev.mu.Unlock()

		newToken, err := s.RotateLink(ctx, owner, courseID)
		if err != nil {
			t.Fatalf("RotateLink: %v", err)
		}
		if newToken == oldToken {
			t.Error("RotateLink returned same token")
		}

		// old token 404
		_, err = s.GetPreview(ctx, oldToken, "")
		if !errors.Is(err, sharing.ErrLinkNotFound) {
			t.Errorf("old token after rotate: got %v, want ErrLinkNotFound", err)
		}

		// new token works
		_, err = s.GetPreview(ctx, newToken, "")
		if err != nil {
			t.Errorf("new token after rotate: %v", err)
		}

		// events
		if len(ev.named("share_link_revoked")) == 0 {
			t.Error("share_link_revoked not emitted on rotate")
		}
		if len(ev.named("share_link_created")) == 0 {
			t.Error("share_link_created not emitted on rotate")
		}
	})

	t.Run("public_preview_returns_allowlisted_payload", func(t *testing.T) {
		courseID, _ := seedPublishableCourse(t, f, s)
		token, err := s.PublishLink(ctx, owner, courseID)
		if err != nil {
			t.Fatal(err)
		}
		ev.mu.Lock()
		ev.events = nil
		ev.mu.Unlock()

		payload, err := s.GetPreview(ctx, token, "")
		if err != nil {
			t.Fatalf("GetPreview: %v", err)
		}

		// Marshal and check no forbidden fields
		raw, _ := json.Marshal(payload)
		str := string(raw)
		for _, forbidden := range []string{"questions", "answer_index", "explanation", "retry_pool", "narration_refs", "narration"} {
			if contains(str, `"`+forbidden+`"`) {
				t.Errorf("preview payload contains forbidden field %q", forbidden)
			}
		}

		// CourseID must be set
		if payload.CourseID != courseID {
			t.Errorf("preview CourseID: got %q, want %q", payload.CourseID, courseID)
		}
		// SampleSlides <= 3
		if len(payload.SampleSlides) > 3 {
			t.Errorf("too many sample slides: %d", len(payload.SampleSlides))
		}

		// share_link_opened event
		if len(ev.named("share_link_opened")) == 0 {
			t.Error("share_link_opened not emitted")
		}
	})

	t.Run("public_preview_with_blocked_viewer_returns_404", func(t *testing.T) {
		courseID, _ := seedPublishableCourse(t, f, s)
		token, err := s.PublishLink(ctx, owner, courseID)
		if err != nil {
			t.Fatal(err)
		}

		// seed a learner who blocked the owner
		seedBlockedLearner(t, f, stranger2, owner)

		_, err = s.GetPreview(ctx, token, stranger2)
		if !errors.Is(err, sharing.ErrLinkNotFound) {
			t.Errorf("blocked viewer: got %v, want ErrLinkNotFound", err)
		}
	})

	t.Run("analytics_events_on_full_lifecycle", func(t *testing.T) {
		courseID, _ := seedPublishableCourse(t, f, s)
		ev.mu.Lock()
		ev.events = nil
		ev.mu.Unlock()

		// Publish
		token, err := s.PublishLink(ctx, owner, courseID)
		if err != nil {
			t.Fatal(err)
		}
		publishEvents := ev.snapshot()
		for _, want := range []string{"course_visibility_changed", "course_published", "share_link_created"} {
			found := false
			for _, e := range publishEvents {
				if e["_event"] == want {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("missing event %q on publish", want)
			}
		}

		// Preview
		ev.mu.Lock()
		ev.events = nil
		ev.mu.Unlock()
		_, _ = s.GetPreview(ctx, token, "")
		if len(ev.named("share_link_opened")) == 0 {
			t.Error("share_link_opened not emitted on GetPreview")
		}

		// Revoke
		ev.mu.Lock()
		ev.events = nil
		ev.mu.Unlock()
		_ = s.RevokeLink(ctx, owner, courseID)
		if len(ev.named("share_link_revoked")) == 0 {
			t.Error("share_link_revoked not emitted on RevokeLink")
		}
	})
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// snapshot returns a copy of events so we can inspect without holding the lock
func (c *capture) snapshot() []map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]map[string]any, len(c.events))
	copy(out, c.events)
	return out
}

// seedPublishedCourseForPricingServer is used by the pricingserver fixture (exported for use there)
func SeedAndPublish(ctx context.Context, s *sharing.Service, f *backuptest.Fixture, ownerID string) (courseID, token string, err error) {
	_ = fmt.Sprintf // keep import
	return "", "", errors.New("not implemented here — see pricingserver fixture")
}
