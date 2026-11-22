package player

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
)

func box(x, y, w, h float64) RegionBox { return RegionBox{X: x, Y: y, W: w, H: h} }

func TestRegionLimitsMatchEngineConfig(t *testing.T) {
	f, err := os.Open("../../../../engine/config/regions.toml")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	values := map[string]string{}
	for sc := bufio.NewScanner(f); sc.Scan(); {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			break
		}
		if k, v, ok := strings.Cut(line, "="); ok && !strings.HasPrefix(line, "#") {
			values[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	overlap, _ := strconv.ParseFloat(values["max_overlap"], 64)
	if values["max_per_slide"] != strconv.Itoa(MaxRegionsPerSlide) || values["max_description_chars"] != strconv.Itoa(MaxRegionDescriptionRune) || overlap != MaxRegionOverlap {
		t.Fatalf("engine/config/regions.toml %v differs from the publication limits", values)
	}
}

func TestValidateRegions(t *testing.T) {
	ok := []Region{
		{ID: "t", Box: box(0.05, 0.04, 0.9, 0.18), Role: "title", Description: "Title: slices"},
		{ID: "c", Box: box(0.05, 0.26, 0.44, 0.52), Role: "code", Description: "s := a[1:3] and i < n"},
		{ID: "d", Box: box(0.51, 0.26, 0.44, 0.52), Window: &RegionWindow{StartMS: 1000, EndMS: 4000}, Role: "diagram", Description: "Backing array"},
	}
	if err := ValidateRegions(ok, 4000); err != nil {
		t.Fatal(err)
	}
	if ValidateRegions(nil, 1000) != nil {
		t.Fatal("a Slide without regions must stay valid")
	}
	mutate := func(f func(r []Region) []Region) []Region {
		c := append([]Region(nil), ok...)
		return f(c)
	}
	bad := map[string][]Region{
		"outside the stage": mutate(func(r []Region) []Region { r[0].Box = box(0.5, 0, 0.6, 0.1); return r }),
		"negative":          mutate(func(r []Region) []Region { r[0].Box = box(-0.1, 0, 0.5, 0.1); return r }),
		"empty box":         mutate(func(r []Region) []Region { r[0].Box = box(0.1, 0.1, 0, 0.1); return r }),
		"NaN":               mutate(func(r []Region) []Region { r[0].Box.X = math.NaN(); return r }),
		"unknown role":      mutate(func(r []Region) []Region { r[0].Role = "banner"; return r }),
		"markup":            mutate(func(r []Region) []Region { r[0].Description = "<b>x</b>"; return r }),
		"entity":            mutate(func(r []Region) []Region { r[0].Description = "a &amp; b"; return r }),
		"control":           mutate(func(r []Region) []Region { r[0].Description = "a\nb"; return r }),
		"too long": mutate(func(r []Region) []Region {
			r[0].Description = strings.Repeat("é", MaxRegionDescriptionRune+1)
			return r
		}),
		"empty text":        mutate(func(r []Region) []Region { r[0].Description = "  "; return r }),
		"duplicate id":      mutate(func(r []Region) []Region { r[1].ID = "t"; return r }),
		"bad id":            mutate(func(r []Region) []Region { r[1].ID = "a b"; return r }),
		"window past clock": mutate(func(r []Region) []Region { r[2].Window = &RegionWindow{StartMS: 0, EndMS: 5000}; return r }),
		"empty window":      mutate(func(r []Region) []Region { r[2].Window = &RegionWindow{StartMS: 2000, EndMS: 2000}; return r }),
		"overlap":           mutate(func(r []Region) []Region { r[2].Box = box(0.05, 0.26, 0.9, 0.52); return r }),
		"over the count": mutate(func(r []Region) []Region {
			for i := len(r); i <= MaxRegionsPerSlide; i++ {
				r = append(r, Region{ID: "x" + strconv.Itoa(i), Box: box(float64(i)/10, 0.9, 0.05, 0.05), Role: "caption", Description: "x"})
			}
			return r
		}),
	}
	for name, regions := range bad {
		if ValidateRegions(regions, 4000) == nil {
			t.Errorf("%s accepted", name)
		}
	}
	// Overlap is allowed when the windows do not meet.
	seq := mutate(func(r []Region) []Region {
		r[1].Window = &RegionWindow{StartMS: 0, EndMS: 1000}
		r[2].Box = box(0.05, 0.26, 0.9, 0.52)
		return r
	})
	if err := ValidateRegions(seq, 4000); err != nil {
		t.Fatal("disjoint windows may reuse a slot", err)
	}
}

func TestRegionsAtRanksByOverlapAndFiltersByTime(t *testing.T) {
	slide := ObjectSlide{DurationMS: 8000, Regions: []Region{
		{ID: "title", Box: box(0.05, 0.04, 0.9, 0.18), Role: "title", Description: "T"},
		{ID: "left", Box: box(0.05, 0.26, 0.44, 0.52), Role: "diagram", Description: "D"},
		{ID: "right", Box: box(0.51, 0.26, 0.44, 0.52), Window: &RegionWindow{StartMS: 2000, EndMS: 6000}, Role: "code", Description: "C"},
	}}
	ids := func(m []RegionMatch) string {
		var s []string
		for _, r := range m {
			s = append(s, r.Region.ID)
		}
		return strings.Join(s, ",")
	}
	// Mostly the diagram, a sliver of the code block.
	sel := box(0.1, 0.3, 0.45, 0.4)
	if got := ids(RegionsAt(slide, sel, 3000)); got != "left,right" {
		t.Fatalf("ranked %q", got)
	}
	// Before and after the code block's window it is not visible; end is exclusive.
	for _, at := range []int{1999, 6000} {
		if got := ids(RegionsAt(slide, sel, at)); got != "left" {
			t.Fatalf("at %d: %q", at, got)
		}
	}
	m := RegionsAt(slide, box(0.05, 0.26, 0.44, 0.52), 0)
	if len(m) != 1 || math.Abs(m[0].Overlap-1) > 1e-9 {
		t.Fatalf("an exact selection has overlap 1: %+v", m)
	}
	// Touching edges and empty space intersect nothing; out-of-stage rectangles are refused.
	if len(RegionsAt(slide, box(0.95, 0.9, 0.05, 0.05), 0)) != 0 || RegionsAt(slide, box(0.9, 0.9, 0.2, 0.2), 0) != nil {
		t.Fatal("no region expected")
	}
	if RegionsAt(ObjectSlide{DurationMS: 1000}, sel, 0) != nil {
		t.Fatal("a Slide without regions grounds on the whole Slide")
	}
	// IoU: a selection covering a small region fully outranks a large region it touches partly.
	two := ObjectSlide{Regions: []Region{
		{ID: "big", Box: box(0, 0, 1, 0.5), Role: "explanation", Description: "B"},
		{ID: "small", Box: box(0.4, 0.5, 0.2, 0.2), Role: "example", Description: "S"},
	}}
	if got := ids(RegionsAt(two, box(0.4, 0.45, 0.2, 0.25), 0)); got != "small,big" {
		t.Fatalf("ranked %q", got)
	}
}

func TestManifestRegionsStayServerSideAndOlderManifestsDecode(t *testing.T) {
	older := `{"version":1,"slides":[{"slide_id":"s1","duration_ms":1000,"html_key":"lessons/a/s1.html","assets":[],"checkpoints":[],"narration_text":"n"}]}`
	if _, err := decodeManifest([]byte(older), "https://assets.example.com"); err != nil {
		t.Fatal("manifest without regions must decode", err)
	}
	with := `{"version":1,"slides":[{"slide_id":"s1","duration_ms":1000,"html_key":"lessons/a/s1.html","assets":[],"checkpoints":[],"narration_text":"n","regions":[{"id":"t","box":{"x":0.05,"y":0.04,"w":0.9,"h":0.18},"role":"title","description":"Title"}]}]}`
	m, err := decodeManifest([]byte(with), "https://assets.example.com")
	if err != nil || len(m.Slides[0].Regions) != 1 || m.Slides[0].Regions[0].Box.W != 0.9 {
		t.Fatal(m, err)
	}
	outside := strings.Replace(with, `"w":0.9`, `"w":0.99`, 1)
	if _, err = decodeManifest([]byte(outside), "https://assets.example.com"); err == nil {
		t.Fatal("a region outside the stage must be rejected")
	}
	raw, _ := json.Marshal(Slide{})
	if strings.Contains(string(raw), "region") {
		t.Fatal("the public Slide projection must not expose regions", string(raw))
	}
}
