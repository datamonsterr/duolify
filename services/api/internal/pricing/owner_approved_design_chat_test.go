package pricing

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"testing"
)

func ownerApprovedPolicy() map[string]string {
	return map[string]string{
		"setup_chat":                      "platform_paid",
		"course_brief_chat":               "platform_paid",
		"outline_chat_change":             "valid_delivered_proposal_even_if_discarded",
		"undelivered_proposal":            "release_reservation",
		"manual_edits_apply_discard_undo": "free",
	}
}

func TestOwnerApprovedDesignChatFixturePolicyAndHistory(t *testing.T) {
	raw, err := os.ReadFile("fixtures/usd_design_chat_owner_approved.json")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != "usd-design-chat-owner-approved-2026-11-16" || cfg.DesignChatDecision != "owner_approved" {
		t.Fatalf("incorrect owner decision: %s %s", cfg.Version, cfg.DesignChatDecision)
	}
	for action, want := range map[string]int{"setup_chat": 0, "course_brief_chat": 0, "outline_chat_change": 5, "outline_replan": 55} {
		if got, ok := cfg.Actions[action]; !ok || got.Credits != want {
			t.Fatalf("%s: got %v present=%v want=%d", action, got, ok, want)
		}
	}
	if !slices.Contains(cfg.DailyFree.EligibleActions, "outline_chat_change") || slices.Contains(cfg.DailyFree.EligibleActions, "outline_replan") || !cfg.DailyFree.SpendFirst {
		t.Fatalf("incorrect Daily free policy: %+v", cfg.DailyFree)
	}
	var original, roundTrip map[string]any
	if err := json.Unmarshal(raw, &original); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original["design_chat_policy"], roundTrip["design_chat_policy"]) {
		t.Fatal("settlement policy was lost when loading the immutable config")
	}
	policyRaw, _ := json.Marshal(ownerApprovedPolicy())
	var wantPolicy any
	if err := json.Unmarshal(policyRaw, &wantPolicy); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original["design_chat_policy"], wantPolicy) {
		t.Fatalf("incorrect settlement policy: %+v", original["design_chat_policy"])
	}
	interimRaw, err := os.ReadFile("fixtures/usd_interim_design_chat.json")
	if err != nil {
		t.Fatal(err)
	}
	var interim map[string]any
	if err := json.Unmarshal(interimRaw, &interim); err != nil {
		t.Fatal(err)
	}
	if interim["design_chat_decision"] != "interim_adr_0012" {
		t.Fatal("historical interim decision was rewritten")
	}
	for key, value := range interim["limits"].(map[string]any) {
		if !reflect.DeepEqual(value, original["limits"].(map[string]any)[key]) {
			t.Fatalf("interim usage envelope changed: %s", key)
		}
	}
	for _, key := range []string{"packs", "plans", "narration", "paid_credits"} {
		if !reflect.DeepEqual(interim[key], original[key]) {
			t.Fatalf("unrelated catalogue changed: %s", key)
		}
	}
}

func TestOwnerApprovedDesignChatPolicyValidation(t *testing.T) {
	cfg := sampleValidConfig()
	cfg["design_chat_decision"] = "owner_approved"
	cfg["design_chat_policy"] = ownerApprovedPolicy()
	raw, _ := json.Marshal(cfg)
	if _, err := Parse(raw); err != nil {
		t.Fatalf("valid owner-approved policy rejected: %v", err)
	}
	for key := range ownerApprovedPolicy() {
		t.Run(key, func(t *testing.T) {
			for _, missing := range []bool{false, true} {
				policy := ownerApprovedPolicy()
				if missing {
					delete(policy, key)
				} else {
					policy[key] = "charge_on_apply"
				}
				cfg["design_chat_policy"] = policy
				raw, _ := json.Marshal(cfg)
				if _, err := Parse(raw); err == nil {
					t.Fatalf("invalid policy %s accepted (missing=%v)", key, missing)
				}
			}
		})
	}
}

func TestOwnerApprovedDesignChatPolicyIsNonAdverseMetadata(t *testing.T) {
	before := sampleValidConfig()
	after := sampleValidConfig()
	after["design_chat_decision"] = "owner_approved"
	after["design_chat_policy"] = ownerApprovedPolicy()
	base, _ := json.Marshal(before)
	next, _ := json.Marshal(after)
	for _, pair := range [][2][]byte{{base, next}, {next, base}} {
		if adverse, err := classifyAdverse(pair[0], pair[1]); err != nil || adverse {
			t.Fatalf("recording/removing policy metadata is adverse=%v err=%v", adverse, err)
		}
	}
	// Recording the decision cannot hide a real adverse price change.
	after["actions"].(map[string]any)["outline_chat_change"] = map[string]any{"credits": 6}
	next, _ = json.Marshal(after)
	if adverse, err := classifyAdverse(base, next); err != nil || !adverse {
		t.Fatalf("price increase lost adverse protection: adverse=%v err=%v", adverse, err)
	}
}
