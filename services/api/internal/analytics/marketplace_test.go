package analytics

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMarketplaceEventsAreCataloguedAndCarryNoQueryText(t *testing.T) {
	for _, name := range []string{"marketplace_browsed", "marketplace_searched", "course_viewed"} {
		if !allowlisted(name) {
			t.Fatalf("%s missing from the catalogue", name)
		}
	}
	id := "a1111111-1111-4111-8111-111111111111"
	common := map[string]any{"source": "api", "platform": "web", "app_version": "0.1.0"}
	with := func(extra map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range common {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	encode := func(event string, in map[string]any) string {
		raw, _ := json.Marshal(safeProperties(event, in))
		return string(raw)
	}
	searched := encode("marketplace_searched", with(map[string]any{
		"query_length": 14, "q": "rust ownership", "query": "rust ownership", "text": "rust ownership",
		"category": "tech", "language": "en", "sort": "rating", "result_count": 3,
		"title": "Rust ownership basics", "description": "Borrowing",
	}))
	for _, banned := range []string{"rust ownership", "Rust ownership basics", "Borrowing", `"q"`, `"query"`, `"text"`, `"title"`} {
		if strings.Contains(searched, banned) {
			t.Errorf("marketplace_searched leaked %q: %s", banned, searched)
		}
	}
	for _, want := range []string{`"query_length":14`, `"category":"tech"`, `"language":"en"`, `"sort":"rating"`, `"result_count":3`} {
		if !strings.Contains(searched, want) {
			t.Errorf("marketplace_searched missing %s: %s", want, searched)
		}
	}
	// Free-text smuggled through the enum fields is dropped.
	dirty := encode("marketplace_browsed", with(map[string]any{
		"category": "tech; drop table", "language": "english please", "sort": "<script>", "result_count": 2, "has_cursor": true,
	}))
	for _, banned := range []string{"drop table", "english please", "script"} {
		if strings.Contains(dirty, banned) {
			t.Errorf("marketplace_browsed kept free text %q: %s", banned, dirty)
		}
	}
	if !strings.Contains(dirty, `"has_cursor":true`) || !strings.Contains(dirty, `"result_count":2`) {
		t.Errorf("marketplace_browsed lost its closed fields: %s", dirty)
	}
	viewed := encode("course_viewed", with(map[string]any{"course_id": id, "version": 3, "surface": "marketplace", "title": "Distributed systems", "email": "a@b.c"}))
	for _, banned := range []string{"Distributed", "a@b.c", `"email"`, `"title"`} {
		if strings.Contains(viewed, banned) {
			t.Errorf("course_viewed leaked %q: %s", banned, viewed)
		}
	}
	for _, want := range []string{id, `"version":3`, `"surface":"marketplace"`} {
		if !strings.Contains(viewed, want) {
			t.Errorf("course_viewed missing %s: %s", want, viewed)
		}
	}
	if strings.Contains(encode("course_viewed", with(map[string]any{"course_id": "not-a-uuid"})), "not-a-uuid") {
		t.Error("course_viewed kept a non-UUID course id")
	}
}
