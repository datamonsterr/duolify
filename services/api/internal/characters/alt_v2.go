package characters

import (
	"fmt"
	"strconv"
	"strings"
)

func words(id string) string { return strings.ReplaceAll(id, "_", " ") }

// colorNameV2 names a colour with integer maths only (parity with altText.ts).
func colorNameV2(hex string) string {
	r, g, b := HexToRGB(hex)
	mx, mn := max(r, g, b), min(r, g, b)
	l, d := (mx+mn)/2, mx-mn
	if d < 28 {
		switch {
		case l < 60:
			return "black"
		case l > 200:
			return "white"
		}
		return "gray"
	}
	var h int
	switch mx {
	case r:
		h = 60 * (g - b) / d
	case g:
		h = 120 + 60*(b-r)/d
	default:
		h = 240 + 60*(r-g)/d
	}
	h = ((h % 360) + 360) % 360
	switch {
	case h < 15 || h >= 335:
		if l > 190 {
			return "pink"
		}
		return "red"
	case h < 70:
		if l < 110 {
			return "brown"
		}
		if h < 45 {
			return "orange"
		}
		return "yellow"
	case h < 160:
		return "green"
	case h < 200:
		return "teal"
	case h < 255:
		return "blue"
	case h < 290:
		return "purple"
	}
	return "pink"
}

func skinBand(tone string) string {
	n, _ := strconv.Atoi(strings.TrimPrefix(tone, "t"))
	switch {
	case n <= 5:
		return "light"
	case n <= 10:
		return "medium"
	}
	return "deep"
}

// AltTextV2 builds the English description from attributes only. The
// TypeScript twin (altText in rig-v2-alt.ts) localizes the same template.
func AltTextV2(spec *SpecV2) string {
	b := spec.Body
	var kind string
	switch b.Kind {
	case "human":
		kind = fmt.Sprintf("human with %s skin tone", skinBand(b.SkinTone))
	case "robot":
		kind = colorNameV2(b.PrimaryColor) + " robot"
	default:
		kind = colorNameV2(b.PrimaryColor) + " " + words(b.Species)
	}
	parts := []string{fmt.Sprintf("%s, %s %s", spec.Info.Name, b.AgeBand, kind)}
	if b.Kind == "human" {
		switch b.Hair.Length {
		case "bald":
			parts = append(parts, "bald")
		default:
			hair := fmt.Sprintf("%s %s %s hair", b.Hair.Length, b.Hair.Texture, colorNameV2(b.Hair.Color))
			if b.Hair.Style != "loose" {
				hair += " in " + words(b.Hair.Style)
			}
			parts = append(parts, hair)
		}
	}
	parts = append(parts, colorNameV2(b.Eyes.Color)+" eyes")
	if b.Kind == "human" && b.FacialHair.Style != "none" {
		parts = append(parts, words(b.FacialHair.Style))
	}
	if b.Glasses.Style != "none" {
		parts = append(parts, words(b.Glasses.Style)+" glasses")
	}
	if b.Kind == "human" && b.HeadCovering.Style != "none" {
		parts = append(parts, colorNameV2(b.HeadCovering.Color)+" "+words(b.HeadCovering.Style))
	}
	worn := []string{}
	for _, item := range []Styled{spec.Clothes.Top, spec.Clothes.Bottom, spec.Clothes.Outerwear, spec.Clothes.Shoes} {
		if item.Style != "none" {
			worn = append(worn, "a "+colorNameV2(item.Color)+" "+words(item.Style))
		}
	}
	for _, a := range spec.Clothes.Accessories {
		worn = append(worn, "a "+words(a.Kind))
	}
	text := strings.Join(parts, ", ")
	if len(worn) > 0 {
		text += ", wearing " + strings.Join(worn[:len(worn)-1], ", ")
		if len(worn) > 1 {
			text += " and "
		}
		text += worn[len(worn)-1]
	}
	return text + "."
}
