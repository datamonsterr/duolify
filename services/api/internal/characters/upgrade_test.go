package characters

import (
	"encoding/json"
	"flag"
	"os"
	"testing"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite schema/v1-to-v2.golden.json")

type goldenPair struct {
	Name string          `json:"name"`
	V1   json.RawMessage `json:"v1"`
	V2   json.RawMessage `json:"v2"`
}

func v1Doc(shape, eyes, mouth string, accessories ...string) CharacterSpec {
	if accessories == nil {
		accessories = []string{}
	}
	return CharacterSpec{
		SchemaVersion: 1, RigVersion: "original-v1",
		Info:        CharacterInfo{Name: "Seed", Bio: "A curious original star.", Backstory: ""},
		Personality: CharacterPersonality{Traits: []string{"curious"}, SpeakingStyle: "Gentle and clear", PersonaPrompt: "Encourage careful observation."},
		Appearance: CharacterAppearance{BodyShape: shape, Palette: Palette{Body: "#FFD23F", Accent: "#EE4266", Outline: "#1A1A24"},
			Eyes: eyes, Mouth: mouth, Accessories: accessories},
		Expressions: []string{"idle", "happy", "sad", "thinking", "celebrate"},
		VoiceKey:    "voice-provisional-guide",
	}
}

func goldenInputs() map[string]CharacterSpec {
	return map[string]CharacterSpec{
		"round slime, round eyes, smile":      v1Doc("round", "round", "smile"),
		"bean ghost, oval eyes, line mouth":   v1Doc("bean", "oval", "line", "scarf"),
		"star fairy, sleepy eyes, open mouth": v1Doc("star", "sleepy", "open", "badge", "hat"),
		"glasses become body glasses":         v1Doc("round", "round", "smile", "glasses", "scarf"),
	}
}

func TestUpgradeV1GoldenPairs(t *testing.T) {
	names := []string{"round slime, round eyes, smile", "bean ghost, oval eyes, line mouth", "star fairy, sleepy eyes, open mouth", "glasses become body glasses"}
	inputs := goldenInputs()
	if *updateGolden {
		var pairs []goldenPair
		for _, n := range names {
			in := inputs[n]
			v1, _ := json.Marshal(in)
			v2, _ := json.Marshal(UpgradeV1(&in))
			pairs = append(pairs, goldenPair{n, v1, v2})
		}
		out, _ := json.MarshalIndent(map[string]any{"pairs": pairs}, "", " ")
		if err := os.WriteFile("schema/v1-to-v2.golden.json", append(out, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile("schema/v1-to-v2.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Pairs []goldenPair `json:"pairs"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Pairs) != len(names) {
		t.Fatalf("pairs=%d", len(f.Pairs))
	}
	for _, p := range f.Pairs {
		t.Run(p.Name, func(t *testing.T) {
			r, err := ReadSpec(p.V1)
			if err != nil || !r.Upgraded || r.ReadOnly || r.SourceVersion != 1 {
				t.Fatalf("read: %+v %v", r, err)
			}
			got, _ := json.Marshal(r.Spec)
			var a, b any
			_ = json.Unmarshal(got, &a)
			_ = json.Unmarshal(p.V2, &b)
			ja, _ := json.Marshal(a)
			jb, _ := json.Marshal(b)
			if string(ja) != string(jb) {
				t.Fatalf("golden mismatch:\n%s\n%s", ja, jb)
			}
			if _, err := ValidateSpecV2JSON(p.V2); err != nil {
				t.Fatalf("golden v2 invalid: %v", err)
			}
		})
	}
}

// Every v1 body shape, eye, mouth and accessory subset upgrades to a valid v2 document.
func TestUpgradeV1IsExhaustivelyValid(t *testing.T) {
	subsets := [][]string{nil, {"glasses"}, {"scarf"}, {"hat"}, {"badge"}, {"glasses", "scarf"}, {"glasses", "hat"}, {"glasses", "badge"}, {"scarf", "hat"}, {"scarf", "badge"}, {"hat", "badge"}}
	n := 0
	for shape := range v1Species {
		for eyes := range v1Eyes {
			for _, mouth := range []string{"smile", "line", "open"} {
				for _, acc := range subsets {
					in := v1Doc(shape, eyes, mouth, acc...)
					if err := ValidateSpec(&in); err != nil {
						t.Fatal(err)
					}
					up := UpgradeV1(&in)
					raw, _ := json.Marshal(up)
					if _, err := ValidateSpecV2JSON(raw); err != nil {
						t.Fatalf("%s/%s/%s/%v: %v", shape, eyes, mouth, acc, err)
					}
					n++
				}
			}
		}
	}
	if n != 3*3*3*11 {
		t.Fatal(n)
	}
}

func TestReadSpecDegradesFutureVersionsAndIds(t *testing.T) {
	good := string(mustFixture(t, "valid human"))
	cases := map[string]string{
		"future schema":  `{"schema_version":3,"anything":true}`,
		"future catalog": `{"schema_version":2,"catalog_version":9,"anything":true}`,
	}
	for name, raw := range cases {
		r, err := ReadSpec([]byte(raw))
		if err != nil || !r.ReadOnly || r.Spec == nil || r.Spec.Body.Kind != "fantasy" {
			t.Fatalf("%s: %+v %v", name, r, err)
		}
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(good), &m)
	m["body"].(map[string]any)["face_shape"] = "hexagon"
	raw, _ := json.Marshal(m)
	r, err := ReadSpec(raw)
	if err != nil || !r.ReadOnly {
		t.Fatalf("unknown catalog id must degrade read-only: %+v %v", r, err)
	}
	if _, err := ReadSpec([]byte(`{"schema_version":2,"catalog_version":1}`)); err == nil {
		t.Fatal("malformed v2 must error")
	}
	if _, err := ReadSpec([]byte(`[]`)); err == nil {
		t.Fatal("non-object must error")
	}
	r, err = ReadSpec([]byte(good))
	if err != nil || r.ReadOnly || r.Upgraded || r.SourceVersion != 2 {
		t.Fatalf("%+v %v", r, err)
	}
}

func mustFixture(t *testing.T, name string) json.RawMessage {
	t.Helper()
	for _, c := range loadFixtures(t) {
		if c.Name == name {
			return c.Spec
		}
	}
	t.Fatalf("fixture %q missing", name)
	return nil
}
