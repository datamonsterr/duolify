package analytics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakePostHog implements the few PostHog endpoints the setup tool uses.
type fakePostHog struct {
	mu         sync.Mutex
	dashboards []map[string]any
	insights   map[int]map[string]any
	nextID     int
	queries    []string
	events     int
	auth       []string
}

func newFake() (*fakePostHog, *httptest.Server) {
	f := &fakePostHog{insights: map[int]map[string]any{}, nextID: 100}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/projects/7/query/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		var body struct{ Query struct{ Query string } }
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.queries = append(f.queries, body.Query.Query)
		_ = json.NewEncoder(w).Encode(map[string]any{"results": [][]any{{f.events}}})
	})
	mux.HandleFunc("/api/projects/7/dashboards/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/7/dashboards/":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": f.dashboards})
		case r.Method == http.MethodPost:
			var d map[string]any
			_ = json.NewDecoder(r.Body).Decode(&d)
			f.nextID++
			d["id"] = f.nextID
			d["tiles"] = []any{}
			f.dashboards = append(f.dashboards, d)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(d)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/projects/7/insights/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		switch r.Method {
		case http.MethodGet:
			list := []any{}
			for _, i := range f.insights {
				list = append(list, i)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": list})
		case http.MethodPost:
			var i map[string]any
			_ = json.NewDecoder(r.Body).Decode(&i)
			f.nextID++
			i["id"] = f.nextID
			f.insights[f.nextID] = i
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(i)
		case http.MethodPatch:
			var i map[string]any
			_ = json.NewDecoder(r.Body).Decode(&i)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(i)
		}
	})
	return f, httptest.NewServer(mux)
}

func TestApplyDashboardIsIdempotent(t *testing.T) {
	f, srv := newFake()
	defer srv.Close()
	api := &PostHogAPI{Base: srv.URL, ProjectID: "7", Token: "personal-secret", HTTP: srv.Client()}
	for run := 0; run < 2; run++ {
		if err := api.ApplyAIDashboard(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.dashboards) != 1 {
		t.Fatalf("dashboards after two runs: %d", len(f.dashboards))
	}
	if want := len(AIInsights()) + len(GuardInsights()); len(f.insights) != want {
		t.Fatalf("insights after two runs: %d want %d", len(f.insights), want)
	}
	for _, a := range f.auth {
		if a != "Bearer personal-secret" {
			t.Fatalf("auth header %q", a)
		}
	}
}

func TestAIInsightsUseLLMAnalyticsProperties(t *testing.T) {
	all := ""
	for _, i := range AIInsights() {
		if i.Name == "" || !strings.Contains(i.Query, "$ai_generation") {
			t.Fatalf("insight %q must query $ai_generation", i.Name)
		}
		all += i.Query
	}
	for _, p := range []string{"$ai_model", "$ai_latency", "$ai_total_cost_usd", "fallback_used", "$ai_is_error", "$ai_input_tokens"} {
		if !strings.Contains(all, p) {
			t.Errorf("no insight uses %s", p)
		}
	}
}

func TestCountEvents(t *testing.T) {
	f, srv := newFake()
	defer srv.Close()
	f.events = 2
	api := &PostHogAPI{Base: srv.URL, ProjectID: "7", Token: "t", HTTP: srv.Client()}
	n, err := api.CountEvents(context.Background(), "duolify_integration_check")
	if err != nil || n != 2 {
		t.Fatalf("got %d, %v", n, err)
	}
	if strings.Contains(f.queries[0], "'; ") || !strings.Contains(f.queries[0], "duolify_integration_check") {
		t.Fatalf("query %q", f.queries[0])
	}
}

func TestCountEventsRejectsUnsafeName(t *testing.T) {
	api := &PostHogAPI{Base: "http://invalid", ProjectID: "7", Token: "t"}
	if _, err := api.CountEvents(context.Background(), "x' or 1=1 --"); err == nil {
		t.Fatal("expected error")
	}
}

func TestAPIErrorsDoNotLeakToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	defer srv.Close()
	api := &PostHogAPI{Base: srv.URL, ProjectID: "7", Token: "personal-secret", HTTP: srv.Client()}
	_, err := api.CountEvents(context.Background(), "duolify_integration_check")
	if err == nil || strings.Contains(err.Error(), "personal-secret") {
		t.Fatalf("err = %v", err)
	}
}

func TestGuardInsightsUseOnlyTheClosedGuardProperties(t *testing.T) {
	for _, i := range GuardInsights() {
		if !strings.Contains(i.Query, "input_guard_decision") {
			t.Fatalf("insight %q must query input_guard_decision", i.Name)
		}
		for _, banned := range []string{"properties.text", "properties.message", "properties.note"} {
			if strings.Contains(i.Query, banned) {
				t.Fatalf("insight %q reads guarded text (%s)", i.Name, banned)
			}
		}
	}
	routed := ""
	for _, i := range AIInsights() {
		routed += i.Query
	}
	for _, p := range []string{"router_source", "prompt_mode", "cache_read_tokens"} {
		if !strings.Contains(routed, p) {
			t.Errorf("no AI insight uses %s", p)
		}
	}
}
