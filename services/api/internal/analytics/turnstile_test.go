package analytics

import (
	"reflect"
	"testing"
)

func TestTurnstileEventAllowsOnlyClosedProperties(t *testing.T) {
	const event = "turnstile_verification"
	if !allowlisted(event) {
		t.Fatal("Turnstile event missing from allowlist")
	}
	props := map[string]any{
		"source": "api", "platform": "web", "app_version": "0.1.0",
		"surface": "share_preview", "outcome": "rejected", "reason_code": "mismatch",
		"ip": "192.0.2.2", "email": "private@example.test", "code": "123456",
		"token": "proof", "share_token": "share-fixture", "error": "raw vendor diagnostic",
	}
	want := map[string]any{
		"source": "api", "platform": "web", "app_version": "0.1.0",
		"surface": "share_preview", "outcome": "rejected", "reason_code": "mismatch",
	}
	if got := safeProperties(event, props); !reflect.DeepEqual(got, want) {
		t.Fatalf("event property schema mismatch: %#v", got)
	}
	props["surface"], props["outcome"], props["reason_code"] = " email_code", "403", "proof"
	if got := safeProperties(event, props); len(got) != 3 {
		t.Fatalf("accepted non-enum event properties: %#v", got)
	}
}
