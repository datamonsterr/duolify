//go:build integration

package accounts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type mockEngineServer struct {
	enginev1.UnimplementedEngineServiceServer
	mu       sync.Mutex
	calls    int
	lastStep string
}

func (m *mockEngineServer) SetupChat(_ context.Context, req *enginev1.SetupChatRequest) (*enginev1.SetupChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.lastStep = req.CurrentStep

	profile := req.Profile
	if profile == nil {
		profile = &enginev1.LearnerProfile{}
	}

	if req.CurrentStep == "goal" {
		if !req.IsFollowUp && strings.Contains(strings.ToLower(req.Answer), "space") {
			return &enginev1.SetupChatResponse{
				Message: "Space is a great pick! Is this for fun, for school, or for work?",
				QuickReplies: []*enginev1.QuickReply{
					{Id: "fun", Label: "For fun"},
					{Id: "school", Label: "For school"},
					{Id: "work", Label: "For work"},
					{Id: "skip", Label: "Skip"},
				},
				UpdatedProfile: &enginev1.LearnerProfile{
					Topic: "Astronomy",
					Goals: req.Answer,
				},
				NextStep:   "goal",
				IsFollowUp: true,
				IsComplete: false,
				Usage: &enginev1.Usage{
					Provider:     "mock",
					Model:        "claude-haiku-5-5",
					InputTokens:  120,
					OutputTokens: 60,
				},
			}, nil
		}

		return &enginev1.SetupChatResponse{
			Message: "What level would you describe yourself as?",
			QuickReplies: []*enginev1.QuickReply{
				{Id: "beginner", Label: "Beginner"},
				{Id: "intermediate", Label: "Intermediate"},
				{Id: "advanced", Label: "Advanced"},
				{Id: "skip", Label: "Skip"},
			},
			UpdatedProfile: &enginev1.LearnerProfile{
				Topic: "Astronomy",
				Goals: "Explain the night sky to my kids",
			},
			NextStep:   "level",
			IsFollowUp: false,
			IsComplete: false,
			Usage: &enginev1.Usage{
				Provider:     "mock",
				Model:        "claude-haiku-5-5",
				InputTokens:  100,
				OutputTokens: 50,
			},
		}, nil
	}

	return &enginev1.SetupChatResponse{
		Message:        "Here's what I've got. Does this look right?",
		UpdatedProfile: profile,
		NextStep:       "confirm",
		IsComplete:     true,
		Usage: &enginev1.Usage{
			Provider:     "mock",
			Model:        "claude-haiku-5-5",
			InputTokens:  80,
			OutputTokens: 40,
		},
	}, nil
}

type engineAdapter struct {
	client enginev1.EngineServiceClient
}

func (a *engineAdapter) SetupChat(ctx context.Context, req *enginev1.SetupChatRequest) (*enginev1.SetupChatResponse, error) {
	return a.client.SetupChat(ctx, req)
}

func TestSetupChatIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("setup_chat_test"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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

	// PostHog event sink
	var mu sync.Mutex
	var captured []map[string]any
	sinkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Batch []map[string]any `json:"batch"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		mu.Lock()
		captured = append(captured, body.Batch...)
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer sinkServer.Close()

	events, err := analytics.New("test-only", sinkServer.URL, func(context.Context, string) (bool, error) { return true, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = events.Close() }()

	// Start mock gRPC engine server
	mockEngine := &mockEngineServer{}
	lis, err := new(net.ListenConfig).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	grpcServer := grpc.NewServer()
	enginev1.RegisterEngineServiceServer(grpcServer, mockEngine)
	go func() { _ = grpcServer.Serve(lis) }()
	defer grpcServer.Stop()

	// gRPC client connection
	conn, err := grpc.NewClient("passthrough:///"+lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	rawEngineClient := enginev1.NewEngineServiceClient(conn)
	engineClient := &engineAdapter{client: rawEngineClient}

	// Auth service
	mail := new(accounts.MailSink)
	auth, err := accounts.New(db.Pool, strings.Repeat("test-key", 5), mail)
	if err != nil {
		t.Fatal(err)
	}
	auth.ConfigureProfile(accounts.ProfileOptions{TextLimit: 2000, Events: events})

	// Load pricing catalogue
	catalogue, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}

	call := func(method, path string, body any, token string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequestWithContext(ctx, method, path, bytes.NewReader(raw))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		auth.ServeHTTP(w, r)
		return w
	}

	type authedUser struct {
		accounts.Tokens
		LearnerID string
	}
	login := func(email string, optOut bool) authedUser {
		in := map[string]any{"email": email, "age_confirmed": true, "locale": "en-US", "time_zone": "UTC", "analytics_opt_out": optOut}
		if w := call("POST", "/v1/auth/code", in, ""); w.Code != 202 {
			t.Fatalf("request code: %d %s", w.Code, w.Body)
		}
		in["code"] = mail.Code(email)
		w := call("POST", "/v1/auth/verify", in, "")
		if w.Code != 200 {
			t.Fatalf("verify code: %d %s", w.Code, w.Body)
		}
		var tokens accounts.Tokens
		if err := json.Unmarshal(w.Body.Bytes(), &tokens); err != nil {
			t.Fatal(err)
		}
		claims, err := auth.ParseAccess(tokens.AccessToken)
		if err != nil {
			t.Fatal(err)
		}
		return authedUser{Tokens: tokens, LearnerID: claims.Subject}
	}

	t.Run("config-disabled fallback when setup_chat not in pricing", func(t *testing.T) {
		user := login("disabled@example.test", false)
		// With production pricing (setup_chat not enabled), calling setup-chat returns 503
		auth.ConfigureSetupChat(accounts.SetupChatOptions{
			Engine:  engineClient,
			Pricing: catalogue, // Launch catalogue has no setup_chat
			Events:  events,
		})

		w := call("POST", "/v1/me/setup-chat", map[string]any{
			"current_step": "goal",
			"answer":       "Something about space",
		}, user.AccessToken)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503 when setup_chat disabled in config, got %d %s", w.Code, w.Body)
		}
	})

	t.Run("roundtrip setup chat contract, confirm-only writes, opt-out, zero charges", func(t *testing.T) {
		// Provide development pricing catalogue that includes unapproved setup_chat key
		devConfig := catalogue.Config()
		devConfig.Actions["setup_chat"] = pricing.Action{Credits: 0}
		devCatalogue := pricing.FromConfig(devConfig)

		auth.ConfigureSetupChat(accounts.SetupChatOptions{
			Engine:  engineClient,
			Pricing: devCatalogue,
			Events:  events,
		})

		user := login("learner@example.test", false)
		optOutUser := login("optout@example.test", true)

		// 1. Anonymous request rejected
		if w := call("POST", "/v1/me/setup-chat", map[string]any{"current_step": "goal", "answer": "hi"}, ""); w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for anonymous setup-chat, got %d", w.Code)
		}

		// 2. Roundtrip vague answer -> follow-up
		w := call("POST", "/v1/me/setup-chat", map[string]any{
			"current_step": "goal",
			"answer":       "Something about space I guess",
			"is_follow_up": false,
		}, user.AccessToken)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d %s", w.Code, w.Body)
		}

		var res struct {
			Message        string              `json:"message"`
			QuickReplies   []map[string]string `json:"quick_replies"`
			UpdatedProfile map[string]any      `json:"updated_profile"`
			NextStep       string              `json:"next_step"`
			IsFollowUp     bool                `json:"is_follow_up"`
			IsComplete     bool                `json:"is_complete"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		if !res.IsFollowUp || !strings.Contains(res.Message, "Space is a great pick") {
			t.Fatalf("expected follow-up, got %+v", res)
		}
		if res.UpdatedProfile["topic"] != "Astronomy" {
			t.Fatalf("expected topic Astronomy, got %v", res.UpdatedProfile["topic"])
		}

		// 3. Confirm-only writes: verify profile has NOT been written in database
		profileW := call("GET", "/v1/me/profile", nil, user.AccessToken)
		if profileW.Code != http.StatusOK {
			t.Fatalf("get profile: %d", profileW.Code)
		}
		var initialProf map[string]any
		_ = json.Unmarshal(profileW.Body.Bytes(), &initialProf)
		if initialProf["setup_completed"] == true || initialProf["topic"] != "" {
			t.Fatalf("profile was written before confirmation: %+v", initialProf)
		}

		// 4. Zero learner charges: check credit_entries table
		var creditCount int
		err = db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM credit_entries WHERE learner_id = $1::uuid", user.LearnerID).Scan(&creditCount)
		if err != nil {
			t.Fatal(err)
		}
		if creditCount != 0 {
			t.Fatalf("expected 0 credit entries for free setup chat, got %d", creditCount)
		}

		// 5. Opt-out: opted-out learner calls setup-chat
		optW := call("POST", "/v1/me/setup-chat", map[string]any{
			"current_step": "goal",
			"answer":       "Something about space I guess",
		}, optOutUser.AccessToken)
		if optW.Code != http.StatusOK {
			t.Fatalf("expected 200 for opt-out user, got %d %s", optW.Code, optW.Body)
		}

		// 6. Explicit confirmation writes profile
		saveW := call("POST", "/v1/me/profile", map[string]any{
			"confirmed": true,
			"action":    "save",
			"fields": map[string]any{
				"topic": "Astronomy",
				"goal":  "Explain the night sky to my kids",
				"level": "beginner",
			},
		}, user.AccessToken)
		if saveW.Code != http.StatusOK {
			t.Fatalf("expected 200 on explicit profile confirm, got %d %s", saveW.Code, saveW.Body)
		}

		// Verify profile is now confirmed in database
		afterW := call("GET", "/v1/me/profile", nil, user.AccessToken)
		var savedProf map[string]any
		_ = json.Unmarshal(afterW.Body.Bytes(), &savedProf)
		if savedProf["setup_completed"] != true || savedProf["topic"] != "Astronomy" {
			t.Fatalf("expected saved profile, got %+v", savedProf)
		}

		// Flush PostHog events
		if err := events.Close(); err != nil {
			t.Fatal(err)
		}

		// 7. Verify analytics events
		mu.Lock()
		defer mu.Unlock()
		var foundAiEvent, foundProfileUpdated, foundOnboardingCompleted bool
		for _, event := range captured {
			if event["distinct_id"] == optOutUser.LearnerID {
				t.Fatalf("opted-out user produced event: %+v", event)
			}
			if event["distinct_id"] == user.LearnerID {
				switch event["event"] {
				case "$ai_generation":
					foundAiEvent = true
					props, _ := event["properties"].(map[string]any)
					if props["provider"] != "mock" || props["model"] != "claude-haiku-5-5" {
						t.Fatalf("invalid $ai_generation properties: %+v", props)
					}
					// Verify NO private text in event properties
					rawProps, _ := json.Marshal(props)
					if strings.Contains(string(rawProps), "space") || strings.Contains(string(rawProps), "Astronomy") {
						t.Fatalf("private learner text leaked into analytics: %s", string(rawProps))
					}
				case "learner_profile_updated":
					foundProfileUpdated = true
				case "onboarding_completed":
					foundOnboardingCompleted = true
				}
			}
		}
		if !foundAiEvent {
			t.Fatal("expected $ai_generation event for non-opted-out learner")
		}
		if !foundProfileUpdated || !foundOnboardingCompleted {
			t.Fatalf("expected learner_profile_updated and onboarding_completed, got updated=%v completed=%v", foundProfileUpdated, foundOnboardingCompleted)
		}
	})
}
