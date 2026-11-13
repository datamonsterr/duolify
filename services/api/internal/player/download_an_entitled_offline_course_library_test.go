package player_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/player"
)

type mockEntitlementService struct {
	plan *credits.FundedPlan
	err  error
}

func (m *mockEntitlementService) FundedPlan(_ context.Context, _ string, _ time.Time) (*credits.FundedPlan, error) {
	return m.plan, m.err
}

func TestDownloadAnEntitledOfflineCourseLibraryEntitlement(t *testing.T) {
	cases := []struct {
		name     string
		plan     *credits.FundedPlan
		entitled bool
	}{
		{
			name: "active learn plan with offline library benefit",
			plan: &credits.FundedPlan{
				PlanID:   "learn",
				Active:   true,
				Benefits: []string{"Offline course library downloads", "No upsell banners"},
			},
			entitled: true,
		},
		{
			name: "active plus plan with offline library benefit",
			plan: &credits.FundedPlan{
				PlanID:   "plus",
				Active:   true,
				Benefits: []string{"Offline course library downloads", "Priority generation queue"},
			},
			entitled: true,
		},
		{
			name: "active pro plan with offline library benefit",
			plan: &credits.FundedPlan{
				PlanID:   "pro",
				Active:   true,
				Benefits: []string{"Offline course library downloads", "Higher concurrent generation limit"},
			},
			entitled: true,
		},
		{
			name: "expired subscription",
			plan: &credits.FundedPlan{
				PlanID:   "learn",
				Active:   false,
				Benefits: []string{"Offline course library downloads"},
			},
			entitled: false,
		},
		{
			name: "revoked subscription",
			plan: &credits.FundedPlan{
				PlanID:   "plus",
				Active:   false,
				Revoked:  true,
				Benefits: []string{"Offline course library downloads"},
			},
			entitled: false,
		},
		{
			name: "active plan without offline downloads benefit",
			plan: &credits.FundedPlan{
				PlanID:   "other",
				Active:   true,
				Benefits: []string{"Basic access only"},
			},
			entitled: false,
		},
		{
			name:     "no plan / nil",
			plan:     nil,
			entitled: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := player.IsPlanEntitledToDownload(tc.plan)
			if got != tc.entitled {
				t.Fatalf("expected entitled=%v, got=%v for plan %+v", tc.entitled, got, tc.plan)
			}
		})
	}
}

func TestDownloadLessonItemDerivation(t *testing.T) {
	tests := []struct {
		name         string
		status       string
		hasManifest  bool
		unlocked     bool
		downloadable bool
		hasURL       bool
	}{
		{
			name:         "locked stub without manifest",
			status:       "locked",
			hasManifest:  false,
			unlocked:     false,
			downloadable: false,
			hasURL:       false,
		},
		{
			name:         "locked stub even if manifest exists",
			status:       "locked",
			hasManifest:  true,
			unlocked:     false,
			downloadable: false,
			hasURL:       false,
		},
		{
			name:         "unlocked ready_to_generate without manifest (ungenerated)",
			status:       "ready_to_generate",
			hasManifest:  false,
			unlocked:     true,
			downloadable: false,
			hasURL:       false,
		},
		{
			name:         "unlocked ready_to_generate with manifest (downloadable)",
			status:       "ready_to_generate",
			hasManifest:  true,
			unlocked:     true,
			downloadable: true,
			hasURL:       true,
		},
		{
			name:         "unlocked current with manifest (downloadable)",
			status:       "current",
			hasManifest:  true,
			unlocked:     true,
			downloadable: true,
			hasURL:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := player.ResolveDownloadItem("lesson-1", "unit-1", "Lesson 1", 0, tt.status, tt.hasManifest)
			if item.Unlocked != tt.unlocked {
				t.Fatalf("expected Unlocked=%v, got=%v", tt.unlocked, item.Unlocked)
			}
			if item.Downloadable != tt.downloadable {
				t.Fatalf("expected Downloadable=%v, got=%v", tt.downloadable, item.Downloadable)
			}
			if tt.hasURL && item.ManifestURL != "/v1/lessons/lesson-1/manifest" {
				t.Fatalf("expected ManifestURL to be set, got %q", item.ManifestURL)
			}
			if !tt.hasURL && item.ManifestURL != "" {
				t.Fatalf("expected empty ManifestURL, got %q", item.ManifestURL)
			}
		})
	}
}

func TestDownloadLibraryAnalyticsAllowlist(t *testing.T) {
	var mu sync.Mutex
	var received []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Batch []map[string]any `json:"batch"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		mu.Lock()
		received = append(received, body.Batch...)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	events, err := analytics.NewWithOptions("fixture-only", server.URL, analytics.Options{Consent: func(context.Context, string) (bool, error) { return true, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = events.Close() }()

	learnerID := "11111111-1111-4111-8111-111111111111"

	// Test ui_control_activated with download_library
	err = events.Capture(learnerID, "ui_control_activated", map[string]any{
		"platform":    "web",
		"app_version": "development",
		"source":      "api",
		"surface":     "courses",
		"control_id":  "download_library",
		"course_id":   "c1111111-1111-4111-8111-111111111111",
		"private_raw": "must be dropped",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Test ui_recovery_selected for subscription_required
	err = events.Capture(learnerID, "ui_recovery_selected", map[string]any{
		"platform":    "web",
		"app_version": "development",
		"source":      "api",
		"surface":     "courses",
		"action":      "retry",
		"error_code":  "subscription_required",
		"course_id":   "c1111111-1111-4111-8111-111111111111",
		"private_raw": "must be dropped",
	})
	if err != nil {
		t.Fatal(err)
	}

	_ = events.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Fatalf("expected 2 events received, got %d", len(received))
	}

	first := received[0]
	if first["event"] != "ui_control_activated" {
		t.Fatalf("expected ui_control_activated, got %v", first["event"])
	}
	props, _ := first["properties"].(map[string]any)
	if props["control_id"] != "download_library" {
		t.Fatalf("expected control_id=download_library, got %v", props["control_id"])
	}
	if props["private_raw"] != nil {
		t.Fatalf("private raw property was not dropped")
	}

	second := received[1]
	if second["event"] != "ui_recovery_selected" {
		t.Fatalf("expected ui_recovery_selected, got %v", second["event"])
	}
	rProps, _ := second["properties"].(map[string]any)
	if rProps["error_code"] != "subscription_required" {
		t.Fatalf("expected error_code=subscription_required, got %v", rProps["error_code"])
	}
	if rProps["private_raw"] != nil {
		t.Fatalf("private raw property was not dropped")
	}
}
