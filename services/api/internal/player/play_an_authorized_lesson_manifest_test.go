package player

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestManifestValidation(t *testing.T) {
	valid := ObjectManifest{Version: 1, Slides: []ObjectSlide{{ID: "sky-1", DurationMS: 3000, HTMLKey: "lessons/v1/one.html", Assets: []string{}, Checkpoints: []Checkpoint{{ID: "turn", AtMS: 1500}}, NarrationText: "Earth turns."}}}
	for _, test := range []struct {
		name   string
		change func(*ObjectManifest)
	}{
		{"empty", func(m *ObjectManifest) { m.Slides = nil }},
		{"duplicate", func(m *ObjectManifest) { m.Slides = append(m.Slides, m.Slides[0]) }},
		{"duration", func(m *ObjectManifest) { m.Slides[0].DurationMS = 0 }},
		{"checkpoint", func(m *ObjectManifest) { m.Slides[0].Checkpoints[0].AtMS = 4000 }},
		{"asset", func(m *ObjectManifest) { m.Slides[0].Assets = []string{"https://evil.test/a.png"} }},
		{"key traversal", func(m *ObjectManifest) { m.Slides[0].HTMLKey = "../private" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, _ := json.Marshal(valid)
			var m ObjectManifest
			_ = json.Unmarshal(raw, &m)
			test.change(&m)
			if ValidateManifest(m, "https://assets.example.test") == nil {
				t.Fatal("accepted invalid manifest")
			}
		})
	}
	if err := ValidateManifest(valid, "https://assets.example.test"); err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"https://assets.example.test/unsafe", "https://assets.example.test' evil", "https://user:pass@assets.example.test", "https://assets.example.test?x=1"} {
		if ValidateManifest(valid, origin) == nil {
			t.Fatal("unsafe CSP origin", origin)
		}
	}
}

func TestSlideHTMLValidation(t *testing.T) {
	for _, html := range []string{
		`<html><head></head><body><img src="https://evil.test/track"></body></html>`,
		`<html><head><base href="https://evil.test"></head><body>Hi</body></html>`,
		`<html><head><meta http-equiv="refresh" content="0;url=https://evil.test"></head><body>Hi</body></html>`,
		`<html><head></head><body><iframe srcdoc="secret"></iframe></body></html>`,
		`<html><head><style>div{background:url(//evil.test/x)}</style></head><body>Hi</body></html>`,
		`<html><head></head><body><script src="&#104;ttps://evil.test/a.js"></script></body></html>`,
	} {
		if ValidateHTML(html, "https://assets.example.test") == nil {
			t.Fatal("accepted unsafe HTML", html)
		}
	}
	if err := ValidateHTML(`<html><head><style>p{color:green}</style></head><body><img src="https://assets.example.test/star.svg"><script>parent.postMessage({type:"ended"},"*")</script></body></html>`, "https://assets.example.test"); err != nil {
		t.Fatal(err)
	}
	if ValidateHTML(strings.Repeat("x", 2<<20), "https://assets.example.test") == nil {
		t.Fatal("oversized HTML")
	}
}
