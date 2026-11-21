package pricing

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

func TestParseConcurrentValidation(t *testing.T) {
	valid := sampleValidConfig()
	valid["design_chat_policy"] = ownerApprovedPolicy()
	raw, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	valid["design_chat_policy"].(map[string]string)["setup_chat"] = "charge_on_apply"
	invalid, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 16 {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			for range 4 {
				cfg, err := Parse(raw)
				if err != nil {
					t.Fatal(err)
				}
				if cfg.DesignChatPolicy["setup_chat"] != "platform_paid" {
					t.Fatal("valid policy changed between parses")
				}
				cfg.DesignChatPolicy["setup_chat"] = "mutated by caller"
				if _, err := Parse(invalid); err == nil {
					t.Fatal("invalid policy accepted during concurrent validation")
				}
			}
		})
	}
}

func BenchmarkParseOwnerApprovedCatalogue(b *testing.B) {
	raw, err := os.ReadFile("fixtures/usd_design_chat_owner_approved.json")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Parse(raw); err != nil {
			b.Fatal(err)
		}
	}
}
