package characters

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"unicode/utf8"
)

// SpecSchemaV2 is the canonical closed v2 contract, validated identically by Go,
// TypeScript (apps/client/src/features/characters/schema-v2.ts) and the Engine.
//
//go:embed schema/v2.json
var SpecSchemaV2 []byte

// speciesRulesV2 lists, per species, the attribute values that make sense for it.
// The first value of each list is the species default. Shared with TypeScript.
//
//go:embed schema/species.v2.json
var speciesRulesV2 []byte

var speciesProfiles = func() map[string]map[string]map[string][]string {
	m := map[string]map[string]map[string][]string{}
	if err := json.Unmarshal(speciesRulesV2, &m); err != nil {
		panic(err)
	}
	return m
}()

func speciesAllows(kind, species, attr, val string) bool {
	for _, v := range speciesProfiles[kind][species][attr] {
		if v == val {
			return true
		}
	}
	return false
}

// Version constants identify documents written by the character creator.
const (
	SchemaV2       = 2             // schema_version
	RigV2          = "original-v2" // rig_version
	CatalogVersion = 1             // catalog_version
	maxSpecBytes   = 8192
	maxTextRunes   = 2400
)

var validExpressionsV2 = []string{"idle", "happy", "sad", "thinking", "celebrate", "surprised", "confused"}

// Styled is a catalog style id with its colour.
type Styled struct {
	Style string `json:"style"`
	Color string `json:"color"`
}

// Eyes is a kind-specific eye shape and its colour.
type Eyes struct {
	Shape string `json:"shape"`
	Color string `json:"color"`
}

// Hair describes a human hairstyle.
type Hair struct {
	Texture string `json:"texture"`
	Length  string `json:"length"`
	Style   string `json:"style"`
	Color   string `json:"color"`
}

// Accessory is one worn accessory.
type Accessory struct {
	Kind  string `json:"kind"`
	Color string `json:"color"`
}

// Identity is free, optional text and is never inferred or used for appearance.
type Identity struct {
	Pronouns string `json:"pronouns"`
	Label    string `json:"label"`
}

// Clothes holds garments, accessories and the shared pattern.
type Clothes struct {
	Top         Styled      `json:"top"`
	Bottom      Styled      `json:"bottom"`
	Shoes       Styled      `json:"shoes"`
	Outerwear   Styled      `json:"outerwear"`
	Pattern     string      `json:"pattern"`
	Accessories []Accessory `json:"accessories"`
}

// Body is the discriminated union over Kind. Only the fields of the active kind
// are encoded; the others stay zero.
type Body struct {
	Kind    string
	AgeBand string
	Build   string
	Height  string
	Glasses Styled
	Mouth   string
	Eyes    Eyes

	// human
	SkinTone     string
	FaceShape    string
	Hair         Hair
	HeadCovering Styled
	Eyebrows     Styled
	Nose         string
	FacialHair   Styled
	SkinFeatures []string

	// animal, robot, fantasy
	Species        string
	PrimaryColor   string
	SecondaryColor string
	Markings       string
	Ears           string
	Tail           string
	Base           string
	Antenna        string
	Locomotion     string
	Wings          string
	Horns          string
}

type bodyCommon struct {
	Kind    string `json:"kind"`
	AgeBand string `json:"age_band"`
	Build   string `json:"build"`
	Height  string `json:"height"`
	Glasses Styled `json:"glasses"`
}

// MarshalJSON emits exactly the fields the schema requires for the kind.
func (b Body) MarshalJSON() ([]byte, error) {
	c := bodyCommon{b.Kind, b.AgeBand, b.Build, b.Height, b.Glasses}
	switch b.Kind {
	case "human":
		features := b.SkinFeatures
		if features == nil {
			features = []string{}
		}
		return json.Marshal(struct {
			bodyCommon
			SkinTone     string   `json:"skin_tone"`
			FaceShape    string   `json:"face_shape"`
			Hair         Hair     `json:"hair"`
			HeadCovering Styled   `json:"head_covering"`
			Eyes         Eyes     `json:"eyes"`
			Eyebrows     Styled   `json:"eyebrows"`
			Nose         string   `json:"nose"`
			Mouth        string   `json:"mouth"`
			FacialHair   Styled   `json:"facial_hair"`
			SkinFeatures []string `json:"skin_features"`
		}{c, b.SkinTone, b.FaceShape, b.Hair, b.HeadCovering, b.Eyes, b.Eyebrows, b.Nose, b.Mouth, b.FacialHair, features})
	case "animal":
		return json.Marshal(struct {
			bodyCommon
			Species        string `json:"species"`
			PrimaryColor   string `json:"primary_color"`
			SecondaryColor string `json:"secondary_color"`
			Markings       string `json:"markings"`
			Ears           string `json:"ears"`
			Tail           string `json:"tail"`
			Eyes           Eyes   `json:"eyes"`
			Mouth          string `json:"mouth"`
		}{c, b.Species, b.PrimaryColor, b.SecondaryColor, b.Markings, b.Ears, b.Tail, b.Eyes, b.Mouth})
	case "robot":
		return json.Marshal(struct {
			bodyCommon
			Base           string `json:"base"`
			PrimaryColor   string `json:"primary_color"`
			SecondaryColor string `json:"secondary_color"`
			Markings       string `json:"markings"`
			Antenna        string `json:"antenna"`
			Locomotion     string `json:"locomotion"`
			Eyes           Eyes   `json:"eyes"`
			Mouth          string `json:"mouth"`
		}{c, b.Base, b.PrimaryColor, b.SecondaryColor, b.Markings, b.Antenna, b.Locomotion, b.Eyes, b.Mouth})
	case "fantasy":
		return json.Marshal(struct {
			bodyCommon
			Species        string `json:"species"`
			PrimaryColor   string `json:"primary_color"`
			SecondaryColor string `json:"secondary_color"`
			Wings          string `json:"wings"`
			Horns          string `json:"horns"`
			Tail           string `json:"tail"`
			Eyes           Eyes   `json:"eyes"`
			Mouth          string `json:"mouth"`
		}{c, b.Species, b.PrimaryColor, b.SecondaryColor, b.Wings, b.Horns, b.Tail, b.Eyes, b.Mouth})
	}
	return nil, fmt.Errorf("unknown body kind %q", b.Kind)
}

// UnmarshalJSON decodes any kind; unknown keys are rejected.
func (b *Body) UnmarshalJSON(raw []byte) error {
	var in struct {
		bodyCommon
		SkinTone       string   `json:"skin_tone"`
		FaceShape      string   `json:"face_shape"`
		Hair           Hair     `json:"hair"`
		HeadCovering   Styled   `json:"head_covering"`
		Eyes           Eyes     `json:"eyes"`
		Eyebrows       Styled   `json:"eyebrows"`
		Nose           string   `json:"nose"`
		Mouth          string   `json:"mouth"`
		FacialHair     Styled   `json:"facial_hair"`
		SkinFeatures   []string `json:"skin_features"`
		Species        string   `json:"species"`
		PrimaryColor   string   `json:"primary_color"`
		SecondaryColor string   `json:"secondary_color"`
		Markings       string   `json:"markings"`
		Ears           string   `json:"ears"`
		Tail           string   `json:"tail"`
		Base           string   `json:"base"`
		Antenna        string   `json:"antenna"`
		Locomotion     string   `json:"locomotion"`
		Wings          string   `json:"wings"`
		Horns          string   `json:"horns"`
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil {
		return err
	}
	*b = Body{in.Kind, in.AgeBand, in.Build, in.Height, in.Glasses, in.Mouth, in.Eyes,
		in.SkinTone, in.FaceShape, in.Hair, in.HeadCovering, in.Eyebrows, in.Nose, in.FacialHair, in.SkinFeatures,
		in.Species, in.PrimaryColor, in.SecondaryColor, in.Markings, in.Ears, in.Tail, in.Base, in.Antenna, in.Locomotion, in.Wings, in.Horns}
	return nil
}

// SpecV2 is the closed v2 character document.
type SpecV2 struct {
	SchemaVersion  int                  `json:"schema_version"`
	RigVersion     string               `json:"rig_version"`
	CatalogVersion int                  `json:"catalog_version"`
	Info           CharacterInfo        `json:"info"`
	Identity       Identity             `json:"identity"`
	Body           Body                 `json:"body"`
	Clothes        Clothes              `json:"clothes"`
	Personality    CharacterPersonality `json:"personality"`
	Expressions    []string             `json:"expressions"`
	VoiceKey       string               `json:"voice_key"`
}

// SpecError is a deterministic validation failure: a stable reason code and the
// JSON path of the first offending value. Error() returns only the code.
type SpecError struct {
	Code string
	Path string
}

func (e *SpecError) Error() string { return e.Code }

func specErr(code, path string) *SpecError { return &SpecError{Code: code, Path: path} }

// ValidateSpecV2JSON validates raw bytes against schema v2 and returns the
// typed document. Reason codes match v2.fixtures.json and the TypeScript validator.
func ValidateSpecV2JSON(raw []byte) (*SpecV2, error) {
	var value any
	d := json.NewDecoder(bytes.NewReader(raw))
	if err := d.Decode(&value); err != nil || d.Decode(new(any)) != io.EOF {
		return nil, specErr("spec_not_object", "")
	}
	root, ok := value.(map[string]any)
	if !ok {
		return nil, specErr("spec_not_object", "")
	}
	if err := checkV2(root); err != nil {
		return nil, err
	}
	var spec SpecV2
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&spec); err != nil {
		return nil, specErr("invalid_type", "")
	}
	if err := crossCheckV2(&spec); err != nil {
		return nil, err
	}
	return &spec, nil
}

func checkV2(root map[string]any) *SpecError {
	compact, _ := marshalNoEscape(root)
	if len(compact) > maxSpecBytes {
		return specErr("spec_too_large", "")
	}
	if n, ok := root["schema_version"].(float64); !ok || n != SchemaV2 {
		return specErr("unsupported_schema_version", "schema_version")
	}
	if s, ok := root["rig_version"].(string); !ok || s != RigV2 {
		return specErr("unsupported_rig_version", "rig_version")
	}
	if n, ok := root["catalog_version"].(float64); !ok || n != CatalogVersion {
		return specErr("unsupported_catalog_version", "catalog_version")
	}
	var schema map[string]any
	if err := json.Unmarshal(SpecSchemaV2, &schema); err != nil {
		return specErr("invalid_schema", "")
	}
	return shapeV2(root, schema, "")
}

func marshalNoEscape(v any) ([]byte, error) {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	err := e.Encode(v)
	return bytes.TrimRight(b.Bytes(), "\n"), err
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// shapeV2 interprets the schema keyword subset documented in v2.json. Traversal
// order (required, unknown, then sorted properties) is part of the contract.
func shapeV2(value any, schema map[string]any, path string) *SpecError {
	if c, ok := schema["const"]; ok && fmt.Sprint(c) != fmt.Sprint(value) {
		return specErr("unknown_catalog_id", path)
	}
	if list, ok := schema["enum"].([]any); ok {
		text, isString := value.(string)
		if !isString {
			return specErr("invalid_type", path)
		}
		found := false
		for _, item := range list {
			if item == text {
				found = true
			}
		}
		if !found {
			return specErr("unknown_catalog_id", path)
		}
	}
	switch schema["type"] {
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return specErr("invalid_type", path)
		}
		props, _ := schema["properties"].(map[string]any)
		if disc, ok := schema["discriminator"].(string); ok {
			kind, present := obj[disc]
			if !present {
				return specErr("missing_field", join(path, disc))
			}
			branches, _ := schema["oneOf"].([]any)
			for _, b := range branches {
				branch := b.(map[string]any)
				if fmt.Sprint(branch["properties"].(map[string]any)[disc].(map[string]any)["const"]) == fmt.Sprint(kind) {
					return shapeV2(value, branch, path)
				}
			}
			return specErr("unknown_catalog_id", join(path, disc))
		}
		if req, ok := schema["required"].([]any); ok {
			names := make([]string, 0, len(req))
			for _, r := range req {
				names = append(names, r.(string))
			}
			sort.Strings(names)
			for _, name := range names {
				if _, present := obj[name]; !present {
					return specErr("missing_field", join(path, name))
				}
			}
		}
		if schema["additionalProperties"] == false {
			for _, key := range sortedKeys(obj) {
				if _, known := props[key]; !known {
					return specErr("unknown_field", join(path, key))
				}
			}
		}
		for _, key := range sortedKeys(props) {
			if child, present := obj[key]; present {
				if err := shapeV2(child, props[key].(map[string]any), join(path, key)); err != nil {
					return err
				}
			}
		}
	case "string":
		s, ok := value.(string)
		if !ok {
			return specErr("invalid_type", path)
		}
		n := utf8.RuneCountInString(s)
		if lo, ok := schema["minLength"].(float64); ok && float64(n) < lo {
			return specErr("invalid_length", path)
		}
		if hi, ok := schema["maxLength"].(float64); ok && float64(n) > hi {
			return specErr("invalid_length", path)
		}
		if pat, ok := schema["pattern"].(string); ok && pat == "^#[0-9A-Fa-f]{6}$" && !hexColorRegex.MatchString(s) {
			return specErr("invalid_format", path)
		}
	case "integer":
		n, ok := value.(float64)
		if !ok || n != math.Trunc(n) {
			return specErr("invalid_type", path)
		}
	case "array":
		list, ok := value.([]any)
		if !ok {
			return specErr("invalid_type", path)
		}
		if lo, ok := schema["minItems"].(float64); ok && float64(len(list)) < lo {
			return specErr("invalid_length", path)
		}
		if hi, ok := schema["maxItems"].(float64); ok && float64(len(list)) > hi {
			return specErr("invalid_length", path)
		}
		items, _ := schema["items"].(map[string]any)
		for i, item := range list {
			if err := shapeV2(item, items, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
		if schema["uniqueItems"] == true {
			by, _ := schema["uniqueBy"].(string)
			seen := map[string]bool{}
			for i, item := range list {
				key := fmt.Sprint(item)
				if by != "" {
					key = fmt.Sprint(item.(map[string]any)[by])
				}
				if seen[key] {
					return specErr("duplicate_item", fmt.Sprintf("%s[%d]", path, i))
				}
				seen[key] = true
			}
		}
	default:
		return specErr("invalid_type", path)
	}
	return nil
}

// ValidateSpecV2 checks text safety, size and cross-field rules on a typed
// document. Structural checks are in ValidateSpecV2JSON; callers that build a
// SpecV2 in memory (the upgrader, the compiler tests) use this entry point.
func ValidateSpecV2(spec *SpecV2) error {
	if spec == nil {
		return specErr("spec_not_object", "")
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		return specErr("invalid_type", "")
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return specErr("invalid_type", "")
	}
	if err := checkV2(generic); err != nil {
		return err
	}
	// A typed nil *SpecError must not become a non-nil error.
	if err := crossCheckV2(spec); err != nil {
		return err
	}
	return nil
}

func crossCheckV2(spec *SpecV2) *SpecError {
	texts := []struct{ path, text string }{
		{"info.name", spec.Info.Name}, {"info.bio", spec.Info.Bio}, {"info.backstory", spec.Info.Backstory},
		{"identity.pronouns", spec.Identity.Pronouns}, {"identity.label", spec.Identity.Label},
		{"personality.speaking_style", spec.Personality.SpeakingStyle}, {"personality.persona_prompt", spec.Personality.PersonaPrompt},
	}
	for i, t := range spec.Personality.Traits {
		texts = append(texts, struct{ path, text string }{fmt.Sprintf("personality.traits[%d]", i), t})
	}
	total := 0
	for _, t := range texts {
		if validateSafeText(t.text) != nil {
			return specErr("unsafe_text", t.path)
		}
		total += utf8.RuneCountInString(t.text)
	}
	if total > maxTextRunes {
		return specErr("text_too_long", "")
	}
	b := spec.Body
	// Robot eyes glow on the dark visor; other non-human eyes sit on the body colour.
	if b.Kind == "robot" && contrastBetween(robotVisor, b.Eyes.Color) < 2.5 ||
		b.Kind != "human" && b.Kind != "robot" && contrastBetween(b.PrimaryColor, b.Eyes.Color) < 2.5 {
		return specErr("low_contrast", "body.eyes.color")
	}
	switch b.Kind {
	case "animal":
		for _, a := range [][2]string{{"ears", b.Ears}, {"tail", b.Tail}, {"markings", b.Markings}} {
			if !speciesAllows("animal", b.Species, a[0], a[1]) {
				return specErr("species_attribute_mismatch", "body."+a[0])
			}
		}
	case "fantasy":
		for _, a := range [][2]string{{"wings", b.Wings}, {"horns", b.Horns}, {"tail", b.Tail}} {
			if !speciesAllows("fantasy", b.Species, a[0], a[1]) {
				return specErr("species_attribute_mismatch", "body."+a[0])
			}
		}
	}
	if b.Kind == "human" {
		c := spec.Clothes
		if (b.AgeBand == "child" || b.AgeBand == "teen") &&
			(c.Top.Style == "none" || (c.Bottom.Style == "none" && c.Top.Style != "dress" && c.Top.Style != "overalls")) {
			return specErr("clothing_required_for_minor", "clothes")
		}
		if b.FacialHair.Style != "none" && b.AgeBand == "child" {
			return specErr("facial_hair_age", "body.facial_hair")
		}
		if b.Hair.Length == "bald" && b.Hair.Style != "loose" {
			return specErr("hair_style_mismatch", "body.hair.style")
		}
		if b.Hair.Length == "buzz" && b.Hair.Style != "loose" && b.Hair.Style != "mohawk" {
			return specErr("hair_style_mismatch", "body.hair.style")
		}
	}
	kinds := map[string]bool{}
	for _, a := range spec.Clothes.Accessories {
		kinds[a.Kind] = true
	}
	if kinds["tie"] && kinds["bow_tie"] {
		return specErr("incompatible_accessories", "clothes.accessories")
	}
	if kinds["hat"] && b.Kind == "human" && b.HeadCovering.Style != "none" {
		return specErr("incompatible_accessories", "clothes.accessories")
	}
	if kinds["headphones"] && b.Kind == "human" && (b.HeadCovering.Style == "helmet" || b.HeadCovering.Style == "hood") {
		return specErr("incompatible_accessories", "clothes.accessories")
	}
	return nil
}

const robotVisor = "#16242F"

func contrastBetween(a, b string) float64 {
	return contrastRatio(relativeLuminance(a), relativeLuminance(b))
}
