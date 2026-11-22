package analytics

import "testing"

func TestCheckInCatalogueDropsUnapprovedProperties(t *testing.T) {
	if !allowlisted("check_in_credits_granted") {
		t.Fatal("missing check-in event")
	}
	props := safeProperties("check_in_credits_granted", map[string]any{"amount": 400, "first_time": true, "banked_after": 400, "capped": false, "email": "private@example.test", "timezone": "private", "prompt": "private"})
	if props["amount"] == nil || props["banked_after"] == nil || props["first_time"] != true || props["capped"] != false {
		t.Fatalf("properties: %+v", props)
	}
	for _, key := range []string{"email", "timezone", "prompt"} {
		if _, ok := props[key]; ok {
			t.Fatalf("leaked %s", key)
		}
	}
}
