// Package narration synthesizes, stores and tracks per-slide spoken narration.
package narration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
)

// Statuses written to slide_narrations and projected into the manifest.
const (
	StatusPending  = "pending"
	StatusReady    = "ready"
	StatusFailed   = "failed"
	StatusDisabled = "disabled"
)

const (
	// VoiceKey is the logical narration voice (ADR 0011 E12-02); the engine resolves it.
	VoiceKey = "default"
	// MaxAudioBytes keeps objects inside the player's bounded object reads.
	MaxAudioBytes = 1 << 20
	// toleranceMS is how far the alignment end may drift from the audio duration.
	toleranceMS = 250
)

var (
	// ErrMissingText rejects a Slide without narration text.
	ErrMissingText = errors.New("narration text missing")
	// ErrAlignment rejects alignment that does not cover the text.
	ErrAlignment = errors.New("narration alignment invalid")
	// ErrOutput rejects incomplete synthesis results.
	ErrOutput = errors.New("narration output invalid")
)

// ContentHash names shared objects: identical text, voice, model and settings are never paid twice.
func ContentHash(text, voiceID, modelID, fingerprint string) string {
	h := sha256.New()
	for _, part := range []string{text, voiceID, modelID, fingerprint} {
		_, _ = fmt.Fprintf(h, "%d:%s|", len(part), part)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Keys returns the versioned audio and alignment object keys for a content hash.
func Keys(hash string) (audio, alignment string) {
	base := "lessons/narration/v1/" + hash
	return base + "/audio.wav", base + "/alignment.json"
}

// ValidateOutput rejects output the player could not honour: missing text or audio,
// gaps, reversals, mismatched counts or alignment far from the duration.
func ValidateOutput(text string, r *enginev1.SynthesizeNarrationResponse) error {
	if strings.TrimSpace(text) == "" {
		return ErrMissingText
	}
	if r == nil || len(r.Audio) == 0 || len(r.Audio) > MaxAudioBytes || r.DurationMs == 0 || r.SettingsFingerprint == "" || r.VoiceId == "" || r.ModelId == "" {
		return ErrOutput
	}
	runes := []rune(text)
	if int(r.Characters) != len(runes) || int(r.AudioBytes) != len(r.Audio) {
		return ErrOutput
	}
	a := r.Alignment
	if a == nil || len(a.Characters) != len(runes) || len(a.StartMs) != len(runes) || len(a.EndMs) != len(runes) {
		return ErrAlignment
	}
	var previous uint32
	for i, want := range runes {
		if a.Characters[i] != string(want) || a.EndMs[i] < a.StartMs[i] || a.StartMs[i] < previous {
			return ErrAlignment
		}
		previous = a.StartMs[i]
	}
	last := a.EndMs[len(runes)-1]
	if diff := int64(last) - int64(r.DurationMs); diff > toleranceMS || diff < -toleranceMS {
		return ErrAlignment
	}
	return nil
}

// Alignment is the stored and served alignment document.
type Alignment struct {
	Version    int      `json:"version"`
	DurationMS int      `json:"duration_ms"`
	Characters []string `json:"characters"`
	StartMS    []uint32 `json:"start_ms"`
	EndMS      []uint32 `json:"end_ms"`
}

func alignmentJSON(r *enginev1.SynthesizeNarrationResponse) ([]byte, error) {
	return json.Marshal(Alignment{Version: 1, DurationMS: int(r.DurationMs), Characters: r.Alignment.Characters, StartMS: r.Alignment.StartMs, EndMS: r.Alignment.EndMs})
}

// ValidText bounds narration text before it is stored or sent to the engine.
func ValidText(text string) error {
	if strings.TrimSpace(text) == "" {
		return ErrMissingText
	}
	if !utf8.ValidString(text) || strings.ContainsAny(text, "<>") {
		return ErrOutput
	}
	return nil
}
