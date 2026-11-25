package pricing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The engine rejects a quote whose route differs from its own (engine-server PreviewCourse and
// siblings compare provider, model and first fallback). This test fails when the newest
// Hy4-routing quote policy proposal no longer covers engine/config/models.toml, so the drift
// shows up in CI instead of as a 502 at use time.

// quotedActionTasks maps quoted pricing actions to the engine task whose route checks them.
var quotedActionTasks = map[string]string{
	"preview":             "preview",
	"outline":             "course_plan",
	"lesson_content":      "lesson",
	"outline_replan":      "outline_replan",
	"outline_chat_change": "outline_chat_change",
	"character_create":    "character_create",
	"quick_note":          "note",
	"tutor_short":         "chat",
	"tutor_long":          "chat",
	"setup_chat":          "setup_chat",
	"course_brief_chat":   "course_brief_chat",
}

type engineRoute struct{ model, fallback string }

// engineRoutes reads the [tasks.*] model and first fallback from models.toml (flat keys only).
func engineRoutes(t *testing.T, path string) map[string]engineRoute {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read engine routes: %v", err)
	}
	routes := map[string]engineRoute{}
	task := ""
	quoted := regexp.MustCompile(`"([^"]+)"`)
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "[tasks.") && strings.HasSuffix(line, "]"):
			task = strings.TrimSuffix(strings.TrimPrefix(line, "[tasks."), "]")
			routes[task] = engineRoute{}
		case strings.HasPrefix(line, "["):
			task = ""
		case task != "" && strings.HasPrefix(line, "model "):
			r := routes[task]
			r.model = quoted.FindStringSubmatch(line)[1]
			routes[task] = r
		case task != "" && strings.HasPrefix(line, "fallback_models "):
			r := routes[task]
			if m := quoted.FindStringSubmatch(line); m != nil {
				r.fallback = m[1]
			}
			routes[task] = r
		}
	}
	return routes
}

// newestHy4Policy returns the version and quote-policy delta of the newest migration that
// proposes a Hy4-routing quote policy.
func newestHy4Policy(t *testing.T, dir string) (string, QuotePolicy) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	insert := regexp.MustCompile(`(?s)INSERT INTO pricing_quote_policies\(version, policy\)\s*SELECT '(usd-hy4-routing[^']*)', policy \|\| \$policy\$(.*?)\$policy\$`)
	for i := len(files) - 1; i >= 0; i-- {
		raw, err := os.ReadFile(files[i])
		if err != nil {
			t.Fatal(err)
		}
		m := insert.FindSubmatch(raw)
		if m == nil {
			continue
		}
		var policy QuotePolicy
		if err := json.Unmarshal(m[2], &policy); err != nil {
			t.Fatalf("%s: %v", files[i], err)
		}
		return string(m[1]), policy
	}
	t.Fatal("no usd-hy4-routing quote policy migration found")
	return "", QuotePolicy{}
}

func TestHy4RoutingPolicyCoversEngineRoutes(t *testing.T) {
	version, policy := newestHy4Policy(t, "../../migrations")
	routes := engineRoutes(t, "../../../../engine/config/models.toml")
	for action, task := range quotedActionTasks {
		route, ok := routes[task]
		if !ok {
			t.Errorf("engine/config/models.toml has no [tasks.%s] for quoted action %s", task, action)
			continue
		}
		key, ok := policy.ActionModels[action]
		if !ok {
			t.Errorf("%s quotes no model for %s; engine serves %s -> %s", version, action, route.model, route.fallback)
			continue
		}
		primary := policy.Models[key]
		fallback := policy.Models[policy.FallbackModels[key]]
		if primary.Provider != "openrouter" || primary.Model != route.model || fallback.Model != route.fallback {
			t.Errorf("%s: %s quotes %s/%s -> %s, engine task %s serves openrouter/%s -> %s",
				version, action, primary.Provider, primary.Model, fallback.Model, task, route.model, route.fallback)
		}
	}
}

func TestEngineRoutesParser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.toml")
	if err := os.WriteFile(path, []byte("[reasoning]\n\"x\" = \"low\"\n\n[tasks.preview]\nmodel = \"a/b\"\nfallback_models = [\"c/d\", \"e/f\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := engineRoutes(t, path)
	if len(got) != 1 || got["preview"] != (engineRoute{"a/b", "c/d"}) {
		t.Fatalf("parsed %v", got)
	}
}
