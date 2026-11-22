package pricing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func routePolicy(primaryProvider, fallbackProvider string) QuotePolicy {
	return QuotePolicy{
		ActionModels:   map[string]string{"preview": "p", "outline": "p", "tutor_short": "p"},
		FallbackModels: map[string]string{"p": "f"},
		Models: map[string]ModelRate{
			"p": {Provider: primaryProvider, Model: "claude-sonnet-5-5"},
			"f": {Provider: fallbackProvider, Model: "anthropic/claude-sonnet-5.5"},
		},
	}
}

func TestUnservedRoutesNamesActionAndBothRoutes(t *testing.T) {
	cfg := Config{Version: "v1", Actions: map[string]Action{"preview": {}, "outline": {}}}
	gaps := UnservedRoutes(cfg, routePolicy("anthropic", "openrouter"), []string{"openrouter", "openrouter"})
	if len(gaps) != 2 {
		t.Fatalf("want preview and outline (tutor_short is not an action of v1), got %+v", gaps)
	}
	g := gaps[1] // sorted by action
	if g.Action != "preview" || g.Version != "v1" ||
		g.Quote != "anthropic/claude-sonnet-5-5 -> openrouter/anthropic/claude-sonnet-5.5" ||
		g.Engine != "openrouter -> openrouter" {
		t.Fatalf("gap %+v", g)
	}
}

func TestUnservedRoutesEmptyWhenProvidersMatch(t *testing.T) {
	cfg := Config{Version: "v1", Actions: map[string]Action{"preview": {}}}
	if gaps := UnservedRoutes(cfg, routePolicy("openrouter", "openrouter"), []string{"openrouter", "openrouter"}); len(gaps) != 0 {
		t.Fatalf("unexpected gaps %+v", gaps)
	}
}

func TestReportUnservedRoutesLogsErrors(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&out, nil))
	cfg := Config{Version: "v1", Actions: map[string]Action{"preview": {}}}
	ReportUnservedRoutes(context.Background(), logger, cfg, routePolicy("anthropic", "openrouter"), func(context.Context) ([]string, error) {
		return []string{"mock", "mock"}, nil
	})
	var line map[string]any
	if err := json.Unmarshal(out.Bytes(), &line); err != nil {
		t.Fatalf("log %q: %v", out.String(), err)
	}
	if line["level"] != "ERROR" || line["action"] != "preview" || line["engine_route"] != "mock -> mock" ||
		!strings.Contains(line["quote_route"].(string), "anthropic/claude-sonnet-5-5") || line["pricing_version"] != "v1" {
		t.Fatalf("log line %v", line)
	}

	out.Reset()
	ReportUnservedRoutes(context.Background(), logger, cfg, routePolicy("anthropic", "openrouter"), func(context.Context) ([]string, error) {
		return nil, errors.New("down")
	})
	if !strings.Contains(out.String(), `"level":"WARN"`) {
		t.Fatalf("engine unreachable must warn, got %q", out.String())
	}
}
