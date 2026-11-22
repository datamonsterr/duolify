package examples_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/examples"
	"golang.org/x/net/html"
)

// snippet is one JavaScript example from the course, with the output Node really prints.
// Refs are "<lesson key>:<slide id | question id | variant id>"; a slide must contain the code
// text and a question or variant must contain the code in its prompt and have a correct choice equal
// to the output (lines joined with ", ").
type snippet struct {
	ID     string   `json:"id"`
	Code   string   `json:"code"`
	Stdout string   `json:"stdout"`
	Refs   []string `json:"refs"`
}

func textOf(s string) string {
	root, err := html.Parse(strings.NewReader(s))
	if err != nil {
		return s
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteString("\n")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return b.String()
}

func TestJavaScriptSnippetsAreVerifiedWithNode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("node is required to verify the JavaScript course (mise provides it)")
	}
	course, ok := examples.Get("javascript-essentials")
	if !ok {
		t.Fatal("javascript-essentials missing")
	}
	raw, err := os.ReadFile("content/javascript-essentials-v1.snippets.json")
	if err != nil {
		t.Fatal(err)
	}
	var snippets []snippet
	if err = json.Unmarshal(raw, &snippets); err != nil || len(snippets) < 15 {
		t.Fatalf("snippets: %v (%d)", err, len(snippets))
	}
	slides := map[string]string{}
	questions := map[string]examples.Question{}
	for _, u := range course.Units {
		for _, l := range u.Lessons {
			for _, s := range l.Slides {
				slides[l.Key+":"+s.ID] = textOf(s.HTML)
			}
			for _, q := range l.Test {
				questions[l.Key+":"+q.ID] = q
			}
			for _, p := range l.VariantPool {
				for _, v := range p.Variants {
					questions[l.Key+":"+v.ID] = v
				}
			}
		}
	}
	covered := map[string]bool{}
	for _, sn := range snippets {
		runCtx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		cmd := exec.CommandContext(runCtx, node, "--input-type=commonjs", "-") //nolint:gosec // node from PATH runs repository-authored snippets
		cmd.Stdin = strings.NewReader(sn.Code)
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		err = cmd.Run()
		cancel()
		if err != nil {
			t.Fatalf("%s: node failed: %v\n%s", sn.ID, err, errb.String())
		}
		got := strings.Join(strings.Split(strings.TrimRight(out.String(), "\n"), "\n"), ", ")
		if got != sn.Stdout {
			t.Fatalf("%s: node printed %q, fixture says %q", sn.ID, got, sn.Stdout)
		}
		if len(sn.Refs) == 0 {
			t.Fatalf("%s: no refs", sn.ID)
		}
		for _, ref := range sn.Refs {
			covered[ref] = true
			if text, isSlide := slides[ref]; isSlide {
				if !strings.Contains(text, strings.TrimSpace(sn.Code)) && !strings.Contains(strings.Join(strings.Fields(text), " "), strings.Join(strings.Fields(sn.Code), " ")) {
					t.Fatalf("%s: slide %s does not show the verified code", sn.ID, ref)
				}
				continue
			}
			q, isQuestion := questions[ref]
			if !isQuestion {
				t.Fatalf("%s: unknown ref %s", sn.ID, ref)
			}
			if !strings.Contains(strings.Join(strings.Fields(q.Prompt), " "), strings.Join(strings.Fields(sn.Code), " ")) {
				t.Fatalf("%s: question %s prompt does not contain the verified code", sn.ID, ref)
			}
			if strings.TrimSpace(q.Choices[q.AnswerIndex]) != sn.Stdout {
				t.Fatalf("%s: question %s marks %q correct, node printed %q", sn.ID, ref, q.Choices[q.AnswerIndex], sn.Stdout)
			}
		}
	}
	// Every question that asks what code prints must be backed by a verified snippet.
	for ref, q := range questions {
		if strings.Contains(q.Prompt, "console.log") && !covered[ref] {
			t.Fatalf("question %s shows console.log but has no node-verified snippet", ref)
		}
	}
}
