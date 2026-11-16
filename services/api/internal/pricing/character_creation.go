package pricing

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// CheckCharacterCreateDecision requires the recorded owner decision and the
// configured owner's review. The separate explicit mock-only proposal path is
// never an approved active version.
func CheckCharacterCreateDecision(c Config, approver, ownerID string) error {
	if _, ok := c.Actions["character_create"]; !ok {
		return nil
	}
	if c.CharacterCreateDecision != "owner_approved" || ownerID == "" || approver == "" || !strings.EqualFold(approver, ownerID) {
		return errors.New("character creation pricing requires owner approval")
	}
	return nil
}

func validateCharacterCreate(c Config) error {
	if _, ok := c.Actions["character_create"]; !ok {
		return nil
	}
	for _, key := range c.DailyFree.EligibleActions {
		if key == "character_create" {
			return errors.New("character_create is not daily-free eligible")
		}
	}
	var limits map[string]json.RawMessage
	var bounds map[string]int64
	if json.Unmarshal(c.Limits, &limits) != nil || json.Unmarshal(limits["character_create"], &bounds) != nil {
		return errors.New("character_create requires an execution envelope")
	}
	for key, max := range map[string]int64{"count": 10, "day_attempts": 5, "minute_attempts": 2, "prompt_chars": 1500, "output_tokens": 2000, "timeout_ms": 90000, "max_attempts": 2, "screen_input_tokens": 18000, "screen_output_tokens": 200, "screen_timeout_ms": 10000, "screen_max_attempts": 2} {
		if bounds[key] <= 0 || bounds[key] > max {
			return fmt.Errorf("character_create.%s must be between 1 and %d", key, max)
		}
	}
	if bounds["input_tokens"] <= 0 || bounds["max_cost_nanos"] <= 0 {
		return errors.New("character_create requires input and provider-cost bounds")
	}
	return nil
}
