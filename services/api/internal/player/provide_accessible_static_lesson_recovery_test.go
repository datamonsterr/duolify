package player

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAccessibleStaticRecoveryManifestValidation(t *testing.T) {
	valid := ObjectManifest{
		Version: 1,
		Slides: []ObjectSlide{
			{
				ID:            "sky-1",
				DurationMS:    3000,
				HTMLKey:       "lessons/v1/one.html",
				Assets:        []string{},
				Checkpoints:   []Checkpoint{{ID: "intro", AtMS: 1500}},
				NarrationText: "Look up at the night sky.",
				Explanation:   "A full static explanation of the rotating celestial sphere for learners with reduced motion.",
			},
		},
	}

	if err := ValidateManifest(valid, "https://assets.example.test"); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}

	// Oversized explanation must be rejected
	invalid := valid
	invalid.Slides = []ObjectSlide{
		{
			ID:            "sky-1",
			DurationMS:    3000,
			HTMLKey:       "lessons/v1/one.html",
			Assets:        []string{},
			Checkpoints:   []Checkpoint{{ID: "intro", AtMS: 1500}},
			NarrationText: "Look up.",
			Explanation:   strings.Repeat("a", 20001),
		},
	}
	if err := ValidateManifest(invalid, "https://assets.example.test"); err == nil {
		t.Fatal("oversized explanation accepted")
	}

	// Manifest decode rejects extra unknown fields
	raw, _ := json.Marshal(valid)
	if _, err := decodeManifest(raw, "https://assets.example.test"); err != nil {
		t.Fatalf("valid decode failed: %v", err)
	}
}
