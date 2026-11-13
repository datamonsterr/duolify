package analytics

import "testing"

func TestFriendStreakEventsAreCataloguedAndSanitised(t *testing.T) {
	input := map[string]any{
		"source": "api", "platform": "web", "app_version": "0.1.0",
		"event_id": "22222222-2222-4222-8222-222222222222", "local_date": "2026-11-14", "streak_length": 3,
		"previous_length": 3, "reason": "friendship_ended",
		"username": "mina", "display_name": "Mina Private", "friend_id": "33333333-3333-4333-8333-333333333333",
	}
	for _, name := range []string{"friend_streak_started", "friend_streak_extended", "friend_streak_broken"} {
		if !allowlisted(name) {
			t.Fatal("missing catalogue", name)
		}
		props := safeProperties(name, input)
		for _, banned := range []string{"username", "display_name", "friend_id"} {
			if _, ok := props[banned]; ok {
				t.Fatalf("%s leaked %s", name, banned)
			}
		}
		if props["event_id"] == nil {
			t.Fatalf("%s lost event_id: %v", name, props)
		}
	}
	if p := safeProperties("friend_streak_broken", input); p["reason"] != "friendship_ended" || p["previous_length"] != 3 {
		t.Fatal(p)
	}
	if p := safeProperties("friend_streak_broken", map[string]any{"reason": "free text with spaces", "previous_length": 2}); p["reason"] != nil {
		t.Fatal("reason must be an enum", p)
	}
	if p := safeProperties("friend_streak_extended", input); p["streak_length"] != 3 || p["local_date"] != "2026-11-14" {
		t.Fatal(p)
	}
	if p := safeProperties("streak_freeze_earned", map[string]any{"event_id": "22222222-2222-4222-8222-222222222222", "reason": "friend_streak", "streak_length": 7, "local_date": "2026-11-14"}); p["reason"] != "friend_streak" {
		t.Fatal(p)
	}
}
