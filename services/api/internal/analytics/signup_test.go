package analytics

import "testing"

func TestSignupPropertiesAreClosed(t *testing.T) {
	if !allowlisted("learner_signed_up") {
		t.Fatal("signup event is not registered")
	}
	props := safeProperties("learner_signed_up", map[string]any{
		"source": "api", "platform": "web", "app_version": "development",
		"method": "email", "email": "private@example.test", "name": "private",
	})
	if props["method"] != "email" || props["source"] != "api" || len(props) != 4 {
		t.Fatalf("unexpected properties: %#v", props)
	}
	if safeProperties("learner_signed_up", map[string]any{"method": "private text"})["method"] != nil {
		t.Fatal("accepted unregistered method")
	}
}

func TestQuoteAcceptanceDropsPrivateInputs(t *testing.T) {
	if !allowlisted("action_quote_accepted") {
		t.Fatal("quote acceptance event is not registered")
	}
	props := safeProperties("action_quote_accepted", map[string]any{
		"source": "api", "platform": "web", "app_version": "development",
		"quote_id": "11111111-1111-4111-8111-111111111111", "feature": "lesson_content", "pricing_version": "v1",
		"prompt": "private text", "input": map[string]any{"content": "private"},
	})
	if len(props) != 6 || props["feature"] != "lesson_content" {
		t.Fatalf("unexpected properties: %#v", props)
	}
}

func TestPricingEventsDoNotDuplicateLearnerIdentity(t *testing.T) {
	props := safeProperties("pricing_version_created", map[string]any{
		"source": "api", "platform": "web", "app_version": "development",
		"pricing_version": "v1", "change_class": "non_adverse",
		"actor_id": "11111111-1111-4111-8111-111111111111",
	})
	if props["actor_id"] != nil {
		t.Fatalf("learner identity appeared as a property: %#v", props)
	}
}
