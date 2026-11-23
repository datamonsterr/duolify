package characters

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

var updateRigGolden = flag.Bool("update-rig-golden", false, "rewrite rig/golden.v2.json")

type rigGolden struct {
	Name   string `json:"name"`
	View   string `json:"view"`
	Pose   string `json:"pose"`
	Expr   string `json:"expression"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
	Alt    string `json:"alt,omitempty"`
}

func validFixtureSpecs(t *testing.T) (map[string]*SpecV2, []string) {
	t.Helper()
	out := map[string]*SpecV2{}
	var names []string
	for _, c := range loadFixtures(t) {
		if !c.Valid {
			continue
		}
		spec, err := ValidateSpecV2JSON(c.Spec)
		if err != nil {
			t.Fatal(err)
		}
		out[c.Name] = spec
		names = append(names, c.Name)
	}
	return out, names
}

func sum(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func budgetFor(view string) int {
	if view == ViewBody {
		return 24576
	}
	return 8192
}

func goldenCases(t *testing.T) []rigGolden {
	t.Helper()
	specs, names := validFixtureSpecs(t)
	var out []rigGolden
	for _, n := range names {
		spec := specs[n]
		add := func(view, pose, expr string) {
			svg, err := CompileV2(spec, view, pose, expr)
			if err != nil {
				t.Fatalf("%s %s/%s/%s: %v", n, view, pose, expr, err)
			}
			out = append(out, rigGolden{Name: n, View: view, Pose: pose, Expr: expr, Bytes: len(svg), SHA256: sum(svg)})
		}
		for _, e := range validExpressionsV2 {
			add(ViewAvatar, "idle", e)
			add(ViewBody, "talk", e)
		}
		for _, p := range validPoses {
			add(ViewBody, p, "idle")
		}
		out[len(out)-1].Alt = AltTextV2(spec)
	}
	return out
}

func TestRigV2MatchesSharedGolden(t *testing.T) {
	got := goldenCases(t)
	if *updateRigGolden {
		raw, _ := json.Marshal(map[string]any{"cases": got})
		if err := os.WriteFile("rig/golden.v2.json", append(raw, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile("rig/golden.v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var want struct {
		Cases []rigGolden `json:"cases"`
	}
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if len(want.Cases) != len(got) {
		t.Fatalf("golden has %d cases, got %d (run -update-rig-golden)", len(want.Cases), len(got))
	}
	for i := range got {
		if got[i] != want.Cases[i] {
			t.Fatalf("case %d (%s %s/%s/%s) changed: %+v vs %+v", i, got[i].Name, got[i].View, got[i].Pose, got[i].Expr, got[i], want.Cases[i])
		}
	}
}

func checkSVG(t *testing.T, label, svg string, budget int) {
	t.Helper()
	if len(svg) > budget {
		t.Fatalf("%s: %d bytes over budget %d", label, len(svg), budget)
	}
	for _, bad := range []string{"<script", "href", "<image", "<use", "<filter", "<animate", "<set ", "<foreignObject", "xlink", "style=", "javascript:", "@import"} {
		if strings.Contains(svg, bad) {
			t.Fatalf("%s: forbidden %q", label, bad)
		}
	}
	if strings.Count(svg, "url(") != strings.Count(svg, "url(#") {
		t.Fatalf("%s: external url()", label)
	}
	d := xml.NewDecoder(strings.NewReader(svg))
	for {
		_, err := d.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("%s: not well-formed XML: %v", label, err)
		}
	}
}

func TestRigV2IsDeterministicAndWithinBudget(t *testing.T) {
	specs, _ := validFixtureSpecs(t)
	for name, spec := range specs {
		for _, view := range []string{ViewAvatar, ViewBody} {
			a, err := CompileV2(spec, view, "wave", "happy")
			if err != nil {
				t.Fatal(name, err)
			}
			b, _ := CompileV2(spec, view, "wave", "happy")
			if a != b {
				t.Fatalf("%s %s not deterministic", name, view)
			}
			checkSVG(t, name+" "+view, a, budgetFor(view))
		}
	}
	if _, err := CompileV2(DefaultSpecV2(), "poster", "idle", "idle"); err == nil {
		t.Fatal("unknown view accepted")
	}
	if _, err := CompileV2(DefaultSpecV2(), ViewBody, "dance", "idle"); err == nil {
		t.Fatal("unknown pose accepted")
	}
	if _, err := CompileV2(DefaultSpecV2(), ViewBody, "idle", "angry"); err == nil {
		t.Fatal("unknown expression accepted")
	}
}

// enumVariants yields every enum slot of a document so each catalog id renders at least once.
func enumVariants(path []string, node any, schemaNode map[string]any, yield func(path []string, id string)) {
	if disc, ok := schemaNode["discriminator"].(string); ok {
		obj := node.(map[string]any)
		for _, b := range schemaNode["oneOf"].([]any) {
			branch := b.(map[string]any)
			if fmt.Sprint(branch["properties"].(map[string]any)[disc].(map[string]any)["const"]) == fmt.Sprint(obj[disc]) {
				enumVariants(path, node, branch, yield)
			}
		}
		return
	}
	switch schemaNode["type"] {
	case "string":
		if list, ok := schemaNode["enum"].([]any); ok {
			for _, id := range list {
				yield(path, id.(string))
			}
		}
	case "object":
		obj, _ := node.(map[string]any)
		props := schemaNode["properties"].(map[string]any)
		for _, k := range sortedKeys(props) {
			if child, ok := obj[k]; ok {
				enumVariants(append(append([]string{}, path...), k), child, props[k].(map[string]any), yield)
			}
		}
	case "array":
		if list, ok := node.([]any); ok {
			items := schemaNode["items"].(map[string]any)
			for i, item := range list {
				enumVariants(append(append([]string{}, path...), fmt.Sprint(i)), item, items, yield)
			}
		}
	}
}

func setPath(root map[string]any, path []string, id string) {
	var cur any = root
	for i, k := range path {
		last := i == len(path)-1
		switch c := cur.(type) {
		case map[string]any:
			if last {
				c[k] = id
				return
			}
			cur = c[k]
		case []any:
			var idx int
			_, _ = fmt.Sscan(k, &idx)
			if last {
				c[idx] = id
				return
			}
			cur = c[idx]
		}
	}
}

func TestRigV2RendersEveryCatalogId(t *testing.T) {
	var schema map[string]any
	_ = json.Unmarshal(SpecSchemaV2, &schema)
	rendered := 0
	for _, c := range loadFixtures(t) {
		if !c.Valid {
			continue
		}
		var base map[string]any
		_ = json.Unmarshal(c.Spec, &base)
		enumVariants(nil, base, schema, func(path []string, id string) {
			if path[len(path)-1] == "kind" {
				return
			}
			var clone map[string]any
			raw, _ := json.Marshal(base)
			_ = json.Unmarshal(raw, &clone)
			setPath(clone, path, id)
			raw, _ = json.Marshal(clone)
			spec, err := ValidateSpecV2JSON(raw)
			if err != nil {
				return // cross-field rules legitimately reject some combinations
			}
			for _, view := range []string{ViewAvatar, ViewBody} {
				for _, pose := range []string{"idle", "celebrate"} {
					label := strings.Join(path, ".") + "=" + id
					svg, err := CompileV2(spec, view, pose, "confused")
					if err != nil {
						t.Fatalf("%s: %v", label, err)
					}
					checkSVG(t, label, svg, budgetFor(view))
					rendered++
				}
			}
		})
	}
	if rendered < 1500 {
		t.Fatalf("only %d renders; enum walk broken", rendered)
	}
}

func TestRigV2PosesAndExpressionsDiffer(t *testing.T) {
	specs, _ := validFixtureSpecs(t)
	spec := specs["valid human"]
	seen := map[string]string{}
	for _, p := range validPoses {
		svg, _ := CompileV2(spec, ViewBody, p, "idle")
		if prev, dup := seen[svg]; dup {
			t.Fatalf("pose %s renders like %s", p, prev)
		}
		seen[svg] = p
	}
	seen = map[string]string{}
	for _, e := range validExpressionsV2 {
		svg, _ := CompileV2(spec, ViewAvatar, "idle", e)
		if prev, dup := seen[svg]; dup {
			t.Fatalf("expression %s renders like %s", e, prev)
		}
		seen[svg] = e
	}
}

func TestAltTextV2(t *testing.T) {
	specs, _ := validFixtureSpecs(t)
	got := AltTextV2(specs["valid human"])
	for _, want := range []string{"Maple", "adult human with medium skin tone", "brown eyes", "sweater", "pants"} {
		if !strings.Contains(got, want) {
			t.Fatalf("alt %q lacks %q", got, want)
		}
	}
	if strings.ContainsAny(got, "<>") {
		t.Fatal("alt must be plain text")
	}
}
