package accounts_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
)

const blue = `{"body":"#2F8FE8","bodyEdge":"#2271BD","trim":"#1A5490","visor":"#0E2606","eye":"#DDF1FF","accent":"#FFC233"}`

func TestMascotPatchValidation(t *testing.T) {
	for _, tc := range []struct {
		body  string
		valid bool
	}{
		{`{"companion_visible":false}`, true},
		{`{"mode":"default","palette":` + blue + `,"accessories":["bowtie","badge"]}`, true},
		{`{"palette":null}`, true},
		{`{"accessories":[]}`, true},
		{`{"mode":"character","character_id":"3b0f5f3e-7a4c-4b8e-9b5f-2f1d0c4e8a11"}`, true},
		{`{"mode":"owl"}`, false},
		{`{"accessories":["bowtie","badge","scarf"]}`, false},
		{`{"accessories":["bowtie","bowtie"]}`, false},
		{`{"accessories":["crown"]}`, false},
		{`{"palette":{"body":"#123456","bodyEdge":"#2271BD","trim":"#1A5490","visor":"#0E2606","eye":"#DDF1FF","accent":"#FFC233"}}`, false},
		{`{"palette":{"body":"#2F8FE8"}}`, false},
		{`{"character_id":"not-a-uuid"}`, false},
		{`{"extra":1}`, false},
		{`{"companion_visible":"yes"}`, false},
	} {
		if _, ok := accounts.ParseMascotPatch(json.RawMessage(tc.body)); ok != tc.valid {
			t.Errorf("%s: got %v", tc.body, ok)
		}
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal([]byte(`{"mascot":null}`), &fields)
	if _, failures := accounts.ValidateSettingsPatch(fields, 2000); len(failures) == 0 {
		t.Error("null mascot must be rejected")
	}
}

func TestMascotMergePreservesOmittedFields(t *testing.T) {
	stored := json.RawMessage(`{"mode":"default","palette":` + blue + `,"accessories":["bowtie"],"companion_visible":true}`)
	patch, _ := accounts.ParseMascotPatch(json.RawMessage(`{"companion_visible":false}`))
	merged, ok := accounts.MergeMascot(stored, patch)
	out, _ := json.Marshal(merged)
	if !ok || !strings.Contains(string(out), `"bowtie"`) || !strings.Contains(string(out), `"#2F8FE8"`) || !strings.Contains(string(out), `"companion_visible":false`) {
		t.Fatalf("%s", out)
	}
	if _, ok := accounts.MergeMascot(nil, accounts.MascotPatch{Mode: ptr("character")}); ok {
		t.Fatal("character mode needs a character id")
	}
}

func ptr[T any](v T) *T { return &v }

// The Go allowlist mirrors the client's rig-part-spec.v1.json; both must change together.
func TestMascotAllowlistMatchesClientSpec(t *testing.T) {
	raw, err := os.ReadFile("../../../../apps/client/src/components/mascot/rig-part-spec.v1.json")
	if err != nil {
		t.Skip("client spec not present")
	}
	var spec struct {
		Swatches    map[string]map[string]string `json:"swatches"`
		Visor       []string                     `json:"visor"`
		Accent      []string                     `json:"accent"`
		Accessories []string                     `json:"accessories"`
		Max         int                          `json:"maxAccessories"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	for id, s := range spec.Swatches {
		p := `{"body":"` + s["body"] + `","bodyEdge":"` + s["bodyEdge"] + `","trim":"` + s["trim"] + `","visor":"` + spec.Visor[0] + `","eye":"` + s["eye"] + `","accent":"` + spec.Accent[0] + `"}`
		if _, ok := accounts.ParseMascotPatch(json.RawMessage(`{"palette":` + p + `}`)); !ok {
			t.Errorf("swatch %s rejected by Go allowlist", id)
		}
	}
	if len(spec.Swatches) != accounts.MascotSwatchCount() || spec.Max != 2 {
		t.Errorf("swatch count or max drifted: %d", len(spec.Swatches))
	}
	for _, a := range spec.Accessories {
		if _, ok := accounts.ParseMascotPatch(json.RawMessage(`{"accessories":["` + a + `"]}`)); !ok {
			t.Errorf("accessory %s rejected", a)
		}
	}
	if accounts.MascotAccessoryCount() != len(spec.Accessories) {
		t.Error("accessory list drifted")
	}
}
