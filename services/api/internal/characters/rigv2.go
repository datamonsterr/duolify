package characters

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// PartsV2 is the original-v2 part library: pure data interpreted identically
// by this compiler and apps/client/src/features/characters/rig-v2.ts.
//
//go:embed rig/parts.v2.json
var PartsV2 []byte

// Rig view names and poses.
const (
	ViewAvatar = "avatar"
	ViewBody   = "body"
)

var validPoses = []string{"idle", "talk", "point", "think", "celebrate", "wave", "shrug"}

type partsTable struct {
	On  string                       `json:"on"`
	Map map[string]map[string]string `json:"map"`
}

type partsLet struct {
	Name  string `json:"name"`
	When  any    `json:"when"`
	Value string `json:"value"`
}

type partsView struct {
	ViewBox string `json:"viewBox"`
	Budget  int    `json:"budget"`
}

type partsLibrary struct {
	Version int                  `json:"version"`
	Views   map[string]partsView `json:"views"`
	Tables  []partsTable         `json:"tables"`
	Lets    []partsLet           `json:"lets"`
	Defs    map[string]any       `json:"defs"`
	Root    any                  `json:"root"`
}

var (
	partsOnce sync.Once
	parts     partsLibrary
	partsErr  error
)

func loadParts() (*partsLibrary, error) {
	partsOnce.Do(func() { partsErr = json.Unmarshal(PartsV2, &parts) })
	return &parts, partsErr
}

// flattenDoc exposes a document to templates as flat string variables:
// body.hair.style becomes hair_style, clothes.top.color becomes top_color,
// accessories become acc_<kind>, skin features feat_<name>.
func flattenDoc(spec *SpecV2) map[string]string {
	v := map[string]string{}
	var raw []byte
	raw, _ = json.Marshal(spec)
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	var walk func(prefix string, value any)
	walk = func(prefix string, value any) {
		switch t := value.(type) {
		case map[string]any:
			for k, child := range t {
				name := k
				if prefix != "" {
					name = prefix + "_" + k
				}
				walk(name, child)
			}
		case string:
			v[prefix] = t
		}
	}
	walk("", doc["body"])
	clothes, _ := doc["clothes"].(map[string]any)
	for k, child := range clothes {
		if k == "accessories" || k == "pattern" {
			continue
		}
		walk(k, child)
	}
	v["pattern"], _ = clothes["pattern"].(string)
	for _, kind := range []string{"scarf", "necklace", "badge", "backpack", "headphones", "bow_tie", "tie", "watch", "gloves", "hat"} {
		v["acc_"+kind] = ""
	}
	for _, a := range spec.Clothes.Accessories {
		v["acc_"+a.Kind] = a.Color
	}
	for _, f := range []string{"freckles", "rosy_cheeks", "vitiligo_patches", "birthmark"} {
		v["feat_"+f] = ""
	}
	for _, f := range spec.Body.SkinFeatures {
		v["feat_"+f] = "1"
	}
	return v
}

func isHexColor(s string) bool { return hexColorRegex.MatchString(s) }

// shade derives an edge (pct < 100) or highlight colour with integer maths only.
func shade(hex string, dark bool) string {
	r, g, b := HexToRGB(hex)
	f := func(c int) int {
		if dark {
			return c * 78 / 100
		}
		return c + (255-c)*40/100
	}
	return fmt.Sprintf("#%02X%02X%02X", f(r), f(g), f(b))
}

func condition(cond any, vars map[string]string) bool {
	m, ok := cond.(map[string]any)
	if !ok {
		return true
	}
	if list, ok := m["all"].([]any); ok {
		for _, c := range list {
			if !condition(c, vars) {
				return false
			}
		}
		return true
	}
	if list, ok := m["any"].([]any); ok {
		for _, c := range list {
			if condition(c, vars) {
				return true
			}
		}
		return false
	}
	name, _ := m["var"].(string)
	value := vars[name]
	contains := func(key string) (bool, bool) {
		list, ok := m[key].([]any)
		if !ok {
			return false, false
		}
		for _, item := range list {
			if s, _ := item.(string); s == value {
				return true, true
			}
		}
		return false, true
	}
	if in, present := contains("in"); present {
		return in
	}
	if in, present := contains("not_in"); present {
		return !in
	}
	return true
}

// substitute resolves {name} and {a+b-3} tokens; arithmetic is integer only.
func substitute(tpl string, vars map[string]string) (string, error) {
	if !strings.Contains(tpl, "{") {
		return tpl, nil
	}
	var out strings.Builder
	for {
		i := strings.IndexByte(tpl, '{')
		if i < 0 {
			out.WriteString(tpl)
			return out.String(), nil
		}
		j := strings.IndexByte(tpl[i:], '}')
		if j < 0 {
			return "", errors.New("rig: unterminated token")
		}
		out.WriteString(tpl[:i])
		value, err := evalToken(tpl[i+1:i+j], vars)
		if err != nil {
			return "", err
		}
		out.WriteString(value)
		tpl = tpl[i+j+1:]
	}
}

func evalToken(expr string, vars map[string]string) (string, error) {
	if !strings.ContainsAny(expr, "+-") {
		v, ok := vars[expr]
		if !ok {
			return "", fmt.Errorf("rig: unknown variable %q", expr)
		}
		return v, nil
	}
	total, sign, start := 0, 1, 0
	flush := func(end int) error {
		operand := expr[start:end]
		n, err := strconv.Atoi(operand)
		if err != nil {
			v, ok := vars[operand]
			if !ok {
				return fmt.Errorf("rig: unknown variable %q", operand)
			}
			if n, err = strconv.Atoi(v); err != nil {
				return fmt.Errorf("rig: variable %q is not an integer", operand)
			}
		}
		total += sign * n
		return nil
	}
	for i := 0; i < len(expr); i++ {
		if (expr[i] == '+' || expr[i] == '-') && i > 0 {
			if err := flush(i); err != nil {
				return "", err
			}
			sign, start = 1, i+1
			if expr[i] == '-' {
				sign = -1
			}
		}
	}
	if err := flush(len(expr)); err != nil {
		return "", err
	}
	return strconv.Itoa(total), nil
}

func renderNode(node any, lib *partsLibrary, vars map[string]string, out *strings.Builder, depth int) error {
	if depth > 32 {
		return errors.New("rig: nesting too deep")
	}
	switch n := node.(type) {
	case nil:
		return nil
	case string:
		s, err := substitute(n, vars)
		if err != nil {
			return err
		}
		out.WriteString(s)
	case []any:
		for _, child := range n {
			if err := renderNode(child, lib, vars, out, depth+1); err != nil {
				return err
			}
		}
	case map[string]any:
		if ref, ok := n["ref"].(string); ok {
			def, found := lib.Defs[ref]
			if !found {
				return fmt.Errorf("rig: unknown def %q", ref)
			}
			return renderNode(def, lib, vars, out, depth+1)
		}
		if name, ok := n["by"].(string); ok {
			cases, _ := n["cases"].(map[string]any)
			if chosen, found := cases[vars[name]]; found {
				return renderNode(chosen, lib, vars, out, depth+1)
			}
			return renderNode(n["default"], lib, vars, out, depth+1)
		}
		if cond, ok := n["when"]; ok {
			if condition(cond, vars) {
				return renderNode(n["then"], lib, vars, out, depth+1)
			}
			return renderNode(n["else"], lib, vars, out, depth+1)
		}
		return errors.New("rig: unknown node")
	default:
		return errors.New("rig: unsupported node type")
	}
	return nil
}

// RigVars computes the template variables for one compile; exported for tests.
func rigVars(lib *partsLibrary, spec *SpecV2, view, pose, expression string) (map[string]string, error) {
	vars := flattenDoc(spec)
	vars["view"], vars["pose"], vars["expression"] = view, pose, expression
	for _, t := range lib.Tables {
		if value, ok := vars[t.On]; ok {
			for k, v := range t.Map[value] {
				vars[k] = v
			}
		}
	}
	set := map[string]bool{}
	for _, l := range lib.Lets {
		if set[l.Name] || !condition(l.When, vars) {
			continue
		}
		value, err := substitute(l.Value, vars)
		if err != nil {
			return nil, err
		}
		vars[l.Name], set[l.Name] = value, true
	}
	names := make([]string, 0, len(vars))
	for k := range vars {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if isHexColor(vars[k]) {
			vars[k+"_dk"], vars[k+"_lt"] = shade(vars[k], true), shade(vars[k], false)
		}
	}
	// Per-compile id seed so several inline Characters never share clip ids.
	h := uint32(2166136261)
	for _, k := range names {
		for _, b := range []byte(k + "=" + vars[k] + ";") {
			h = (h ^ uint32(b)) * 16777619
		}
	}
	vars["uid"] = fmt.Sprintf("c%08x", h)
	return vars, nil
}

// CompileV2 compiles a validated v2 document to deterministic SVG. Output is
// byte-identical for the same document, view, pose and expression in Go and
// TypeScript; it contains no scripts, external references, filters or SMIL.
func CompileV2(spec *SpecV2, view, pose, expression string) (string, error) {
	if spec == nil {
		return "", errors.New("spec is nil")
	}
	lib, err := loadParts()
	if err != nil {
		return "", err
	}
	v, ok := lib.Views[view]
	if !ok {
		return "", fmt.Errorf("unsupported view: %s", view)
	}
	if !contains(validPoses, pose) {
		return "", fmt.Errorf("unsupported pose: %s", pose)
	}
	if !contains(validExpressionsV2, expression) {
		return "", fmt.Errorf("unsupported expression: %s", expression)
	}
	vars, err := rigVars(lib, spec, view, pose, expression)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="%s" class="character-rig" data-view="%s" data-pose="%s" data-expression="%s">`, v.ViewBox, view, pose, expression)
	if err := renderNode(lib.Root, lib, vars, &b, 0); err != nil {
		return "", err
	}
	b.WriteString("</svg>")
	if b.Len() > v.Budget {
		return "", fmt.Errorf("rig: %s render %d bytes exceeds budget %d", view, b.Len(), v.Budget)
	}
	return b.String(), nil
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}
