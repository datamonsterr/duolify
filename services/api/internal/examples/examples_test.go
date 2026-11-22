package examples_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/examples"
	"github.com/datamonsterr/duolify/services/api/internal/lessontest"
	"github.com/datamonsterr/duolify/services/api/internal/player"
)

const origin = "https://assets.duolify.test"

// Mirrors engine-core slide::validate (DANGEROUS) and lessontest::validate_narration/validate_variant:
// the engine is Rust and not callable from Go, so the rules are repeated here and the Rust validator
// is run against the same files by `mise run engine:test` tooling (see openspec/changes/example-courses/design.md).
var (
	dangerous  = regexp.MustCompile(`(?i)(?:&(?:#|[a-z]+;)|\\|\b(?:fetch|websocket|eventsource|xmlhttprequest|sendbeacon|importscripts)\b|@import|\b(?:ftp|wss?|data|javascript):|<\s*(?:iframe|object|embed|base)\b)`)
	comment    = regexp.MustCompile(`(?s)<!--.*?-->`)
	rootEl     = regexp.MustCompile(`(?i)<[a-z][a-z0-9-]*\b[^>]*\bid\s*=\s*(?:"root"|'root'|root(?:\s|>))`)
	externalRe = regexp.MustCompile(`(?i)(?:https?:)?//[^\s"'<>)}]+`)
	tagRe      = regexp.MustCompile(`<[^>]+>`)
)

func TestAuthoredCatalogueShape(t *testing.T) {
	latest := examples.Latest()
	if len(latest) != 2 || latest[0].Slug != "product-management-foundations" || latest[1].Slug != "javascript-essentials" {
		t.Fatalf("catalogue: %+v", examples.Catalogue())
	}
	for _, c := range examples.AllVersions() {
		if c.Level != "beginner" || len(c.Units) != 2 || c.LessonCount() != 6 {
			t.Fatalf("%s v%d: want 2 units x 3 lessons beginner, got %d units %d lessons", c.Slug, c.Version, len(c.Units), c.LessonCount())
		}
		for _, u := range c.Units {
			if len(u.Lessons) != 3 {
				t.Fatalf("%s: unit %q has %d lessons", c.Slug, u.Title, len(u.Lessons))
			}
		}
		if c.EstimatedMinutes() < 10 || c.EstimatedMinutes() > 60 || strings.TrimSpace(c.Description) == "" || len(c.Description) > 160 {
			t.Fatalf("%s: estimate %d or description %q out of range", c.Slug, c.EstimatedMinutes(), c.Description)
		}
	}
}

func TestEveryAuthoredLessonPassesProductionValidators(t *testing.T) {
	for _, c := range examples.AllVersions() {
		keys := map[string]bool{}
		for _, u := range c.Units {
			for _, l := range u.Lessons {
				name := fmt.Sprintf("%s/v%d/%s", c.Slug, c.Version, l.Key)
				if l.Key == "" || keys[l.Key] || strings.TrimSpace(l.Title) == "" || strings.TrimSpace(l.Goal) == "" {
					t.Fatalf("%s: key/title/goal", name)
				}
				keys[l.Key] = true
				// The same validator a generated lesson passes before it is stored.
				if err := lessontest.ValidateGeneratedLesson(l.Proto()); err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				if n := len(l.Slides); n < 3 || n > 5 {
					t.Fatalf("%s: %d slides, want 3 to 5", name, n)
				}
				topics := map[string]bool{}
				ids := map[string]bool{}
				for _, s := range l.Slides {
					checkSlide(t, name, s)
					if ids[s.ID] {
						t.Fatalf("%s: duplicate slide id %s", name, s.ID)
					}
					ids[s.ID] = true
					for _, tp := range s.TopicIDs {
						topics[tp] = true
					}
				}
				checkTest(t, name, l, topics)
			}
		}
	}
}

func checkSlide(t *testing.T, name string, s examples.Slide) {
	t.Helper()
	if err := player.ValidateHTML(s.HTML, origin); err != nil {
		t.Fatalf("%s/%s: player markup: %v", name, s.ID, err)
	}
	stripped := comment.ReplaceAllString(s.HTML, "")
	if !rootEl.MatchString(stripped) || dangerous.MatchString(stripped) || len(s.HTML) > 256*1024 {
		t.Fatalf("%s/%s: engine slide rules (root element, entities, backslashes, network words)", name, s.ID)
	}
	if external := externalRe.FindString(stripped); external != "" {
		t.Fatalf("%s/%s: external URL %q", name, s.ID, external)
	}
	if strings.Contains(strings.ToLower(s.HTML), "<script") {
		t.Fatalf("%s/%s: script", name, s.ID)
	}
	if s.DurationMS < 3000 || s.DurationMS > 60000 {
		t.Fatalf("%s/%s: duration %d", name, s.ID, s.DurationMS)
	}
	words := len(strings.Fields(s.Narration))
	if words == 0 || tagRe.MatchString(s.Narration) || s.DurationMS < 400*words {
		t.Fatalf("%s/%s: narration of %d words needs markup-free text and 400 ms per word, has %d ms", name, s.ID, words, s.DurationMS)
	}
	if len(s.TopicIDs) == 0 || len(s.TopicIDs) > 2 {
		t.Fatalf("%s/%s: topic ids", name, s.ID)
	}
}

func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r == ' ' || r == '\t' || r == '\n' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || r > 127 {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func checkTest(t *testing.T, name string, l examples.Lesson, topics map[string]bool) {
	t.Helper()
	if len(l.Test) < 2 || len(l.Test) > 4 {
		t.Fatalf("%s: %d test questions, want 2 to 4", name, len(l.Test))
	}
	pools := map[string][]examples.Question{}
	for _, p := range l.VariantPool {
		pools[p.TopicID] = p.Variants
	}
	for _, q := range l.Test {
		if !topics[q.TopicID] {
			t.Fatalf("%s/%s: topic %s not taught", name, q.ID, q.TopicID)
		}
		if len(q.Choices) < 3 || len(q.Choices) > 4 {
			t.Fatalf("%s/%s: %d choices", name, q.ID, len(q.Choices))
		}
		variants := pools[q.TopicID]
		if len(variants) < 2 {
			t.Fatalf("%s/%s: retry pool for %s has %d variants, want at least 2", name, q.ID, q.TopicID, len(variants))
		}
		seen := map[string]bool{strings.ToLower(strings.TrimSpace(q.Prompt)): true}
		for _, v := range variants {
			p := strings.ToLower(strings.TrimSpace(v.Prompt))
			if seen[p] || strings.Contains(p, "(rephrased)") || strings.HasPrefix(p, "in other words:") || v.VariantOf != q.ID || v.ID == "" || v.ID == q.ID {
				t.Fatalf("%s/%s: variant %s must be a distinct paraphrase with variant_of=%s", name, q.ID, v.ID, q.ID)
			}
			seen[p] = true
			if v.TopicID != q.TopicID { // the engine contract links a variant to its topic
				t.Fatalf("%s/%s: variant %s needs topic_id %s", name, q.ID, v.ID, q.TopicID)
			}
			if len(v.Choices) != len(q.Choices) || v.AnswerIndex >= len(v.Choices) || strings.TrimSpace(v.Explanation) == "" {
				t.Fatalf("%s/%s: variant %s choices", name, q.ID, v.ID)
			}
			same := true
			for i := range v.Choices {
				same = same && v.Choices[i] == q.Choices[i]
			}
			if same {
				t.Fatalf("%s/%s: variant %s keeps the source choice order", name, q.ID, v.ID)
			}
			want := q.Choices[q.AnswerIndex]
			if normalize(v.Choices[v.AnswerIndex]) != normalize(want) && v.Choices[v.AnswerIndex] != want {
				t.Fatalf("%s/%s: variant %s correct answer %q is not %q", name, q.ID, v.ID, v.Choices[v.AnswerIndex], want)
			}
			have := map[string]bool{}
			for _, c := range q.Choices {
				have[c] = true
			}
			for _, c := range v.Choices {
				if !have[c] {
					t.Fatalf("%s/%s: variant %s introduces choice %q; variants reuse the source choice texts", name, q.ID, v.ID, c)
				}
			}
		}
	}
}
