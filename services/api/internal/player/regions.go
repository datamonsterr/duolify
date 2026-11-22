package player

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Region limits match engine/config/regions.toml (regions_test keeps them in step).
// Publication re-validates Engine output with these rules instead of trusting it.
const (
	MaxRegionsPerSlide       = 6
	MaxRegionDescriptionRune = 300
	MaxRegionOverlap         = 0.2
	maxRegionIDRunes         = 40
)

// RegionRoles is the closed role set of the Slide manifest.
var RegionRoles = []string{"title", "explanation", "example", "diagram", "code", "question", "caption"}

var regionID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)
var regionMarkup = regexp.MustCompile(`(?i)<[a-z/!?]|&(?:#[0-9]+|#x[0-9a-f]+|[a-z][a-z0-9]*);`)

// RegionBox is a rectangle in normalized coordinates (0..1) of the 16:9 stage, origin top left.
type RegionBox struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// RegionWindow is a window on the Slide clock: StartMS <= t < EndMS.
type RegionWindow struct {
	StartMS int `json:"start_ms"`
	EndMS   int `json:"end_ms"`
}

// Region is one declared block of a Slide: derived lesson content, never learner data.
type Region struct {
	ID          string        `json:"id"`
	Box         RegionBox     `json:"box"`
	Window      *RegionWindow `json:"window,omitempty"`
	Role        string        `json:"role"`
	Description string        `json:"description"`
}

func (b RegionBox) area() float64 { return b.W * b.H }

func (b RegionBox) intersection(o RegionBox) float64 {
	w := math.Min(b.X+b.W, o.X+o.W) - math.Max(b.X, o.X)
	h := math.Min(b.Y+b.H, o.Y+o.H) - math.Max(b.Y, o.Y)
	if w <= 0 || h <= 0 {
		return 0
	}
	return w * h
}

// InsideStage reports a finite, non-empty box within the stage.
func (b RegionBox) InsideStage() bool {
	for _, v := range []float64{b.X, b.Y, b.W, b.H} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	const eps = 1e-9
	return b.X >= 0 && b.Y >= 0 && b.W > 0 && b.H > 0 && b.X+b.W <= 1+eps && b.Y+b.H <= 1+eps
}

func (w *RegionWindow) contains(atMS int) bool {
	return w == nil || (atMS >= w.StartMS && atMS < w.EndMS)
}

func windowsMeet(a, b *RegionWindow) bool {
	return a == nil || b == nil || (a.StartMS < b.EndMS && b.StartMS < a.EndMS)
}

// PlainText rejects control and bidi override characters, tags and HTML entities.
func PlainText(s string) bool {
	if !utf8.ValidString(s) || regionMarkup.MatchString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069) {
			return false
		}
	}
	return true
}

func knownRole(role string) bool {
	for _, r := range RegionRoles {
		if r == role {
			return true
		}
	}
	return false
}

// ValidateRegions applies the publication rules to one Slide's regions. No regions is valid.
func ValidateRegions(regions []Region, durationMS int) error {
	if len(regions) > MaxRegionsPerSlide {
		return errInvalid
	}
	seen := map[string]bool{}
	for i, r := range regions {
		desc := strings.TrimSpace(r.Description)
		if !regionID.MatchString(r.ID) || seen[r.ID] || !r.Box.InsideStage() || !knownRole(r.Role) ||
			desc == "" || utf8.RuneCountInString(r.Description) > MaxRegionDescriptionRune || !PlainText(r.Description) {
			return errInvalid
		}
		if w := r.Window; w != nil && (w.StartMS < 0 || w.StartMS >= w.EndMS || w.EndMS > durationMS) {
			return errInvalid
		}
		seen[r.ID] = true
		for _, o := range regions[:i] {
			smaller := math.Min(o.Box.area(), r.Box.area())
			if windowsMeet(o.Window, r.Window) && o.Box.intersection(r.Box)/smaller > MaxRegionOverlap {
				return errInvalid
			}
		}
	}
	return nil
}

// RegionMatch is a declared region intersecting a selected rectangle.
type RegionMatch struct {
	Region Region
	// Overlap is intersection over union of the selection and the region box (0..1].
	Overlap float64
}

// RegionsAt returns the regions of a manifest Slide visible at atMS that intersect rect,
// ranked by overlap (highest first, then manifest order). It never reads Slide pixels; an
// empty result means the caller grounds on the whole Slide.
func RegionsAt(slide ObjectSlide, rect RegionBox, atMS int) []RegionMatch {
	if !rect.InsideStage() {
		return nil
	}
	var out []RegionMatch
	for _, r := range slide.Regions {
		if !r.Window.contains(atMS) {
			continue
		}
		inter := r.Box.intersection(rect)
		if inter <= 0 {
			continue
		}
		out = append(out, RegionMatch{Region: r, Overlap: inter / (r.Box.area() + rect.area() - inter)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Overlap > out[j].Overlap })
	return out
}
