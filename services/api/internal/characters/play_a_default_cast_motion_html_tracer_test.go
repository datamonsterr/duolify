package characters_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/characters"
)

func TestClosedSchemaAndCrossFieldBounds(t *testing.T) {
	valid := characters.DefaultGuideFixture()

	// 1. Valid fixture passes
	raw, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := characters.ValidateSpecJSON(raw)
	if err != nil {
		t.Fatalf("expected valid spec to pass: %v", err)
	}
	if spec == nil {
		t.Fatal("expected non-nil spec")
	}

	// 2. Reject unknown fields (closed schema)
	var asMap map[string]any
	_ = json.Unmarshal(raw, &asMap)
	asMap["unknown_field"] = "malicious"
	rawUnknown, _ := json.Marshal(asMap)
	if _, err := characters.ValidateSpecJSON(rawUnknown); err == nil {
		t.Error("expected rejection on unknown field in CharacterSpec")
	}

	// 3. Reject injected markup
	maliciousCases := []struct {
		name   string
		mutate func(m map[string]any)
	}{
		{
			name: "script in name",
			mutate: func(m map[string]any) {
				info := m["info"].(map[string]any)
				info["name"] = "<script>alert(1)</script>"
			},
		},
		{
			name: "html in bio",
			mutate: func(m map[string]any) {
				info := m["info"].(map[string]any)
				info["bio"] = "<img src=x onerror=alert(1)>"
			},
		},
		{
			name: "javascript url in speaking style",
			mutate: func(m map[string]any) {
				p := m["personality"].(map[string]any)
				p["speaking_style"] = "javascript:alert(1)"
			},
		},
		{
			name: "http url in traits",
			mutate: func(m map[string]any) {
				p := m["personality"].(map[string]any)
				p["traits"] = []string{"https://evil.com/payload"}
			},
		},
		{
			name: "provider ID in voice_key",
			mutate: func(m map[string]any) {
				m["voice_key"] = "elevenlabs:21m00Tcm4TlvDq8ikWAM"
			},
		},
		{
			name: "unsupported schema version",
			mutate: func(m map[string]any) {
				m["schema_version"] = 2
			},
		},
		{
			name: "unsupported rig version",
			mutate: func(m map[string]any) {
				m["rig_version"] = "future-v2"
			},
		},
		{
			name: "invalid body shape",
			mutate: func(m map[string]any) {
				app := m["appearance"].(map[string]any)
				app["body_shape"] = "dragon"
			},
		},
		{
			name: "too many accessories",
			mutate: func(m map[string]any) {
				app := m["appearance"].(map[string]any)
				app["accessories"] = []string{"glasses", "scarf", "hat"}
			},
		},
		{
			name: "duplicate accessories",
			mutate: func(m map[string]any) {
				app := m["appearance"].(map[string]any)
				app["accessories"] = []string{"glasses", "glasses"}
			},
		},
		{
			name: "low contrast palette",
			mutate: func(m map[string]any) {
				app := m["appearance"].(map[string]any)
				app["palette"] = map[string]string{
					"body":    "#111111",
					"outline": "#111111",
					"accent":  "#111111",
				}
			},
		},
	}

	for _, tc := range maliciousCases {
		t.Run(tc.name, func(t *testing.T) {
			m := make(map[string]any)
			_ = json.Unmarshal(raw, &m)
			tc.mutate(m)
			mutRaw, _ := json.Marshal(m)
			if _, err := characters.ValidateSpecJSON(mutRaw); err == nil {
				t.Errorf("case %q: expected validation rejection", tc.name)
			}
		})
	}

	// 4. Oversized content rejection (> 8 KiB)
	hugeBio := strings.Repeat("A", 9000)
	hugeMap := make(map[string]any)
	_ = json.Unmarshal(raw, &hugeMap)
	hugeMap["info"].(map[string]any)["backstory"] = hugeBio
	hugeRaw, _ := json.Marshal(hugeMap)
	if _, err := characters.ValidateSpecJSON(hugeRaw); err == nil {
		t.Error("expected rejection on >8KiB spec")
	}

	// Field bound checks (backstory > 800)
	textLimitMap := make(map[string]any)
	_ = json.Unmarshal(raw, &textLimitMap)
	info := textLimitMap["info"].(map[string]any)
	info["backstory"] = strings.Repeat("A", 801)
	textLimitRaw, _ := json.Marshal(textLimitMap)
	if _, err := characters.ValidateSpecJSON(textLimitRaw); err == nil {
		t.Error("expected rejection when backstory exceeds 800 chars")
	}

	// Combined text limit (max 2400 chars) checked on spec
	specTooLong := characters.DefaultGuideFixture()
	specTooLong.Info.Backstory = strings.Repeat("A", 2401)
	if err := characters.ValidateSpec(&specTooLong); err == nil {
		t.Error("expected rejection when combined text exceeds 2400 chars")
	}
}

func TestTrustedRigCompilationAndExpressionIdentity(t *testing.T) {
	spec := characters.DefaultGuideFixture()
	expressions := []string{"idle", "happy", "sad", "thinking", "celebrate"}

	compiled := make(map[string]string)
	for _, exp := range expressions {
		svg, err := characters.CompileSVG(&spec, exp)
		if err != nil {
			t.Fatalf("failed to compile SVG for expression %s: %v", exp, err)
		}
		if !strings.HasPrefix(svg, "<svg") || !strings.HasSuffix(svg, "</svg>") {
			t.Errorf("compiled expression %s is not valid SVG tag", exp)
		}
		if strings.Contains(svg, "<script") || strings.Contains(svg, "javascript:") || strings.Contains(svg, "onerror") {
			t.Errorf("compiled SVG for %s contains unsafe script markup", exp)
		}
		compiled[exp] = svg
	}

	// Base body shape, palette colors and accessories must remain invariant across all expressions
	idleSVG := compiled["idle"]
	for _, exp := range expressions[1:] {
		svg := compiled[exp]
		// Body color check
		if !strings.Contains(svg, spec.Appearance.Palette.Body) {
			t.Errorf("expression %s missing body color %s", exp, spec.Appearance.Palette.Body)
		}
		if !strings.Contains(svg, spec.Appearance.Palette.Outline) {
			t.Errorf("expression %s missing outline color %s", exp, spec.Appearance.Palette.Outline)
		}
		// Invariance of base body shape element
		if !strings.Contains(svg, `data-part="body-shape"`) {
			t.Errorf("expression %s missing data-part=body-shape", exp)
		}
		// The idle SVG and other SVG must differ in facial features
		if svg == idleSVG {
			t.Errorf("expression %s produced identical SVG to idle", exp)
		}
	}

	// Invalid expression rejected
	if _, err := characters.CompileSVG(&spec, "angry"); err == nil {
		t.Error("expected error for unsupported expression 'angry'")
	}

	// Bundled safe default
	fallbackSpec := characters.BundledFallbackSpec()
	fallbackSVG, err := characters.CompileSVG(&fallbackSpec, "idle")
	if err != nil {
		t.Fatalf("failed to compile fallback SVG: %v", err)
	}
	if len(fallbackSVG) == 0 {
		t.Fatal("empty fallback SVG")
	}
}

func TestDefaultCastContainsThreeOriginalMascots(t *testing.T) {
	cast := characters.GetDefaultCast()
	if len(cast) != 3 {
		t.Fatalf("expected 3 default cast members, got %d", len(cast))
	}

	seenIDs := make(map[string]bool)
	seenShapes := make(map[string]bool)
	for _, char := range cast {
		if char.Info.Name == "" {
			t.Error("empty character name")
		}
		if seenIDs[char.Info.Name] {
			t.Errorf("duplicate character name: %s", char.Info.Name)
		}
		seenIDs[char.Info.Name] = true
		seenShapes[char.Appearance.BodyShape] = true

		// Closed schema check
		raw, err := json.Marshal(char)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := characters.ValidateSpecJSON(raw); err != nil {
			t.Errorf("default cast character %s failed validation: %v", char.Info.Name, err)
		}
	}

	// 3 distinct body shapes (round, bean, star)
	if len(seenShapes) != 3 {
		t.Errorf("expected 3 distinct shapes in default cast, got %d", len(seenShapes))
	}
}
