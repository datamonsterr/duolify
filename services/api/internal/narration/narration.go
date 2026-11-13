// Package narration synthesizes, stores and tracks per-slide spoken narration.
package narration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"golang.org/x/text/unicode/norm"
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

// Recovery states returned to the player by Retry and Recover.
const (
	// RecoveryOK means every object is present; nothing changed.
	RecoveryOK = "ok"
	// RecoveryRestored means a retained version made the audio playable again at no charge.
	RecoveryRestored = "restored"
	// RecoveryRegenerating means a tracked operator regeneration is pending; the Learner is never charged.
	RecoveryRegenerating = "regenerating"
	// RecoveryPending means the slide was queued again under the same content hash.
	RecoveryPending = "pending"
	// RecoveryFailed means the slide cannot be retried (for example it has no text).
	RecoveryFailed = "failed"
)

// DefaultRetention is how long an unreferenced asset is kept after its last reference ends.
const DefaultRetention = 30 * 24 * time.Hour

// RetentionExpired reports whether an asset unreferenced since `since` may now be deleted.
func RetentionExpired(since, now time.Time, retention time.Duration) bool {
	return !since.IsZero() && now.Sub(since) >= retention
}

// NormalizeText composes Unicode (NFC) and collapses whitespace, so equivalent texts share one
// hash, one provider call and one charge. The provider receives the normalized text.
func NormalizeText(text string) string {
	return strings.Join(strings.Fields(norm.NFC.String(text)), " ")
}

// TextHash links the retained settings versions of one normalized narration text.
func TextHash(normalized string) string {
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

// AggregateCredits prices newly synthesized characters with one rounding up: the sum of
// characters times credits over the character block, ceiled once (never per slide).
func AggregateCredits(characters []int, credits, block int) int {
	total := 0
	for _, c := range characters {
		total += c
	}
	if total <= 0 || block <= 0 {
		return 0
	}
	return (total*credits + block - 1) / block
}

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

// IsValidPlaybackRate checks whether the playback rate is supported by the narration clock.
func IsValidPlaybackRate(rate float64) bool {
	switch rate {
	case 0.75, 1.0, 1.25, 1.5:
		return true
	default:
		return false
	}
}

// ClampPlaybackTime bounds requested seek time between 0 and durationMs.
func ClampPlaybackTime(timeMs, durationMs int) int {
	if timeMs < 0 {
		return 0
	}
	if durationMs < 0 {
		return 0
	}
	if timeMs > durationMs {
		return durationMs
	}
	return timeMs
}

// EffectiveSlideDuration returns the playback duration that holds the final frame through audio completion.
func EffectiveSlideDuration(slideDurationMs, audioDurationMs int) int {
	if audioDurationMs > slideDurationMs {
		return audioDurationMs
	}
	return slideDurationMs
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
