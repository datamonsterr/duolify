package narration

import (
	"testing"
)

func TestPlaybackSpeedsValidation(t *testing.T) {
	validSpeeds := []float64{0.75, 1.0, 1.25, 1.5}
	for _, speed := range validSpeeds {
		if !IsValidPlaybackRate(speed) {
			t.Fatalf("expected speed %f to be valid", speed)
		}
	}

	invalidSpeeds := []float64{0.0, -1.0, 0.5, 2.5, 3.0}
	for _, speed := range invalidSpeeds {
		if IsValidPlaybackRate(speed) {
			t.Fatalf("expected speed %f to be invalid", speed)
		}
	}
}

func TestClampedSeekTime(t *testing.T) {
	duration := 5000
	cases := []struct {
		requested int
		expected  int
	}{
		{-500, 0},
		{0, 0},
		{2500, 2500},
		{5000, 5000},
		{6000, 5000},
	}

	for _, c := range cases {
		got := ClampPlaybackTime(c.requested, duration)
		if got != c.expected {
			t.Fatalf("requested %d with duration %d: got %d, expected %d", c.requested, duration, got, c.expected)
		}
	}
}

func TestHoldFinalFrameDuration(t *testing.T) {
	// Slide duration vs audio duration: effective playback duration holds final frame
	if got := EffectiveSlideDuration(3000, 5000); got != 5000 {
		t.Fatalf("expected 5000ms when audio is longer than slide, got %d", got)
	}
	if got := EffectiveSlideDuration(5000, 3000); got != 5000 {
		t.Fatalf("expected 5000ms when slide is longer than audio, got %d", got)
	}
	if got := EffectiveSlideDuration(4000, 0); got != 4000 {
		t.Fatalf("expected 4000ms when audio is missing or 0, got %d", got)
	}
}
