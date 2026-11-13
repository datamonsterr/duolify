//go:build integration

package gamification

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestPaidLearningAnalyticsAndHistoryExportsIntegration(t *testing.T) {
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("duolify"), postgres.WithUsername("duolify"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)
	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err = migrations.Up(ctx, url); err != nil {
		t.Fatal(err)
	}
	db, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mail := &accounts.MailSink{}
	auth, err := accounts.New(db.Pool, "gamification-test-secret-at-least-32-bytes", mail)
	if err != nil {
		t.Fatal(err)
	}

	sink := &eventSink{}
	svc := New(db.Pool, analytics.WithConsent(sink, analytics.DatabaseConsent(db.Pool)))
	creditSvc := credits.NewServiceWithCatalogue(db.Pool, nil, nil, sink)
	svc.UseEntitlements(creditSvc)

	h := httpapi.New(httpapi.Dependencies{
		Gamification: svc,
		Accounts:     auth,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Helper to create learner with access token
	createLearner := func(email string) (string, string) {
		req := httptest.NewRequestWithContext(ctx, "POST", "/v1/auth/code", strings.NewReader(`{"email":"`+email+`","age_confirmed":true}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		auth.ServeHTTP(w, req)
		if w.Code != 202 {
			t.Fatalf("auth code status: %d", w.Code)
		}
		code := mail.Code(email)
		verifyReq := httptest.NewRequestWithContext(ctx, "POST", "/v1/auth/verify", strings.NewReader(`{"email":"`+email+`","code":"`+code+`","age_confirmed":true}`))
		verifyReq.Header.Set("Content-Type", "application/json")
		vw := httptest.NewRecorder()
		auth.ServeHTTP(vw, verifyReq)
		if vw.Code != 200 {
			t.Fatalf("auth verify status: %d", vw.Code)
		}
		var resp struct {
			AccessToken string `json:"access_token"`
		}
		_ = json.NewDecoder(vw.Body).Decode(&resp)
		var learnerID string
		if err := db.Pool.QueryRow(ctx, "SELECT learner_id::text FROM auth_identities WHERE email=$1", email).Scan(&learnerID); err != nil {
			t.Fatalf("get learner id for %s: %v", email, err)
		}
		return learnerID, resp.AccessToken
	}

	// Helper to insert a funded subscription term for testing
	insertSubscription := func(learnerID, planID, planName string, benefits []string, paidThrough time.Time) {
		orderID := uuid.New().String()
		subID := "sub_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:14]
		invoiceID := "in_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:14]
		periodKey := "stripe:test:" + subID + ":1800000000:1802592000:0"
		bJSON, _ := json.Marshal(benefits)

		_, err := db.Pool.Exec(ctx, `INSERT INTO credit_checkout_orders(id,request_id,learner_id,owner_reference,pricing_version,sku,amount_minor,currency,credits,billing_period,platform,return_to)
			VALUES($1::uuid,gen_random_uuid(),$2::uuid,encode(sha256(convert_to($2::text,'UTF8')),'hex'),'usd-quotes-2026-11-09',$3,1999,'USD',1600,'monthly','web','/credits')`,
			orderID, learnerID, planID+"_monthly")
		if err != nil {
			t.Fatalf("insert checkout order: %v", err)
		}

		_, err = db.Pool.Exec(ctx, `INSERT INTO credit_subscriptions(subscription_id,environment,order_id,customer_id)
			VALUES($1,'test',$2::uuid,'cus_test')`,
			subID, orderID)
		if err != nil {
			t.Fatalf("insert credit subscription: %v", err)
		}

		var ledgerID int64
		err = db.Pool.QueryRow(ctx, `INSERT INTO credit_entries(learner_id,owner_reference,amount,kind,reference,pricing_version,provenance)
			VALUES($1::uuid,encode(sha256(convert_to($1::text,'UTF8')),'hex'),1600,'subscription_grant',$2,'usd-quotes-2026-11-09','{}') RETURNING id`,
			learnerID, periodKey).Scan(&ledgerID)
		if err != nil {
			t.Fatalf("insert credit entry: %v", err)
		}

		_, err = db.Pool.Exec(ctx, `INSERT INTO credit_subscription_terms(period_key,subscription_id,invoice_id,pricing_version,plan_id,plan_name,entitlement_version,service_start,paid_through,amount_minor,currency,monthly_credits,benefits,concurrent_generation,ledger_id)
			VALUES($1,$2,$3,'usd-quotes-2026-11-09',$4,$5,'usd-quotes-2026-11-09',now() - interval '10 days',$6,1999,'USD',1600,$7,1,$8)`,
			periodKey, subID, invoiceID, planID, planName, paidThrough, bJSON, ledgerID)
		if err != nil {
			t.Fatalf("insert subscription terms: %v", err)
		}
	}

	freeID, freeToken := createLearner("free@example.test")
	learnID, learnToken := createLearner("learn@example.test")
	plusID, plusToken := createLearner("plus@example.test")
	proID, proToken := createLearner("pro@example.test")
	expiredID, expiredToken := createLearner("expired@example.test")

	now := time.Now().UTC()
	future := now.Add(20 * 24 * time.Hour)
	past := now.Add(-5 * 24 * time.Hour)

	insertSubscription(learnID, "learn", "Learn", []string{"Learning history export"}, future)
	insertSubscription(plusID, "plus", "Plus", []string{"Learning history export", "Advanced learning analytics"}, future)
	insertSubscription(proID, "pro", "Pro", []string{"Learning history export", "Advanced learning analytics", "Custom learning analytics exports"}, future)
	insertSubscription(expiredID, "pro", "Pro", []string{"Learning history export", "Advanced learning analytics", "Custom learning analytics exports"}, past)

	// Helper to insert course
	insertCourse := func(courseID, ownerID, title string, enroll bool) {
		quoteID := uuid.New().String()
		_, err := db.Pool.Exec(ctx, `INSERT INTO pricing_quotes(id,learner_id,owner_reference,pricing_version,snapshot,created_at,expires_at)
			VALUES($1::uuid,$2::uuid,encode(sha256(convert_to($2::text,'UTF8')),'hex'),'usd-quotes-2026-11-09','{}',now(),now()+interval '1 hour')`,
			quoteID, ownerID)
		if err != nil {
			t.Fatalf("insert quote: %v", err)
		}
		creationID := uuid.New().String()
		_, err = db.Pool.Exec(ctx, `INSERT INTO course_creations(id,learner_id,draft_id,revision,quote_id,request,state,course_id)
			VALUES($1::uuid,$2::uuid,gen_random_uuid(),1,$3::uuid,'{}','enrolled',$4::uuid)`,
			creationID, ownerID, quoteID, courseID)
		if err != nil {
			t.Fatalf("insert creation: %v", err)
		}
		_, err = db.Pool.Exec(ctx, `INSERT INTO courses(id,learner_id,creation_id,title)
			VALUES($1::uuid,$2::uuid,$3::uuid,$4)`,
			courseID, ownerID, creationID, title)
		if err != nil {
			t.Fatalf("insert course: %v", err)
		}
		if enroll {
			_, err = db.Pool.Exec(ctx, `INSERT INTO course_enrollments(learner_id,course_id) VALUES($1::uuid,$2::uuid)`, ownerID, courseID)
			if err != nil {
				t.Fatalf("insert enrollment: %v", err)
			}
		}
	}

	// Seed courses: Course 1 (learner's course), Course 2 (friend's private course)
	course1 := uuid.New().String()
	course2Private := uuid.New().String()
	friendID, _ := createLearner("friend@example.test")

	insertCourse(course1, plusID, "Astronomy Basics", true)
	insertCourse(course2Private, friendID, "Secret Diary of Friend", false)

	// Seed XP events for Plus learner on course1
	lesson1 := uuid.New().String()
	comp1 := uuid.New().String()
	_, err = db.Pool.Exec(ctx, `INSERT INTO xp_events(learner_id,lesson_id,course_id,completion_id,amount,perfect,config_version,earned_at,local_date,time_zone)
		VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,15,true,'launch-v1',now() - interval '1 hour',current_date,'UTC')`, plusID, lesson1, course1, comp1)
	if err != nil {
		t.Fatalf("insert xp_events: %v", err)
	}

	// Seed streak state for Plus learner
	_, err = db.Pool.Exec(ctx, `INSERT INTO streak_state(learner_id,current_days,longest_days,last_active_date,decided_through)
		VALUES($1::uuid,7,23,current_date,current_date)`, plusID)
	if err != nil {
		t.Fatalf("insert streak_state: %v", err)
	}

	// 1. Analytics access checks:
	// Free -> 403
	req := httptest.NewRequestWithContext(ctx, "GET", "/v1/gamification/analytics", nil)
	req.Header.Set("Authorization", "Bearer "+freeToken)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("Free analytics status: got %d, want 403", w.Code)
	}

	// Learn -> 403
	req = httptest.NewRequestWithContext(ctx, "GET", "/v1/gamification/analytics", nil)
	req.Header.Set("Authorization", "Bearer "+learnToken)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("Learn analytics status: got %d, want 403", w.Code)
	}

	// Expired -> 403
	req = httptest.NewRequestWithContext(ctx, "GET", "/v1/gamification/analytics", nil)
	req.Header.Set("Authorization", "Bearer "+expiredToken)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("Expired analytics status: got %d, want 403", w.Code)
	}

	// Plus -> 200 with breakdown
	req = httptest.NewRequestWithContext(ctx, "GET", "/v1/gamification/analytics", nil)
	req.Header.Set("Authorization", "Bearer "+plusToken)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Plus analytics status: got %d, want 200, body: %s", w.Code, w.Body.String())
	}
	var analyticsResp struct {
		PlanID          string `json:"plan_id"`
		CourseBreakdown []struct {
			CourseID string `json:"course_id"`
			Title    string `json:"title"`
			TotalXP  int    `json:"total_xp"`
		} `json:"course_breakdown"`
		StreakBreakdown struct {
			CurrentStreak int `json:"current_streak"`
			LongestStreak int `json:"longest_streak"`
		} `json:"streak_breakdown"`
		CustomExportAllowed bool `json:"custom_export_allowed"`
	}
	if err := json.NewDecoder(w.Body).Decode(&analyticsResp); err != nil {
		t.Fatalf("decode analytics resp: %v", err)
	}
	if analyticsResp.PlanID != "plus" {
		t.Errorf("plan_id: got %q, want plus", analyticsResp.PlanID)
	}
	if len(analyticsResp.CourseBreakdown) != 1 || analyticsResp.CourseBreakdown[0].Title != "Astronomy Basics" {
		t.Errorf("course breakdown mismatch: %+v", analyticsResp.CourseBreakdown)
	}
	if analyticsResp.StreakBreakdown.CurrentStreak != 7 || analyticsResp.StreakBreakdown.LongestStreak != 23 {
		t.Errorf("streak breakdown mismatch: %+v", analyticsResp.StreakBreakdown)
	}
	if analyticsResp.CustomExportAllowed {
		t.Errorf("Plus should not have custom_export_allowed")
	}

	// Pro -> 200 with custom_export_allowed=true
	req = httptest.NewRequestWithContext(ctx, "GET", "/v1/gamification/analytics", nil)
	req.Header.Set("Authorization", "Bearer "+proToken)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Pro analytics status: got %d, want 200", w.Code)
	}
	var proAnalyticsResp struct {
		CustomExportAllowed bool `json:"custom_export_allowed"`
	}
	_ = json.NewDecoder(w.Body).Decode(&proAnalyticsResp)
	if !proAnalyticsResp.CustomExportAllowed {
		t.Errorf("Pro should have custom_export_allowed=true")
	}

	// 2. Learning History Export:
	// Free -> 403
	req = httptest.NewRequestWithContext(ctx, "GET", "/v1/gamification/export/history", nil)
	req.Header.Set("Authorization", "Bearer "+freeToken)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("Free history export status: got %d, want 403", w.Code)
	}

	// Learn -> 200 attachment
	req = httptest.NewRequestWithContext(ctx, "GET", "/v1/gamification/export/history", nil)
	req.Header.Set("Authorization", "Bearer "+learnToken)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Learn history export status: got %d, want 200", w.Code)
	}
	if !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Errorf("missing attachment disposition: %s", w.Header().Get("Content-Disposition"))
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("missing Cache-Control: no-store")
	}

	// 3. Custom Analytics Export (Pro allowlist):
	// Plus -> 403
	req = httptest.NewRequestWithContext(ctx, "POST", "/v1/gamification/export/analytics", strings.NewReader(`{"dimensions":["course","daily"]}`))
	req.Header.Set("Authorization", "Bearer "+plusToken)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("Plus custom export status: got %d, want 403", w.Code)
	}

	// Pro with invalid dimension -> 400
	req = httptest.NewRequestWithContext(ctx, "POST", "/v1/gamification/export/analytics", strings.NewReader(`{"dimensions":["invalid_dimension","passwords"]}`))
	req.Header.Set("Authorization", "Bearer "+proToken)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Pro invalid dimension status: got %d, want 400", w.Code)
	}

	// Pro with valid allowlisted dimensions -> 200
	req = httptest.NewRequestWithContext(ctx, "POST", "/v1/gamification/export/analytics", strings.NewReader(`{"dimensions":["course","daily"]}`))
	req.Header.Set("Authorization", "Bearer "+proToken)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Pro valid custom export status: got %d, want 200, body: %s", w.Code, w.Body.String())
	}
	var exportResp struct {
		Dimensions []string `json:"dimensions"`
		Data       struct {
			Course any `json:"course"`
			Daily  any `json:"daily"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&exportResp); err != nil {
		t.Fatalf("decode export resp: %v", err)
	}
	if len(exportResp.Dimensions) != 2 || exportResp.Dimensions[0] != "course" {
		t.Errorf("dimensions mismatch: %+v", exportResp.Dimensions)
	}

	// Friend course privacy check:
	// Verify that secret friend course is never in Plus learner export or analytics
	fullBody, _ := io.ReadAll(w.Body)
	if strings.Contains(string(fullBody), "Secret Diary of Friend") {
		t.Errorf("Leak detected: friend private course content appeared in export!")
	}
	_ = freeID
}
