package characters

import (
	"errors"
	"fmt"
	"strings"
)

// CompileSVG compiles a validated CharacterSpec into pure, trusted parametric SVG.
func CompileSVG(spec *CharacterSpec, expression string) (string, error) {
	if spec == nil {
		return "", errors.New("spec is nil")
	}

	validExp := false
	for _, e := range validExpressions {
		if e == expression {
			validExp = true
			break
		}
	}
	if !validExp {
		return "", fmt.Errorf("unsupported expression: %s", expression)
	}

	palette := spec.Appearance.Palette
	var b strings.Builder

	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 160 160" class="character-rig" data-expression="%s">`, expression)

	// 1. Base body shape (Invariant across all expressions)
	switch spec.Appearance.BodyShape {
	case "star":
		fmt.Fprintf(&b, `<polygon data-part="body-shape" points="80,18 97,55 138,58 107,85 117,126 80,103 43,126 53,85 22,58 63,55" fill="%s" stroke="%s" stroke-width="5" stroke-linejoin="round"/>`, palette.Body, palette.Outline)
	case "bean":
		fmt.Fprintf(&b, `<path data-part="body-shape" d="M 50,45 C 50,20 110,20 110,45 C 110,75 125,95 115,125 C 105,145 55,145 45,125 C 35,95 50,75 50,45 Z" fill="%s" stroke="%s" stroke-width="5" stroke-linejoin="round"/>`, palette.Body, palette.Outline)
	case "round":
		fallthrough
	default:
		fmt.Fprintf(&b, `<circle data-part="body-shape" cx="80" cy="80" r="54" fill="%s" stroke="%s" stroke-width="5"/>`, palette.Body, palette.Outline)
	}

	// 2. Accessories - behind face if hat, on body if scarf/badge (Invariant)
	for _, acc := range spec.Appearance.Accessories {
		switch acc {
		case "hat":
			fmt.Fprintf(&b, `<path data-part="accessory-hat" d="M 55,38 L 80,12 L 105,38 Z" fill="%s" stroke="%s" stroke-width="3"/>`, palette.Accent, palette.Outline)
		case "scarf":
			fmt.Fprintf(&b, `<path data-part="accessory-scarf" d="M 46,108 Q 80,120 114,108 Q 118,124 100,128 L 104,146 L 88,146 L 86,128 Q 42,124 46,108 Z" fill="%s" stroke="%s" stroke-width="3"/>`, palette.Accent, palette.Outline)
		case "badge":
			fmt.Fprintf(&b, `<circle data-part="accessory-badge" cx="112" cy="106" r="9" fill="%s" stroke="%s" stroke-width="2"/>`, palette.Accent, palette.Outline)
		}
	}

	// 3. Eyes (Changes with expression)
	switch expression {
	case "happy":
		fmt.Fprintf(&b, `<path data-part="eyes-happy" d="M 56,72 Q 66,60 76,72 M 84,72 Q 94,60 104,72" fill="none" stroke="%s" stroke-width="4" stroke-linecap="round"/>`, palette.Outline)
	case "sad":
		fmt.Fprintf(&b, `<path data-part="eyes-sad" d="M 56,66 Q 66,76 76,66 M 84,66 Q 94,76 104,66" fill="none" stroke="%s" stroke-width="4" stroke-linecap="round"/>`, palette.Outline)
	case "thinking":
		fmt.Fprintf(&b, `<ellipse data-part="eyes-thinking" cx="66" cy="64" rx="5" ry="6" fill="%s"/><ellipse cx="94" cy="58" rx="5" ry="6" fill="%s"/><path d="M 88,48 Q 96,44 104,49" fill="none" stroke="%s" stroke-width="3" stroke-linecap="round"/>`, palette.Outline, palette.Outline, palette.Outline)
	case "celebrate":
		fmt.Fprintf(&b, `<path data-part="eyes-celebrate" d="M 56,70 L 66,62 L 76,70 M 84,70 L 94,62 L 104,70" fill="none" stroke="%s" stroke-width="4" stroke-linecap="round"/>`, palette.Outline)
	case "idle":
		fallthrough
	default:
		switch spec.Appearance.Eyes {
		case "oval":
			fmt.Fprintf(&b, `<ellipse data-part="eyes-idle" cx="66" cy="68" rx="5" ry="8" fill="%s"/><ellipse cx="94" cy="68" rx="5" ry="8" fill="%s"/>`, palette.Outline, palette.Outline)
		case "sleepy":
			fmt.Fprintf(&b, `<path data-part="eyes-idle" d="M 58,70 Q 66,75 74,70 M 86,70 Q 94,75 102,70" fill="none" stroke="%s" stroke-width="4" stroke-linecap="round"/>`, palette.Outline)
		case "round":
			fallthrough
		default:
			fmt.Fprintf(&b, `<circle data-part="eyes-idle" cx="66" cy="68" r="6" fill="%s"/><circle cx="94" cy="68" r="6" fill="%s"/>`, palette.Outline, palette.Outline)
		}
	}

	// 4. Mouth (Changes with expression)
	switch expression {
	case "happy":
		fmt.Fprintf(&b, `<path data-part="mouth-happy" d="M 64,88 Q 80,112 96,88 Z" fill="%s" stroke="%s" stroke-width="3"/>`, palette.Accent, palette.Outline)
	case "sad":
		fmt.Fprintf(&b, `<path data-part="mouth-sad" d="M 68,102 Q 80,90 92,102" fill="none" stroke="%s" stroke-width="3" stroke-linecap="round"/>`, palette.Outline)
	case "thinking":
		fmt.Fprintf(&b, `<ellipse data-part="mouth-thinking" cx="86" cy="94" rx="4" ry="4" fill="%s"/>`, palette.Outline)
	case "celebrate":
		fmt.Fprintf(&b, `<ellipse data-part="mouth-celebrate" cx="80" cy="95" rx="14" ry="10" fill="%s" stroke="%s" stroke-width="3"/>`, palette.Accent, palette.Outline)
	case "idle":
		fallthrough
	default:
		switch spec.Appearance.Mouth {
		case "line":
			fmt.Fprintf(&b, `<line data-part="mouth-idle" x1="68" y1="92" x2="92" y2="92" stroke="%s" stroke-width="3" stroke-linecap="round"/>`, palette.Outline)
		case "open":
			fmt.Fprintf(&b, `<ellipse data-part="mouth-idle" cx="80" cy="92" rx="7" ry="9" fill="%s" stroke="%s" stroke-width="2"/>`, palette.Outline, palette.Outline)
		case "smile":
			fallthrough
		default:
			fmt.Fprintf(&b, `<path data-part="mouth-idle" d="M 68,88 Q 80,102 92,88" fill="none" stroke="%s" stroke-width="3" stroke-linecap="round"/>`, palette.Outline)
		}
	}

	// 5. Glasses accessory (Drawn over eyes if present)
	for _, acc := range spec.Appearance.Accessories {
		if acc == "glasses" {
			fmt.Fprintf(&b, `<g data-part="accessory-glasses" stroke="%s" stroke-width="3" fill="none"><circle cx="66" cy="68" r="14"/><circle cx="94" cy="68" r="14"/><line x1="80" y1="68" x2="80" y2="68"/></g>`, palette.Outline)
		}
	}

	b.WriteString(`</svg>`)
	return b.String(), nil
}

// BundledFallbackSVG produces a safe static mascot placeholder SVG.
func BundledFallbackSVG(expression string) string {
	spec := BundledFallbackSpec()
	svg, err := CompileSVG(&spec, expression)
	if err != nil {
		return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 160 160" class="fallback-mascot" data-fallback="true"><circle cx="80" cy="80" r="50" fill="#E0E0E0" stroke="#888888" stroke-width="4"/><circle cx="65" cy="70" r="5" fill="#444"/><circle cx="95" cy="70" r="5" fill="#444"/><path d="M 70 95 Q 80 105 90 95" fill="none" stroke="#444" stroke-width="3"/></svg>`
	}
	return strings.Replace(svg, `class="character-rig"`, `class="character-rig fallback-mascot" data-fallback="true"`, 1)
}
