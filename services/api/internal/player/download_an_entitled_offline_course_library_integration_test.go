//go:build integration

package player_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
)

func TestDownloadAnEntitledOfflineCourseLibraryIntegration(t *testing.T) {
	f := backuptest.New(t)
	f.Seed(t)
	mail := new(accounts.MailSink)
	auth, err := accounts.New(f.DB.Pool, strings.Repeat("player-e306-test", 4), mail)
	if err != nil {
		t.Fatal(err)
	}

	catalogue, err := pricing.Load(t.Context(), f.DB.Queries)
	if err != nil {
		t.Fatal(err)
	}
	creditService := credits.NewServiceWithCatalogue(f.DB.Pool, nil, catalogue, nil)

	playerService := player.New(player.Options{
		Pool:        f.DB.Pool,
		Objects:     f.ApplicationObjects(),
		AssetOrigin: "https://assets.example.test",
		Credits:     creditService,
	})

	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{
		Accounts: auth,
		Player:   playerService,
		Credits:  creditService,
	}, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()

	call := func(method, path, token string, body any) (int, []byte) {
		var r io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			r = bytes.NewReader(raw)
		}
		req, _ := http.NewRequestWithContext(t.Context(), method, server.URL+path, r)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer func() { _ = resp.Body.Close() }()
		out, e := io.ReadAll(resp.Body)
		if e != nil {
			t.Fatal(e)
		}
		return resp.StatusCode, out
	}

	login := func(email string) string {
		if c, b := call("POST", "/v1/auth/code", "", map[string]any{"email": email, "age_confirmed": true}); c != 202 {
			t.Fatal(c, string(b))
		}
		c, b := call("POST", "/v1/auth/verify", "", map[string]any{"email": email, "code": mail.Code(email), "age_confirmed": true})
		if c != 200 {
			t.Fatal(c, string(b))
		}
		var token struct {
			Access string `json:"access_token"`
		}
		if err := json.Unmarshal(b, &token); err != nil {
			t.Fatal(err)
		}
		return token.Access
	}

	subscriberToken := login("synthetic@example.test")
	freeToken := login("free-learner@example.test")

	const courseID = "f1111111-1111-4111-8111-111111111111"
	const lessonID = "f3333333-3333-4333-8333-333333333333"
	const lockedLessonID = "f4444444-4444-4444-8444-444444444444"
	const ungeneratedLessonID = "f5555555-5555-5555-8555-555555555555"
	const unitID = "f2222222-2222-4222-8222-222222222222"
	const subscriberID = "a1111111-1111-4111-8111-111111111111"

	// Seed a locked lesson stub and an ungenerated lesson stub
	_, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO course_lesson_stubs(id, learner_id, unit_id, authored_id, position, title, goal, status)
		VALUES($1, $2, $3, 'locked-stub', 1, 'Locked Lesson', 'Locked goal', 'locked'),
		      ($4, $2, $3, 'ungenerated-stub', 2, 'Ungenerated Lesson', 'Ungenerated goal', 'ready_to_generate')
		ON CONFLICT DO NOTHING`, lockedLessonID, subscriberID, unitID, ungeneratedLessonID)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Unauthenticated request to course download library returns 401
	status, body := call("GET", "/v1/courses/"+courseID+"/download", "", nil)
	if status != 401 {
		t.Fatalf("expected 401 for unauthenticated download, got %d: %s", status, body)
	}

	// 2. Free / unentitled learner request returns 403 Forbidden with subscription_required
	status, body = call("GET", "/v1/courses/"+courseID+"/download", freeToken, nil)
	if status != 403 || !strings.Contains(string(body), "subscription_required") {
		t.Fatalf("expected 403 subscription_required for free learner, got %d: %s", status, body)
	}

	// 3. BUT universal caching remains: free learner enrolled in a course can still get individual lesson manifest
	// (Enrolling free learner first to test unmetered replay)
	var freeLearnerID string
	err = f.DB.Pool.QueryRow(t.Context(), `SELECT id::text FROM learners WHERE id != 'a1111111-1111-4111-8111-111111111111' ORDER BY created_at DESC LIMIT 1`).Scan(&freeLearnerID)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.DB.Pool.Exec(t.Context(), `INSERT INTO course_enrollments(learner_id, course_id) VALUES($1, $2) ON CONFLICT DO NOTHING`, freeLearnerID, courseID)
	// Opened lesson caching is universal: manifest endpoint works without subscription
	status, _ = call("GET", "/v1/lessons/"+lessonID+"/manifest", subscriberToken, nil)
	if status != 200 {
		t.Fatalf("expected 200 for lesson manifest, got %d", status)
	}

	// 4. Entitled subscriber (funded in f.Seed) requests course download library -> 200 OK
	status, body = call("GET", "/v1/courses/"+courseID+"/download", subscriberToken, nil)
	if status != 200 {
		t.Fatalf("expected 200 for entitled course library download, got %d: %s", status, body)
	}

	var lib player.CourseDownloadLibrary
	if err := json.Unmarshal(body, &lib); err != nil {
		t.Fatalf("failed to decode library response: %v, raw: %s", err, body)
	}
	if lib.CourseID != courseID || len(lib.Lessons) < 3 {
		t.Fatalf("unexpected library payload: %+v", lib)
	}
	if !lib.Entitled {
		t.Fatalf("expected entitled to be true")
	}
	// Check that private assessment data / answers are not leaked
	if strings.Contains(string(body), "answer") || strings.Contains(string(body), "html_key") {
		t.Fatalf("leaked private fields in course library download: %s", body)
	}

	// Verify honesty of Unlocked and Downloadable:
	// - Locked lesson: unlocked=false, downloadable=false, no manifest_url
	// - Ungenerated lesson: unlocked=true, downloadable=false, no manifest_url
	// - Generated lesson: unlocked=true, downloadable=true, has manifest_url
	var foundGenerated, foundLocked, foundUngenerated bool
	for _, l := range lib.Lessons {
		switch l.LessonID {
		case lessonID:
			foundGenerated = true
			if !l.Unlocked || !l.Downloadable || l.ManifestURL == "" {
				t.Fatalf("expected generated lesson to be unlocked and downloadable with manifest URL, got %+v", l)
			}
		case lockedLessonID:
			foundLocked = true
			if l.Unlocked || l.Downloadable || l.ManifestURL != "" {
				t.Fatalf("expected locked lesson to NOT be unlocked and NOT downloadable, got %+v", l)
			}
		case ungeneratedLessonID:
			foundUngenerated = true
			if !l.Unlocked || l.Downloadable || l.ManifestURL != "" {
				t.Fatalf("expected ungenerated lesson to be unlocked but NOT downloadable, got %+v", l)
			}
		}
	}
	if !foundGenerated || !foundLocked || !foundUngenerated {
		t.Fatalf("missing expected lessons in download library: gen=%v locked=%v ungen=%v", foundGenerated, foundLocked, foundUngenerated)
	}

	// 4b. When Credits service is not provided (nil), download library returns 503
	nilCreditsPlayer := player.New(player.Options{
		Pool:        f.DB.Pool,
		Objects:     f.ApplicationObjects(),
		AssetOrigin: "https://assets.example.test",
		Credits:     nil,
	})
	nilCreditsServer := httptest.NewServer(httpapi.New(httpapi.Dependencies{
		Accounts: auth,
		Player:   nilCreditsPlayer,
	}, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer nilCreditsServer.Close()

	nilReq, _ := http.NewRequestWithContext(t.Context(), "GET", nilCreditsServer.URL+"/v1/courses/"+courseID+"/download", nil)
	nilReq.Header.Set("Authorization", "Bearer "+subscriberToken)
	nilResp, err := nilCreditsServer.Client().Do(nilReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = nilResp.Body.Close()
	if nilResp.StatusCode != 503 {
		t.Fatalf("expected 503 when Credits is nil, got %d", nilResp.StatusCode)
	}

	// 5. Expired subscription: record revocation or past paid_through
	_, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO credit_payment_inbox(event_id, environment, event_type, session_id)
		VALUES('evt_integration_expiry', 'test', 'customer.subscription.deleted', 'sub_fixture')
		ON CONFLICT DO NOTHING`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO credit_subscription_revocations(period_key, event_id, reason)
		SELECT period_key, 'evt_integration_expiry', 'refunded' FROM credit_subscription_terms
		ON CONFLICT DO NOTHING`)
	if err != nil {
		t.Fatal(err)
	}
	_ = creditService.InvalidateCache(t.Context(), "a1111111-1111-4111-8111-111111111111")

	// Now attempting new bulk library download returns 403 Forbidden
	status, body = call("GET", "/v1/courses/"+courseID+"/download", subscriberToken, nil)
	if status != 403 || !strings.Contains(string(body), "subscription_required") {
		t.Fatalf("expected 403 subscription_required after expiry, got %d: %s", status, body)
	}

	// BUT individual opened lesson manifest remains accessible for enrolled learner
	status, _ = call("GET", "/v1/lessons/"+lessonID+"/manifest", subscriberToken, nil)
	if status != 200 {
		t.Fatalf("already delivered/opened lesson must remain accessible after subscription expiry, got %d", status)
	}
}
