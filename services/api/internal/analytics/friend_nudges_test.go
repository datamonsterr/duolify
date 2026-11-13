package analytics

import "testing"

func TestFriendNudgeSentIsCataloguedAndSanitised(t *testing.T) {
	if !allowlisted("friend_nudge_sent") {
		t.Fatal("friend_nudge_sent missing from the catalogue")
	}
	props := safeProperties("friend_nudge_sent", map[string]any{
		"source": "api", "platform": "web", "app_version": "0.1.0",
		"event_id": "22222222-2222-4222-8222-222222222222", "local_date": "2026-11-14",
		"username": "mina", "display_name": "Mina Private", "friend_id": "33333333-3333-4333-8333-333333333333",
		"recipient_id": "44444444-4444-4444-8444-444444444444", "message": "come back and learn!",
	})
	if props["event_id"] != "22222222-2222-4222-8222-222222222222" || props["local_date"] != "2026-11-14" {
		t.Fatal(props)
	}
	for _, banned := range []string{"username", "display_name", "friend_id", "recipient_id", "message"} {
		if _, ok := props[banned]; ok {
			t.Fatalf("friend_nudge_sent leaked %s", banned)
		}
	}
}
