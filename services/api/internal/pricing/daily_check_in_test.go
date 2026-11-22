package pricing

import "testing"

func TestCheckInConfiguration(t *testing.T) {
	cfg := sampleValidConfig()
	cfg["check_in"] = map[string]any{"daily_credits": 150, "first_time_bonus": 250, "bank_days": 7}
	got, err := Parse(mustJSON(t, cfg))
	if err != nil {
		t.Fatal(err)
	}
	if got.CheckIn == nil || got.CheckIn.DailyCredits != 150 || got.CheckIn.FirstTimeBonus != 250 || got.CheckIn.BankDays != 7 {
		t.Fatalf("config: %+v", got.CheckIn)
	}
	cfg["check_in"].(map[string]any)["bank_days"] = 0
	if _, err = Parse(mustJSON(t, cfg)); err == nil {
		t.Fatal("zero bank days accepted")
	}
}
