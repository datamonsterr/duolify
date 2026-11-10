package accounts_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
)

type testEngineClient struct {
	resp *enginev1.SetupChatResponse
	err  error
}

func (c *testEngineClient) SetupChat(_ context.Context, _ *enginev1.SetupChatRequest) (*enginev1.SetupChatResponse, error) {
	return c.resp, c.err
}

func TestSetupChatUnit(t *testing.T) {
	devConfig := pricing.Config{
		Version:  "usd-dev-test",
		Currency: "USD",
		Actions: map[string]pricing.Action{
			"setup_chat": {Credits: 0},
		},
	}
	catalogue := pricing.FromConfig(devConfig)

	for _, field := range []string{"goal", "topic", "background", "level", "locale", "time_zone", "reminder_time"} {
		t.Run("reject oversized draft "+field, func(t *testing.T) {
			limit := 64
			switch field {
			case "goal", "topic", "background":
				limit = 2000
			case "reminder_time":
				limit = 5
			}
			service := accounts.NewTestService(t)
			service.ConfigureSetupChat(accounts.SetupChatOptions{Engine: &testEngineClient{err: errors.New("must not call")}, Pricing: catalogue})
			body, _ := json.Marshal(map[string]any{"current_step": "goal", "answer": "ok", "draft": map[string]string{field: strings.Repeat("x", limit+1)}})
			r := httptest.NewRequestWithContext(t.Context(), "POST", "/v1/me/setup-chat", strings.NewReader(string(body)))
			r = r.WithContext(accounts.WithTestLearner(r.Context(), "a52c3fe5-fdbb-4d40-9a4f-56bbcf79df72"))
			w := httptest.NewRecorder()
			service.HandleSetupChat(w, r)
			if w.Code != 400 {
				t.Fatalf("oversized draft reached engine: %d", w.Code)
			}
		})
	}

	t.Run("single follow-up bound and question flow", func(t *testing.T) {
		mockClient := &testEngineClient{
			resp: &enginev1.SetupChatResponse{
				Message:    "Space is a great pick! Is this for fun, for school, or for work?",
				IsFollowUp: true,
				NextStep:   "goal",
				UpdatedProfile: &enginev1.LearnerProfile{
					Topic: "Astronomy",
				},
				Usage: &enginev1.Usage{Provider: "mock", Model: "claude-haiku-5-5"},
			},
		}

		service := accounts.NewTestService(t)
		service.ConfigureSetupChat(accounts.SetupChatOptions{
			Engine:  mockClient,
			Pricing: catalogue,
		})

		body, _ := json.Marshal(map[string]any{
			"current_step": "goal",
			"answer":       "Something about space I guess",
			"is_follow_up": false,
		})
		r := httptest.NewRequestWithContext(t.Context(), "POST", "/v1/me/setup-chat", strings.NewReader(string(body)))
		r = r.WithContext(accounts.WithTestLearner(r.Context(), "a52c3fe5-fdbb-4d40-9a4f-56bbcf79df72"))
		w := httptest.NewRecorder()

		service.HandleSetupChat(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d %s", w.Code, w.Body)
		}

		var res struct {
			IsFollowUp bool   `json:"is_follow_up"`
			NextStep   string `json:"next_step"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if !res.IsFollowUp || res.NextStep != "goal" {
			t.Fatalf("expected single follow-up, got %+v", res)
		}
	})

	t.Run("skipped fields preserve profile values without inventing data", func(t *testing.T) {
		mockClient := &testEngineClient{
			resp: &enginev1.SetupChatResponse{
				Message:    "What level would you describe yourself as?",
				IsFollowUp: false,
				NextStep:   "level",
				UpdatedProfile: &enginev1.LearnerProfile{
					Topic: "Astronomy",
				},
				Usage: &enginev1.Usage{Provider: "mock", Model: "claude-haiku-5-5"},
			},
		}

		service := accounts.NewTestService(t)
		service.ConfigureSetupChat(accounts.SetupChatOptions{
			Engine:  mockClient,
			Pricing: catalogue,
		})

		body, _ := json.Marshal(map[string]any{
			"current_step": "goal",
			"skipped":      true,
		})
		r := httptest.NewRequestWithContext(t.Context(), "POST", "/v1/me/setup-chat", strings.NewReader(string(body)))
		r = r.WithContext(accounts.WithTestLearner(r.Context(), "a52c3fe5-fdbb-4d40-9a4f-56bbcf79df72"))
		w := httptest.NewRecorder()

		service.HandleSetupChat(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d %s", w.Code, w.Body)
		}

		var res struct {
			IsFollowUp bool   `json:"is_follow_up"`
			NextStep   string `json:"next_step"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res.IsFollowUp || res.NextStep != "level" {
			t.Fatalf("expected advance to level without follow-up, got %+v", res)
		}
	})

	t.Run("bounded failure on provider error falls back safely", func(t *testing.T) {
		mockClient := &testEngineClient{
			err: errors.New("provider timeout"),
		}

		service := accounts.NewTestService(t)
		service.ConfigureSetupChat(accounts.SetupChatOptions{
			Engine:  mockClient,
			Pricing: catalogue,
		})

		body, _ := json.Marshal(map[string]any{
			"current_step": "goal",
			"answer":       "Astronomy",
		})
		r := httptest.NewRequestWithContext(t.Context(), "POST", "/v1/me/setup-chat", strings.NewReader(string(body)))
		r = r.WithContext(accounts.WithTestLearner(r.Context(), "a52c3fe5-fdbb-4d40-9a4f-56bbcf79df72"))
		w := httptest.NewRecorder()

		service.HandleSetupChat(w, r)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503 on engine error, got %d %s", w.Code, w.Body)
		}
	})

	t.Run("summary field validation", func(t *testing.T) {
		service := accounts.NewTestService(t)
		service.ConfigureSetupChat(accounts.SetupChatOptions{
			Engine:  &testEngineClient{},
			Pricing: catalogue,
		})

		// Text too long
		body, _ := json.Marshal(map[string]any{
			"current_step": "goal",
			"answer":       strings.Repeat("a", 3000),
		})
		r := httptest.NewRequestWithContext(t.Context(), "POST", "/v1/me/setup-chat", strings.NewReader(string(body)))
		r = r.WithContext(accounts.WithTestLearner(r.Context(), "a52c3fe5-fdbb-4d40-9a4f-56bbcf79df72"))
		w := httptest.NewRecorder()

		service.HandleSetupChat(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 on over-limit text, got %d", w.Code)
		}
	})
}
