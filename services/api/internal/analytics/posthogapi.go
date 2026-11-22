package analytics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// PostHogAPI is the small slice of the PostHog REST API used by `posthog-setup`:
// HogQL queries to confirm ingestion and idempotent dashboard provisioning.
// Base is the app host (https://us.posthog.com), not the ingestion host.
type PostHogAPI struct {
	Base      string
	ProjectID string
	Token     string // personal API key; never logged or returned in errors
	HTTP      *http.Client
}

// AppHost maps an ingestion host (us.i.posthog.com) to its app/API host.
func AppHost(ingestion string) string {
	u, err := url.Parse(ingestion)
	if err != nil || u.Host == "" {
		return "https://us.posthog.com"
	}
	host := strings.Replace(u.Host, ".i.posthog.com", ".posthog.com", 1)
	return "https://" + host
}

const managedTag = "managed-by:duolify"

// AIDashboardName is the dashboard owned by ApplyAIDashboard.
const AIDashboardName = "Duolify AI reliability and cost"

// Insight is a named HogQL insight on the dashboard.
type Insight struct{ Name, Query string }

// AIInsights are the reliability and AI-cost views over `$ai_generation` events
// (model = the model that served the call, latency in seconds, cost in USD).
func AIInsights() []Insight {
	const g = "event = '$ai_generation' AND properties.feature != 'integration_check' AND timestamp >= now() - INTERVAL 30 DAY"
	return []Insight{
		{"Generations by served model per day", "SELECT toStartOfDay(timestamp) AS day, properties.$ai_model AS model, count() AS generations FROM events WHERE " + g + " GROUP BY day, model ORDER BY day"},
		{"Fallback rate by feature", "SELECT properties.feature AS feature, countIf(properties.fallback_used = true) / count() AS fallback_rate, count() AS generations FROM events WHERE " + g + " GROUP BY feature ORDER BY fallback_rate DESC"},
		{"Error rate by feature", "SELECT properties.feature AS feature, countIf(properties.$ai_is_error = true) / count() AS error_rate, count() AS generations FROM events WHERE " + g + " GROUP BY feature ORDER BY error_rate DESC"},
		{"Latency p50 and p95 by model", "SELECT properties.$ai_model AS model, quantile(0.5)(toFloat(properties.$ai_latency)) AS p50_seconds, quantile(0.95)(toFloat(properties.$ai_latency)) AS p95_seconds FROM events WHERE " + g + " GROUP BY model"},
		{"Provider cost (USD) by model per day", "SELECT toStartOfDay(timestamp) AS day, properties.$ai_model AS model, sum(toFloat(properties.$ai_total_cost_usd)) AS cost_usd FROM events WHERE " + g + " GROUP BY day, model ORDER BY day"},
		{"Tokens by model", "SELECT properties.$ai_model AS model, sum(toFloat(properties.$ai_input_tokens)) AS input_tokens, sum(toFloat(properties.$ai_output_tokens)) AS output_tokens FROM events WHERE " + g + " GROUP BY model"},
		{"Prompt cache read share by feature", "SELECT properties.feature AS feature, sum(toFloat(properties.cache_read_tokens)) / nullIf(sum(toFloat(properties.$ai_input_tokens)), 0) AS cache_read_share FROM events WHERE " + g + " GROUP BY feature"},
	}
}

var eventName = regexp.MustCompile(`^[A-Za-z0-9_.$-]{1,100}$`)

// CountEvents returns how many events with this name the project holds.
func (a *PostHogAPI) CountEvents(ctx context.Context, event string) (int, error) {
	if !eventName.MatchString(event) {
		return 0, errors.New("invalid event name")
	}
	body := map[string]any{"refresh": "force_blocking", "query": map[string]any{
		"kind": "HogQLQuery", "query": "SELECT count() FROM events WHERE event = '" + event + "'",
	}}
	var out struct{ Results [][]json.Number }
	if err := a.do(ctx, http.MethodPost, "/query/", body, &out); err != nil {
		return 0, err
	}
	if len(out.Results) == 0 || len(out.Results[0]) == 0 {
		return 0, errors.New("empty query result")
	}
	n, err := strconv.Atoi(out.Results[0][0].String())
	return n, err
}

// ApplyAIDashboard creates or updates the dashboard and its insights by name.
func (a *PostHogAPI) ApplyAIDashboard(ctx context.Context) error {
	var list struct {
		Results []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}
	}
	if err := a.do(ctx, http.MethodGet, "/dashboards/?limit=200", nil, &list); err != nil {
		return err
	}
	dashID := 0
	for _, d := range list.Results {
		if d.Name == AIDashboardName {
			dashID = d.ID
		}
	}
	if dashID == 0 {
		var created struct{ ID int }
		err := a.do(ctx, http.MethodPost, "/dashboards/", map[string]any{
			"name": AIDashboardName, "description": "OpenRouter generations (Hy4 Flash, GLM fallback): reliability, latency, tokens, cost. Managed by `mise run analytics:setup`.",
			"tags": []string{managedTag}, "pinned": true,
		}, &created)
		if err != nil {
			return err
		}
		dashID = created.ID
	}
	var existing struct {
		Results []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}
	}
	if err := a.do(ctx, http.MethodGet, "/insights/?saved=true&limit=500&search="+url.QueryEscape("Duolify AI"), nil, &existing); err != nil {
		return err
	}
	byName := map[string]int{}
	for _, i := range existing.Results {
		byName[i.Name] = i.ID
	}
	for _, in := range AIInsights() {
		name := "Duolify AI: " + in.Name
		body := map[string]any{
			"name": name, "saved": true, "tags": []string{managedTag}, "dashboards": []int{dashID},
			"query": map[string]any{"kind": "DataVisualizationNode", "source": map[string]any{"kind": "HogQLQuery", "query": in.Query}},
		}
		if id, ok := byName[name]; ok {
			if err := a.do(ctx, http.MethodPatch, fmt.Sprintf("/insights/%d/", id), body, nil); err != nil {
				return err
			}
			continue
		}
		if err := a.do(ctx, http.MethodPost, "/insights/", body, nil); err != nil {
			return err
		}
	}
	return nil
}

func (a *PostHogAPI) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(a.Base, "/")+"/api/projects/"+url.PathEscape(a.ProjectID)+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.Token)
	req.Header.Set("Content-Type", "application/json")
	c := a.HTTP
	if c == nil {
		c = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("posthog %s %s: %w", method, strings.SplitN(path, "?", 2)[0], errors.Unwrap(err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("posthog %s %s: HTTP %d", method, strings.SplitN(path, "?", 2)[0], resp.StatusCode)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
