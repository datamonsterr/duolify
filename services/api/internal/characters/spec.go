package characters

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var (
	hexColorRegex    = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
	validBodyShapes  = map[string]bool{"round": true, "bean": true, "star": true}
	validEyes        = map[string]bool{"round": true, "oval": true, "sleepy": true}
	validMouths      = map[string]bool{"smile": true, "line": true, "open": true}
	validAccessories = map[string]bool{"glasses": true, "scarf": true, "hat": true, "badge": true}
	validExpressions = []string{"idle", "happy", "sad", "thinking", "celebrate"}
	validVoiceKeys   = map[string]bool{
		"voice-provisional-guide":     true,
		"voice-provisional-supporter": true,
		"voice-provisional-companion": true,
	}
)

// CharacterInfo contains structured descriptive metadata for a character.
type CharacterInfo struct {
	Name      string `json:"name"`
	Bio       string `json:"bio"`
	Backstory string `json:"backstory"`
}

// CharacterPersonality defines traits, speaking style, and the persona prompt.
type CharacterPersonality struct {
	Traits        []string `json:"traits"`
	SpeakingStyle string   `json:"speaking_style"`
	PersonaPrompt string   `json:"persona_prompt"`
}

// Palette specifies the body, accent, and outline hex colors.
type Palette struct {
	Body    string `json:"body"`
	Accent  string `json:"accent"`
	Outline string `json:"outline"`
}

// CharacterAppearance specifies the parametric SVG rig configuration.
type CharacterAppearance struct {
	BodyShape   string   `json:"body_shape"`
	Palette     Palette  `json:"palette"`
	Eyes        string   `json:"eyes"`
	Mouth       string   `json:"mouth"`
	Accessories []string `json:"accessories"`
}

// CharacterSpec is the closed v1 schema representing a full character.
type CharacterSpec struct {
	SchemaVersion int                  `json:"schema_version"`
	RigVersion    string               `json:"rig_version"`
	Info          CharacterInfo        `json:"info"`
	Personality   CharacterPersonality `json:"personality"`
	Appearance    CharacterAppearance  `json:"appearance"`
	Expressions   []string             `json:"expressions"`
	VoiceKey      string               `json:"voice_key"`
}

// ValidateSpecJSON decodes raw bytes into CharacterSpec, enforcing a closed schema.
func ValidateSpecJSON(raw []byte) (*CharacterSpec, error) {
	if len(raw) > 8192 {
		return nil, errors.New("CharacterSpec exceeds maximum size of 8 KiB")
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	var spec CharacterSpec
	if err := decoder.Decode(&spec); err != nil {
		return nil, fmt.Errorf("invalid character spec schema: %w", err)
	}

	if err := ValidateSpec(&spec); err != nil {
		return nil, err
	}

	return &spec, nil
}

// ValidateSpec checks cross-field constraints, bounds, contrast, and safety.
func ValidateSpec(spec *CharacterSpec) error {
	if spec == nil {
		return errors.New("spec is nil")
	}

	if spec.SchemaVersion != 1 {
		return fmt.Errorf("unsupported schema_version: %d", spec.SchemaVersion)
	}

	if spec.RigVersion != "original-v1" {
		return fmt.Errorf("unsupported rig_version: %s", spec.RigVersion)
	}

	// Validate Info
	nameLen := len([]rune(spec.Info.Name))
	if nameLen < 1 || nameLen > 40 {
		return errors.New("name must be 1-40 characters")
	}
	bioLen := len([]rune(spec.Info.Bio))
	if bioLen > 280 {
		return errors.New("bio must be 0-280 characters")
	}
	backstoryLen := len([]rune(spec.Info.Backstory))
	if backstoryLen > 800 {
		return errors.New("backstory must be 0-800 characters")
	}

	// Validate Personality
	traitCount := len(spec.Personality.Traits)
	if traitCount < 1 || traitCount > 5 {
		return errors.New("traits must contain 1-5 items")
	}
	traitSet := make(map[string]bool)
	traitTotalLen := 0
	for _, trait := range spec.Personality.Traits {
		tLen := len([]rune(trait))
		if tLen < 1 || tLen > 40 {
			return errors.New("each trait must be 1-40 characters")
		}
		if traitSet[trait] {
			return errors.New("duplicate trait")
		}
		traitSet[trait] = true
		traitTotalLen += tLen
	}

	styleLen := len([]rune(spec.Personality.SpeakingStyle))
	if styleLen < 1 || styleLen > 200 {
		return errors.New("speaking_style must be 1-200 characters")
	}
	promptLen := len([]rune(spec.Personality.PersonaPrompt))
	if promptLen < 1 || promptLen > 500 {
		return errors.New("persona_prompt must be 1-500 characters")
	}

	// Cross-field text length check: maximum combined persona/info text 2,400 characters
	totalText := nameLen + bioLen + backstoryLen + traitTotalLen + styleLen + promptLen
	if totalText > 2400 {
		return errors.New("combined info and persona text exceeds 2400 characters")
	}

	// Check for unsafe strings, markup, URLs, control chars
	allStrings := []string{
		spec.Info.Name, spec.Info.Bio, spec.Info.Backstory,
		spec.Personality.SpeakingStyle, spec.Personality.PersonaPrompt,
	}
	allStrings = append(allStrings, spec.Personality.Traits...)
	for _, s := range allStrings {
		if err := validateSafeText(s); err != nil {
			return err
		}
	}

	// Validate Appearance
	if !validBodyShapes[spec.Appearance.BodyShape] {
		return fmt.Errorf("invalid body_shape: %s", spec.Appearance.BodyShape)
	}

	if !hexColorRegex.MatchString(spec.Appearance.Palette.Body) ||
		!hexColorRegex.MatchString(spec.Appearance.Palette.Accent) ||
		!hexColorRegex.MatchString(spec.Appearance.Palette.Outline) {
		return errors.New("palette colors must be valid hex #RRGGBB")
	}

	// Check palette contrast
	if err := checkContrast(spec.Appearance.Palette); err != nil {
		return err
	}

	if !validEyes[spec.Appearance.Eyes] {
		return fmt.Errorf("invalid eyes: %s", spec.Appearance.Eyes)
	}

	if !validMouths[spec.Appearance.Mouth] {
		return fmt.Errorf("invalid mouth: %s", spec.Appearance.Mouth)
	}

	if len(spec.Appearance.Accessories) > 2 {
		return errors.New("accessories must be 0-2 items")
	}
	accSet := make(map[string]bool)
	for _, acc := range spec.Appearance.Accessories {
		if !validAccessories[acc] {
			return fmt.Errorf("invalid accessory: %s", acc)
		}
		if accSet[acc] {
			return errors.New("duplicate accessory")
		}
		accSet[acc] = true
	}

	// Validate Expressions: exactly the 5 unique values
	if len(spec.Expressions) != 5 {
		return errors.New("expressions must contain exactly 5 items")
	}
	expSet := make(map[string]bool)
	for _, exp := range spec.Expressions {
		expSet[exp] = true
	}
	for _, exp := range validExpressions {
		if !expSet[exp] {
			return fmt.Errorf("missing required expression: %s", exp)
		}
	}

	// Validate VoiceKey
	if !validVoiceKeys[spec.VoiceKey] {
		return fmt.Errorf("unresolvable or unapproved voice_key: %s", spec.VoiceKey)
	}

	return nil
}

func validateSafeText(s string) error {
	lower := strings.ToLower(s)
	if strings.Contains(lower, "<") || strings.Contains(lower, ">") {
		return errors.New("HTML/SVG markup is forbidden in text fields")
	}
	if strings.Contains(lower, "javascript:") || strings.Contains(lower, "data:") {
		return errors.New("script/data URLs are forbidden")
	}
	if strings.Contains(lower, "http://") || strings.Contains(lower, "https://") {
		return errors.New("external URLs are forbidden")
	}
	for _, r := range s {
		if r < 32 && r != '\t' && r != '\n' && r != '\r' {
			return errors.New("control characters are forbidden")
		}
	}
	return nil
}

func checkContrast(p Palette) error {
	if strings.EqualFold(p.Body, p.Outline) || strings.EqualFold(p.Body, p.Accent) || strings.EqualFold(p.Outline, p.Accent) {
		return errors.New("palette colors must not be identical")
	}

	lumBody := relativeLuminance(p.Body)
	lumOutline := relativeLuminance(p.Outline)

	contrast := contrastRatio(lumBody, lumOutline)
	if contrast < 2.5 {
		return fmt.Errorf("low contrast between body and outline: ratio %.2f < 2.5", contrast)
	}

	return nil
}

func relativeLuminance(hexColor string) float64 {
	clean := strings.TrimPrefix(hexColor, "#")
	if len(clean) != 6 {
		return 0
	}
	bytes, _ := hex.DecodeString(clean)
	r := channelLuminance(float64(bytes[0]) / 255.0)
	g := channelLuminance(float64(bytes[1]) / 255.0)
	b := channelLuminance(float64(bytes[2]) / 255.0)
	return 0.2126*r + 0.7152*g + 0.0722*b
}

func channelLuminance(val float64) float64 {
	if val <= 0.03928 {
		return val / 12.92
	}
	return math.Pow((val+0.055)/1.055, 2.4)
}

func contrastRatio(l1, l2 float64) float64 {
	lighter := math.Max(l1, l2)
	darker := math.Min(l1, l2)
	return (lighter + 0.05) / (darker + 0.05)
}

// HexToRGB parses a hex color string into RGB integers.
func HexToRGB(hexColor string) (int, int, int) {
	clean := strings.TrimPrefix(hexColor, "#")
	if len(clean) != 6 {
		return 0, 0, 0
	}
	r, _ := strconv.ParseInt(clean[0:2], 16, 64)
	g, _ := strconv.ParseInt(clean[2:4], 16, 64)
	b, _ := strconv.ParseInt(clean[4:6], 16, 64)
	return int(r), int(g), int(b)
}
