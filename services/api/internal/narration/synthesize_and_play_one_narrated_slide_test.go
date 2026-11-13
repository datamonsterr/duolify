package narration

import (
	"errors"
	"strings"
	"testing"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/narration/narrationtest"
)

func response(t *testing.T, text string) *enginev1.SynthesizeNarrationResponse {
	t.Helper()
	r, err := narrationtest.Synthesize(&enginev1.SynthesizeNarrationRequest{SlideId: "s1", Text: text, VoiceKey: "default", Limits: &enginev1.UsageLimits{Model: "eleven_flash_v2_5"}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestContentHashDependsOnTextVoiceModelAndSettings(t *testing.T) {
	base := ContentHash("hello", "voice", "model", "fp")
	if base != ContentHash("hello", "voice", "model", "fp") || len(base) != 64 {
		t.Fatal("hash must be stable sha256 hex", base)
	}
	for _, other := range []string{ContentHash("hello!", "voice", "model", "fp"), ContentHash("hello", "voice2", "model", "fp"), ContentHash("hello", "voice", "model2", "fp"), ContentHash("hello", "voice", "model", "fp2"), ContentHash("hel", "lovoice", "model", "fp")} {
		if other == base {
			t.Fatal("hash collision across inputs")
		}
	}
	audio, alignment := Keys(base)
	if audio != "lessons/narration/v1/"+base+"/audio.wav" || alignment != "lessons/narration/v1/"+base+"/alignment.json" {
		t.Fatal(audio, alignment)
	}
}

func TestValidateOutputAcceptsMockAndRejectsBrokenResults(t *testing.T) {
	const text = "The sky is a map."
	if err := ValidateOutput(text, response(t, text)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateOutput("   ", response(t, text)); !errors.Is(err, ErrMissingText) {
		t.Fatal("blank text must be rejected", err)
	}
	for name, mutate := range map[string]func(*enginev1.SynthesizeNarrationResponse){
		"short alignment":  func(r *enginev1.SynthesizeNarrationResponse) { r.Alignment.Characters = r.Alignment.Characters[1:] },
		"mismatched array": func(r *enginev1.SynthesizeNarrationResponse) { r.Alignment.EndMs = r.Alignment.EndMs[1:] },
		"wrong character":  func(r *enginev1.SynthesizeNarrationResponse) { r.Alignment.Characters[2] = "Z" },
		"backwards start":  func(r *enginev1.SynthesizeNarrationResponse) { r.Alignment.StartMs[4], r.Alignment.StartMs[3] = 0, 90 },
		"end before start": func(r *enginev1.SynthesizeNarrationResponse) { r.Alignment.EndMs[1] = r.Alignment.StartMs[1] - 1 },
		"ends after audio": func(r *enginev1.SynthesizeNarrationResponse) {
			r.DurationMs = r.Alignment.EndMs[len(r.Alignment.EndMs)-1] / 2
		},
		"ends before audio": func(r *enginev1.SynthesizeNarrationResponse) {
			r.DurationMs = r.Alignment.EndMs[len(r.Alignment.EndMs)-1] + 5000
		},
		"no alignment":    func(r *enginev1.SynthesizeNarrationResponse) { r.Alignment = nil },
		"no audio":        func(r *enginev1.SynthesizeNarrationResponse) { r.Audio = nil },
		"zero duration":   func(r *enginev1.SynthesizeNarrationResponse) { r.DurationMs = 0 },
		"byte count":      func(r *enginev1.SynthesizeNarrationResponse) { r.AudioBytes++ },
		"character count": func(r *enginev1.SynthesizeNarrationResponse) { r.Characters++ },
		"no fingerprint":  func(r *enginev1.SynthesizeNarrationResponse) { r.SettingsFingerprint = "" },
	} {
		r := response(t, text)
		mutate(r)
		if err := ValidateOutput(text, r); !errors.Is(err, ErrAlignment) && !errors.Is(err, ErrOutput) {
			t.Fatalf("%s: expected rejection, got %v", name, err)
		}
	}
}

func TestMockSynthesisIsDeterministicAndCountsUnicodeCharacters(t *testing.T) {
	a, b := response(t, "Xin chào, thế giới."), response(t, "Xin chào, thế giới.")
	if string(a.Audio) != string(b.Audio) || a.DurationMs != b.DurationMs || a.SettingsFingerprint != b.SettingsFingerprint {
		t.Fatal("mock must be deterministic")
	}
	if int(a.Characters) != len([]rune("Xin chào, thế giới.")) || len(a.Alignment.Characters) != int(a.Characters) {
		t.Fatal("alignment must cover every Unicode character", a.Characters, len(a.Alignment.Characters))
	}
	if !strings.HasPrefix(string(a.Audio[:4]), "RIFF") || a.AudioFormat != "audio/wav" || int(a.AudioBytes) != len(a.Audio) {
		t.Fatal("mock audio must be a WAV container")
	}
	if a.Usage == nil || a.Usage.Component != "audio" || a.Usage.Provider != "elevenlabs" || a.Usage.NarrationCharacters != a.Characters || !a.Usage.Delivered {
		t.Fatal("actual usage must be reported", a.Usage)
	}
}
