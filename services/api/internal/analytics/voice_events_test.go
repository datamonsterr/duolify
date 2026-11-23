package analytics

import "testing"

func TestVoiceTranscriptionEventsKeepOnlyClosedValues(t *testing.T) {
	for _, e := range []string{"voice_transcription_succeeded", "voice_transcription_failed", "transcription_requested", "transcription_succeeded", "transcription_failed", "transcription_fallback_used"} {
		if !allowlisted(e) {
			t.Errorf("%s is not allowlisted", e)
		}
	}
	got := safeProperties("voice_transcription_succeeded", map[string]any{
		"source": "api", "platform": "ios", "app_version": "1.0.0",
		"purpose": "note", "duration_bucket": "5_15s", "language": "vi", "served_model": "qwen/qwen3-asr-0.6b", "fallback_used": true, "empty": false,
		"text": "what is a closure", "transcript": "x", "audio": []byte{1}, "error_class": "free text from a provider",
	})
	want := map[string]any{"source": "api", "platform": "ios", "app_version": "1.0.0", "purpose": "note", "duration_bucket": "5_15s", "language": "vi", "served_model": "qwen/qwen3-asr-0.6b", "fallback_used": true, "empty": false}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	for _, k := range []string{"text", "transcript", "audio", "error_class"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s survived sanitising", k)
		}
	}
	bad := safeProperties("voice_transcription_failed", map[string]any{"source": "api", "platform": "web", "app_version": "1", "purpose": "dictation", "duration_bucket": "9_hours", "language": "Vietnamese, probably", "error_class": "voice_timeout"})
	if bad["purpose"] != nil || bad["duration_bucket"] != nil || bad["language"] != nil || bad["error_class"] != "voice_timeout" {
		t.Errorf("unexpected %v", bad)
	}
}
