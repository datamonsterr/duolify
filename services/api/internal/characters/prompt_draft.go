package characters

import (
	"encoding/json"
	"io"
	"slices"
	"strings"
)

type promptAttribute struct {
	Field string `json:"field"`
	Value string `json:"value"`
}

func decodePromptAttributes(raw string) ([]promptAttribute, error) {
	var proposal struct {
		Attributes []promptAttribute `json:"attributes"`
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	invalid := specErr("invalid_spec", "")
	if len(raw) > maxSpecBytes || dec.Decode(&proposal) != nil || dec.Decode(new(any)) != io.EOF ||
		proposal.Attributes == nil || len(proposal.Attributes) > 64 {
		return nil, invalid
	}
	return proposal.Attributes, nil
}

// promptDraft applies sparse catalog choices to neutral editor defaults.
func promptDraft(raw string) (*SpecV2, error) {
	attributes, err := decodePromptAttributes(raw)
	if err != nil {
		return nil, err
	}
	return buildPromptDraft(attributes)
}

// Recover choices that still form a complete, valid editor document. Apply
// kind and species before their dependent choices so sparse repairs can carry
// useful parts even when the full proposal conflicts with a neutral default.
func partialPromptDraft(raw string) *SpecV2 {
	attributes, err := decodePromptAttributes(raw)
	if err != nil {
		return nil
	}
	priority := func(field string) int {
		switch field {
		case "body.kind":
			return 0
		case "body.species":
			return 1
		default:
			return 2
		}
	}
	slices.SortStableFunc(attributes, func(a, b promptAttribute) int {
		return priority(a.Field) - priority(b.Field)
	})
	var kept []promptAttribute
	var best *SpecV2
	for _, attribute := range attributes {
		candidate := append(kept, attribute)
		if draft, err := buildPromptDraft(candidate); err == nil {
			kept, best = candidate, draft
		}
	}
	return best
}

func buildPromptDraft(attributes []promptAttribute) (*SpecV2, error) {
	invalid := specErr("invalid_spec", "")
	first := func(field, fallback string) string {
		for _, attr := range attributes {
			if attr.Field == field {
				return attr.Value
			}
		}
		return fallback
	}
	kind := first("body.kind", "human")
	doc := neutralPromptDraft(kind, first("body.species", ""))
	base, err := json.Marshal(doc)
	if err != nil {
		return nil, invalid
	}
	var tree map[string]any
	if json.Unmarshal(base, &tree) != nil {
		return nil, invalid
	}
	body := tree["body"].(map[string]any)
	clothes := tree["clothes"].(map[string]any)
	seen := make(map[string]bool)
	accessories := map[string]string{}
	for _, attr := range attributes {
		key := attr.Field
		switch {
		case key == "clothes.accessory":
			if _, exists := accessories[attr.Value]; !exists {
				accessories[attr.Value] = noneColor
			}
		case strings.HasPrefix(key, "clothes.accessories."):
			id := strings.TrimPrefix(key, "clothes.accessories.")
			accessories[id] = attr.Value
		case key == "body.skin_features" && kind == "human":
			list := body["skin_features"].([]any)
			body["skin_features"] = append(list, attr.Value)
		case strings.HasPrefix(key, "body.") || strings.HasPrefix(key, "clothes."):
			if seen[key] || !setDraftAttribute(tree, strings.Split(key, "."), attr.Value) {
				return nil, invalid
			}
			seen[key] = true
		default:
			return nil, invalid
		}
	}
	for _, id := range sortedKeys(accessories) {
		clothes["accessories"] = append(clothes["accessories"].([]any), map[string]string{"kind": id, "color": accessories[id]})
	}
	updated, err := json.Marshal(tree)
	if err != nil {
		return nil, invalid
	}
	return ValidateSpecV2JSON(updated)
}

func setDraftAttribute(node map[string]any, path []string, value string) bool {
	if len(path) == 1 {
		if _, ok := node[path[0]].(string); !ok {
			return false
		}
		node[path[0]] = value
		return true
	}
	child, ok := node[path[0]].(map[string]any)
	return ok && setDraftAttribute(child, path[1:], value)
}

func neutralPromptDraft(kind, species string) *SpecV2 {
	doc := DefaultSpecV2()
	doc.Info.Name = "New character"
	doc.Clothes.Top = Styled{Style: "tee", Color: "#4DA6F0"}
	doc.Clothes.Bottom = Styled{Style: "pants", Color: "#2E4057"}
	doc.Clothes.Shoes = Styled{Style: "sneakers", Color: "#F5F1E8"}
	body := Body{
		Kind: kind, AgeBand: "adult", Build: "average", Height: "medium", Glasses: none(),
		Mouth: "smile", Eyes: Eyes{Shape: "round", Color: noneColor},
	}
	switch kind {
	case "human":
		body.SkinTone, body.FaceShape, body.Nose = "t06", "oval", "round"
		body.Hair = Hair{Texture: "wavy", Length: "medium", Style: "loose", Color: "#3A2418"}
		body.HeadCovering, body.FacialHair = none(), none()
		body.Eyes.Color = "#4B3621"
		body.Eyebrows = Styled{Style: "arched", Color: "#3A2418"}
	case "animal":
		body.Species, body.PrimaryColor, body.SecondaryColor = "fox", "#E07A3F", "#FFF3E0"
	case "robot":
		body.Base, body.PrimaryColor, body.SecondaryColor = "boxy", "#9DB4C0", "#E4572E"
		body.Markings, body.Antenna, body.Locomotion = "panels", "single", "legs"
		body.Eyes, body.Mouth = Eyes{Shape: "screen", Color: "#7CFFB2"}, "line"
	case "fantasy":
		body.Species, body.PrimaryColor, body.SecondaryColor = "dragon", "#6BBF59", "#F2C94C"
		body.Eyes.Shape, body.Mouth = "wide", "open"
	}
	if kind == "animal" || kind == "fantasy" {
		if species != "" {
			body.Species = species
		}
		for attr, values := range speciesProfiles[kind][body.Species] {
			switch attr {
			case "ears":
				body.Ears = values[0]
			case "tail":
				body.Tail = values[0]
			case "markings":
				body.Markings = values[0]
			case "wings":
				body.Wings = values[0]
			case "horns":
				body.Horns = values[0]
			}
		}
	}
	doc.Body = body
	return doc
}
