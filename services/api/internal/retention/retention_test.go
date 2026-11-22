package retention

import (
	"testing"
	"time"
)

func TestConfigDefaults(t *testing.T) {
	cfg := ConfigFromEnv(func(string) string { return "" }, 7*24*time.Hour)
	if cfg.PlaybackSyncEventWindow != 30*24*time.Hour || cfg.AnswerOperationWindow != 7*24*time.Hour || cfg.SweepInterval != 24*time.Hour || cfg.BatchSize != 5000 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestConfigParsesDaysAndDurationsAndRejectsGarbage(t *testing.T) {
	env := map[string]string{
		"PLAYBACK_SYNC_EVENT_RETENTION":          "45d",
		"LESSON_TEST_ANSWER_OPERATION_RETENTION": "240h",
		"ROW_RETENTION_SWEEP_INTERVAL":           "-1h",
		"ROW_RETENTION_BATCH_SIZE":               "100",
	}
	cfg := ConfigFromEnv(func(k string) string { return env[k] }, 7*24*time.Hour)
	if cfg.PlaybackSyncEventWindow != 45*24*time.Hour || cfg.AnswerOperationWindow != 240*time.Hour || cfg.SweepInterval != 24*time.Hour || cfg.BatchSize != 100 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	env["ROW_RETENTION_BATCH_SIZE"] = "abc"
	env["PLAYBACK_SYNC_EVENT_RETENTION"] = "0d"
	cfg = ConfigFromEnv(func(k string) string { return env[k] }, 7*24*time.Hour)
	if cfg.BatchSize != 5000 || cfg.PlaybackSyncEventWindow != 30*24*time.Hour {
		t.Fatalf("invalid values must fall back: %+v", cfg)
	}
}

func TestAnswerOperationWindowNeverBelowSessionTTL(t *testing.T) {
	env := map[string]string{"LESSON_TEST_ANSWER_OPERATION_RETENTION": "24h"}
	cfg := ConfigFromEnv(func(k string) string { return env[k] }, 7*24*time.Hour)
	if cfg.AnswerOperationWindow != 7*24*time.Hour {
		t.Fatalf("window %v must be raised to the session TTL", cfg.AnswerOperationWindow)
	}
}
