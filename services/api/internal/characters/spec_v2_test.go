package characters

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type fixtureCase struct {
	Name   string          `json:"name"`
	Valid  bool            `json:"valid"`
	Reason string          `json:"reason"`
	Path   string          `json:"path"`
	Spec   json.RawMessage `json:"spec"`
}

func loadFixtures(t *testing.T) []fixtureCase {
	t.Helper()
	raw, err := os.ReadFile("schema/v2.fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Cases []fixtureCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f.Cases
}

func TestSpecV2SharedFixtures(t *testing.T) {
	for _, c := range loadFixtures(t) {
		t.Run(c.Name, func(t *testing.T) {
			spec, err := ValidateSpecV2JSON(c.Spec)
			if c.Valid {
				if err != nil {
					t.Fatalf("want valid, got %v", err)
				}
				// A valid document round-trips through the typed struct unchanged.
				again, err := json.Marshal(spec)
				if err != nil {
					t.Fatal(err)
				}
				var a, b any
				_ = json.Unmarshal(c.Spec, &a)
				_ = json.Unmarshal(again, &b)
				ja, _ := json.Marshal(a)
				jb, _ := json.Marshal(b)
				if string(ja) != string(jb) {
					t.Fatalf("round trip changed document:\n%s\n%s", ja, jb)
				}
				return
			}
			se, ok := err.(*SpecError)
			if !ok {
				t.Fatalf("want SpecError %s, got %v", c.Reason, err)
			}
			if se.Code != c.Reason || se.Path != c.Path {
				t.Fatalf("want %s at %q, got %s at %q", c.Reason, c.Path, se.Code, se.Path)
			}
		})
	}
}

func TestSpecV2RejectsOversizedAndTrailingJSON(t *testing.T) {
	big := `{"schema_version":2,"pad":"` + strings.Repeat("x", 9000) + `"}`
	if _, err := ValidateSpecV2JSON([]byte(big)); err == nil || err.Error() != "spec_too_large" {
		t.Fatalf("got %v", err)
	}
	if _, err := ValidateSpecV2JSON([]byte(`{} {}`)); err == nil || err.Error() != "spec_not_object" {
		t.Fatalf("got %v", err)
	}
}

func TestSpecV2SchemaIsValidJSON(t *testing.T) {
	var v map[string]any
	if err := json.Unmarshal(SpecSchemaV2, &v); err != nil || v["$id"] != "duolify/character/v2" {
		t.Fatal("schema/v2.json is not the v2 schema", err)
	}
}
