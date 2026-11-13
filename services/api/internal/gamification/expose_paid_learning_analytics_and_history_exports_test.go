package gamification

import (
	"context"
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
