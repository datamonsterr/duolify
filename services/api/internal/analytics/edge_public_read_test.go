package analytics

import (
	"reflect"
	"testing"
)

func TestEdgePublicReadAllowsOnlyClosedProperties(t *testing.T) {
	const event = "edge_public_read"
	if !allowlisted(event) {
		t.Fatal("edge read event must be allowlisted")
	}
	input := map[string]any{ // #nosec G101 -- synthetic redaction fixtures.
		"source": "api", "platform": "web", "app_version": "0.1.0",
		"surface": "share_preview", "read_path": "go_origin", "cache_status": "bypass", "outcome": "served",
		"query": "learner text", "token": "share credential", "ip": "192.0.2.1",
	}
	expected := map[string]any{
		"source": "api", "platform": "web", "app_version": "0.1.0",
		"surface": "share_preview", "read_path": "go_origin", "cache_status": "bypass", "outcome": "served",
	}
	if got := safeProperties(event, input); !reflect.DeepEqual(got, expected) {
		t.Fatalf("properties: got %v, want %v", got, expected)
	}
	for _, key := range []string{"surface", "read_path", "cache_status", "outcome"} {
		input[key] = "learner text"
		if _, exists := safeProperties(event, input)[key]; exists {
			t.Errorf("%q allowed arbitrary text", key)
		}
	}
}
