package gamification

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
)

type mockEntitlementService struct {
	plan *credits.FundedPlan
	err  error
}

func (m *mockEntitlementService) FundedPlan(_ context.Context, _ string, _ time.Time) (*credits.FundedPlan, error) {
	return m.plan, m.err
}

func TestEntitlementPermissions(t *testing.T) {
	now := time.Now().UTC()
	future := now.Add(24 * time.Hour)
	past := now.Add(-24 * time.Hour)

	cases := []struct {
		name          string
		plan          *credits.FundedPlan
		wantHistory   bool
		wantAnalytics bool
		wantCustom    bool
	}{
		{
			name:          "nil plan",
			plan:          nil,
			wantHistory:   false,
			wantAnalytics: false,
			wantCustom:    false,
		},
		{
			name: "expired plan",
			plan: &credits.FundedPlan{
				PlanID:      "pro",
				Name:        "Pro",
				Active:      false,
				PaidThrough: past,
				Benefits:    []string{"Learning history export", "Advanced learning analytics", "Custom learning analytics exports"},
			},
			wantHistory:   false,
			wantAnalytics: false,
			wantCustom:    false,
		},
		{
			name: "revoked plan",
			plan: &credits.FundedPlan{
				PlanID:      "pro",
				Name:        "Pro",
				Active:      true,
				Revoked:     true,
				PaidThrough: future,
				Benefits:    []string{"Learning history export", "Advanced learning analytics", "Custom learning analytics exports"},
			},
			wantHistory:   false,
			wantAnalytics: false,
			wantCustom:    false,
		},
		{
			name: "active learn plan",
			plan: &credits.FundedPlan{
				PlanID:      "learn",
				Name:        "Learn",
				Active:      true,
				PaidThrough: future,
				Benefits:    []string{"Learning history export"},
			},
			wantHistory:   true,
			wantAnalytics: false,
			wantCustom:    false,
		},
		{
			name: "active plus plan",
			plan: &credits.FundedPlan{
				PlanID:      "plus",
				Name:        "Plus",
				Active:      true,
				PaidThrough: future,
				Benefits:    []string{"Learning history export", "Advanced learning analytics"},
			},
			wantHistory:   true,
			wantAnalytics: true,
			wantCustom:    false,
		},
		{
			name: "active pro plan",
			plan: &credits.FundedPlan{
				PlanID:      "pro",
				Name:        "Pro",
				Active:      true,
				PaidThrough: future,
				Benefits:    []string{"Learning history export", "Advanced learning analytics", "Custom learning analytics exports"},
			},
			wantHistory:   true,
			wantAnalytics: true,
			wantCustom:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isEntitledToHistoryExport(tc.plan); got != tc.wantHistory {
				t.Errorf("history export: got %v, want %v", got, tc.wantHistory)
			}
			if got := isEntitledToAnalytics(tc.plan); got != tc.wantAnalytics {
				t.Errorf("analytics: got %v, want %v", got, tc.wantAnalytics)
			}
			if got := isEntitledToCustomExport(tc.plan); got != tc.wantCustom {
				t.Errorf("custom export: got %v, want %v", got, tc.wantCustom)
			}
		})
	}
}

func TestExportDimensionsAllowlist(t *testing.T) {
	cases := []struct {
		name       string
		dimensions []string
		wantErr    bool
	}{
		{
			name:       "valid single dimension",
			dimensions: []string{"course"},
			wantErr:    false,
		},
		{
			name:       "valid multiple dimensions",
			dimensions: []string{"course", "daily", "streak", "completions", "leagues"},
			wantErr:    false,
		},
		{
			name:       "empty dimensions",
			dimensions: []string{},
			wantErr:    true,
		},
		{
			name:       "invalid dimension",
			dimensions: []string{"credit_cards"},
			wantErr:    true,
		},
		{
			name:       "mixed valid and invalid",
			dimensions: []string{"course", "passwords"},
			wantErr:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateExportDimensions(tc.dimensions)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateExportDimensions() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestAnalyticsEndpointAuthentication(t *testing.T) {
	ctx := t.Context()
	svc := &Service{}
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(ctx, "GET", "/v1/gamification/analytics", nil)
	svc.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated analytics status = %d, want 401", w.Code)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequestWithContext(ctx, "GET", "/v1/gamification/export/history", nil)
	svc.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated history export status = %d, want 401", w.Code)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequestWithContext(ctx, "POST", "/v1/gamification/export/analytics", strings.NewReader(`{"dimensions":["course"]}`))
	svc.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated custom export status = %d, want 401", w.Code)
	}
}

func TestAnalyticsEntitlementRejection(t *testing.T) {
	svc := &Service{}
	mock := &mockEntitlementService{
		plan: &credits.FundedPlan{
			PlanID:      "learn",
			Name:        "Learn",
			Active:      true,
			PaidThrough: time.Now().Add(24 * time.Hour),
			Benefits:    []string{"Learning history export"},
		},
	}
	svc.UseEntitlements(mock)

	// Analytics endpoint requires Plus or Pro -> Learn gets 403
	ctx := accounts.WithLearnerID(t.Context(), "a1111111-1111-4111-8111-111111111111")
	req := httptest.NewRequestWithContext(ctx, "GET", "/v1/gamification/analytics", nil)
	w := httptest.NewRecorder()
	svc.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("Learn accessing analytics got status %d, want 403", w.Code)
	}

	// Custom export endpoint requires Pro -> Learn gets 403
	req = httptest.NewRequestWithContext(ctx, "POST", "/v1/gamification/export/analytics", strings.NewReader(`{"dimensions":["course"]}`))
	w = httptest.NewRecorder()
	svc.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("Learn accessing custom export got status %d, want 403", w.Code)
	}
}

// Entitlement comes only from the verified benefits of the funded term (version-aware);
// a plan id or name never grants a benefit by itself.
func TestEntitlementIgnoresPlanNameWithoutBenefit(t *testing.T) {
	plan := &credits.FundedPlan{PlanID: "pro_custom", Name: "Pro", Active: true, PaidThrough: time.Now().Add(time.Hour)}
	if isEntitledToHistoryExport(plan) || isEntitledToAnalytics(plan) || isEntitledToCustomExport(plan) {
		t.Fatal("plan name/id without a benefit must not entitle")
	}
}

func TestDeniedExportsExplainWhy(t *testing.T) {
	ctx := accounts.WithLearnerID(t.Context(), "a1111111-1111-4111-8111-111111111111")
	future := time.Now().Add(time.Hour)
	cases := []struct {
		name         string
		plan         *credits.FundedPlan
		path         string
		wantCode     string
		wantSuppress bool
	}{
		{"none history", nil, "/v1/gamification/export/history", "subscription_required", false},
		{"expired history", &credits.FundedPlan{PlanID: "learn", Active: false, Benefits: []string{"Learning history export"}}, "/v1/gamification/export/history", "subscription_expired", false},
		{"expired custom", &credits.FundedPlan{PlanID: "pro", Active: false, Benefits: []string{"Custom learning analytics exports"}}, "/v1/gamification/export/analytics?dimensions=course", "subscription_expired", false},
		{"learn analytics suppresses upsell", &credits.FundedPlan{PlanID: "learn", Active: true, PaidThrough: future, Benefits: []string{"Learning history export", "No upsell banners"}}, "/v1/gamification/analytics", "subscription_required", true},
		{"free analytics upsell ok", nil, "/v1/gamification/analytics", "subscription_required", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &Service{}
			svc.UseEntitlements(&mockEntitlementService{plan: tc.plan})
			w := httptest.NewRecorder()
			svc.ServeHTTP(w, httptest.NewRequestWithContext(ctx, "GET", tc.path, nil))
			var body struct {
				Code           string `json:"code"`
				UpsellSuppress bool   `json:"upsell_suppressed"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &body)
			if w.Code != http.StatusForbidden || body.Code != tc.wantCode || body.UpsellSuppress != tc.wantSuppress {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestEntitlementLookupFailureIs503(t *testing.T) {
	svc := &Service{}
	svc.UseEntitlements(&mockEntitlementService{err: errors.New("down")})
	ctx := accounts.WithLearnerID(t.Context(), "a1111111-1111-4111-8111-111111111111")
	for _, p := range []string{"/v1/gamification/analytics", "/v1/gamification/export/history", "/v1/gamification/export/analytics"} {
		w := httptest.NewRecorder()
		svc.ServeHTTP(w, httptest.NewRequestWithContext(ctx, "GET", p, nil))
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s status=%d", p, w.Code)
		}
	}
}

func TestNormalizeExportDimensions(t *testing.T) {
	got, err := normalizeExportDimensions([]string{" Daily", "course", "daily", "COURSE"})
	if err != nil || strings.Join(got, ",") != "course,daily" {
		t.Fatalf("got %v %v", got, err)
	}
	for _, bad := range [][]string{nil, {"course", "x;DROP TABLE xp_events"}, {""}} {
		if _, err := normalizeExportDimensions(bad); err == nil {
			t.Fatalf("%v accepted", bad)
		}
	}
}
