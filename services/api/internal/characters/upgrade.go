package characters

import (
	"bytes"
	"encoding/json"
)

const noneColor = "#1A1A24"

var (
	v1Species = map[string]string{"round": "slime", "bean": "ghost", "star": "fairy"}
	v1Eyes    = map[string]string{"round": "round", "oval": "wide", "sleepy": "sleepy"}
)

func none() Styled { return Styled{Style: "none", Color: noneColor} }

// UpgradeV1 maps a validated v1 document to v2 without loss: the abstract blob
// becomes a fantasy creature, the palette becomes colours, accessories keep
// their meaning. The result is validated by ValidateSpecV2 (see tests).
func UpgradeV1(old *CharacterSpec) *SpecV2 {
	p := old.Appearance.Palette
	b := Body{
		Kind: "fantasy", AgeBand: "adult", Build: "average", Height: "medium",
		Glasses: none(), Mouth: old.Appearance.Mouth,
		Eyes:    Eyes{Shape: v1Eyes[old.Appearance.Eyes], Color: p.Outline},
		Species: v1Species[old.Appearance.BodyShape], PrimaryColor: p.Body, SecondaryColor: p.Accent,
		Wings: "none", Horns: "none", Tail: "none",
	}
	if b.Species == "fairy" {
		b.Wings = "insect"
	}
	accessories := []Accessory{}
	for _, a := range old.Appearance.Accessories {
		if a == "glasses" {
			b.Glasses = Styled{Style: "round", Color: p.Outline}
			continue
		}
		accessories = append(accessories, Accessory{Kind: a, Color: p.Accent})
	}
	return &SpecV2{
		SchemaVersion: SchemaV2, RigVersion: RigV2, CatalogVersion: CatalogVersion,
		Info:        old.Info,
		Identity:    Identity{},
		Body:        b,
		Clothes:     Clothes{Top: none(), Bottom: none(), Shoes: none(), Outerwear: none(), Pattern: "solid", Accessories: accessories},
		Personality: old.Personality,
		Expressions: append([]string(nil), validExpressionsV2...),
		VoiceKey:    old.VoiceKey,
	}
}

// DefaultSpecV2 is the neutral document rendered when a stored document has an
// unknown future version or catalog id. It is never written back.
func DefaultSpecV2() *SpecV2 {
	return &SpecV2{
		SchemaVersion: SchemaV2, RigVersion: RigV2, CatalogVersion: CatalogVersion,
		Info:     CharacterInfo{Name: "Guide"},
		Identity: Identity{},
		Body: Body{
			Kind: "fantasy", AgeBand: "adult", Build: "round", Height: "medium", Glasses: none(), Mouth: "smile",
			Eyes:    Eyes{Shape: "round", Color: "#03045E"},
			Species: "slime", PrimaryColor: "#90E0EF", SecondaryColor: "#CAF0F8", Wings: "none", Horns: "none", Tail: "none",
		},
		Clothes:     Clothes{Top: none(), Bottom: none(), Shoes: none(), Outerwear: none(), Pattern: "solid", Accessories: []Accessory{}},
		Personality: CharacterPersonality{Traits: []string{"friendly"}, SpeakingStyle: "Clear and kind", PersonaPrompt: "Explain simply and encourage the Learner."},
		Expressions: append([]string(nil), validExpressionsV2...),
		VoiceKey:    "voice-provisional-guide",
	}
}

// Reading is the result of resolving any stored Character document.
type Reading struct {
	Spec          *SpecV2
	SourceVersion int
	// Upgraded is true for stored v1 documents (read-time upgrade, never rewritten).
	Upgraded bool
	// ReadOnly documents render the neutral default and refuse writes: the
	// stored schema_version or catalog id is newer than this build understands.
	ReadOnly bool
}

// ReadSpec resolves a stored document of any supported version. Invalid
// documents return an error; unknown future versions or catalog ids degrade.
func ReadSpec(raw []byte) (Reading, error) {
	var head struct {
		SchemaVersion  json.Number `json:"schema_version"`
		CatalogVersion json.Number `json:"catalog_version"`
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	_ = d.Decode(&head)
	version, _ := head.SchemaVersion.Int64()
	catalog, _ := head.CatalogVersion.Int64()
	switch {
	case version == 1:
		old, err := ValidateSpecJSON(raw)
		if err != nil {
			return Reading{}, err
		}
		return Reading{Spec: UpgradeV1(old), SourceVersion: 1, Upgraded: true}, nil
	case version == SchemaV2 && catalog <= CatalogVersion:
		spec, err := ValidateSpecV2JSON(raw)
		if err != nil {
			if se, ok := err.(*SpecError); ok && se.Code == "unknown_catalog_id" {
				return Reading{Spec: DefaultSpecV2(), SourceVersion: 2, ReadOnly: true}, nil
			}
			return Reading{}, err
		}
		return Reading{Spec: spec, SourceVersion: 2}, nil
	case version > SchemaV2 || (version == SchemaV2 && catalog > CatalogVersion):
		return Reading{Spec: DefaultSpecV2(), SourceVersion: int(version), ReadOnly: true}, nil
	}
	return Reading{}, specErr("unsupported_schema_version", "schema_version")
}
