// Package examples holds the authored example Courses every new learner can start for free.
//
// Content is Git-versioned JSON (content/<slug>-v<N>.json) in the exact shape of a generated
// Lesson (slides, test, retry pool). A published version is immutable: a content change is a new
// file with the next version, and the old file stays so learners pinned to it keep working.
// Nothing here calls a model, the database or the network.
package examples

import (
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

//go:embed content/*.json
var contentFS embed.FS

// Slide is one authored Motion HTML slide.
type Slide struct {
	ID         string   `json:"slide_id"`
	HTML       string   `json:"html"`
	Narration  string   `json:"narration"`
	DurationMS int      `json:"duration_ms"`
	TopicIDs   []string `json:"topic_ids"`
}

// Question is a test question or a retry variant.
type Question struct {
	ID          string   `json:"question_id"`
	TopicID     string   `json:"topic_id,omitempty"`
	VariantOf   string   `json:"variant_of,omitempty"`
	Prompt      string   `json:"prompt"`
	Choices     []string `json:"choices"`
	AnswerIndex int      `json:"answer_index"`
	Explanation string   `json:"explanation"`
}

// VariantPool is the pre-generated retry pool of one topic.
type VariantPool struct {
	TopicID  string     `json:"topic_id"`
	Variants []Question `json:"variants"`
}

// Lesson is one authored Lesson.
type Lesson struct {
	Key         string        `json:"key"`
	Title       string        `json:"title"`
	Goal        string        `json:"goal"`
	Slides      []Slide       `json:"slides"`
	Test        []Question    `json:"test"`
	VariantPool []VariantPool `json:"variant_pool"`
}

// Unit is an ordered group of Lessons.
type Unit struct {
	Title   string   `json:"title"`
	Lessons []Lesson `json:"lessons"`
}

// Course is one authored example Course at one version.
type Course struct {
	Slug        string `json:"slug"`
	Version     int    `json:"version"`
	Order       int    `json:"order"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Level       string `json:"level"`
	Units       []Unit `json:"units"`

	hash string
}

// Summary is the catalogue card of a Course (the GET /v1/courses/examples item).
type Summary struct {
	Slug             string `json:"slug"`
	Title            string `json:"title"`
	Description      string `json:"description"`
	Level            string `json:"level"`
	Units            int    `json:"units"`
	Lessons          int    `json:"lessons"`
	EstimatedMinutes int    `json:"estimated_minutes"`
}

// LessonCount is the number of Lessons across all units.
func (c Course) LessonCount() int {
	n := 0
	for _, u := range c.Units {
		n += len(u.Lessons)
	}
	return n
}

// EstimatedMinutes is slide time plus two minutes of reading and test per Lesson, rounded up to 5.
func (c Course) EstimatedMinutes() int {
	ms := 0
	for _, u := range c.Units {
		for _, l := range u.Lessons {
			for _, s := range l.Slides {
				ms += s.DurationMS
			}
		}
	}
	m := math.Ceil(float64(ms)/60000) + 2*float64(c.LessonCount())
	return int(math.Ceil(m/5) * 5)
}

// Summary describes the Course as a catalogue card.
func (c Course) Summary() Summary {
	return Summary{Slug: c.Slug, Title: c.Title, Description: c.Description, Level: c.Level, Units: len(c.Units), Lessons: c.LessonCount(), EstimatedMinutes: c.EstimatedMinutes()}
}

// ContentHash is the SHA-256 of the exact authored bytes of this version.
func (c Course) ContentHash() string { return c.hash }

type loaded struct {
	versions map[string]map[int]Course
	latest   []Course
}

var registry = mustLoad()

func mustLoad() loaded {
	entries, err := contentFS.ReadDir("content")
	if err != nil {
		panic(err)
	}
	out := loaded{versions: map[string]map[int]Course{}}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".snippets.json") {
			continue
		}
		raw, err := contentFS.ReadFile("content/" + e.Name())
		if err != nil {
			panic(err)
		}
		var c Course
		if err = json.Unmarshal(raw, &c); err != nil {
			panic(fmt.Sprintf("examples: %s: %v", e.Name(), err))
		}
		if e.Name() != fmt.Sprintf("%s-v%d.json", c.Slug, c.Version) {
			panic("examples: file name must be <slug>-v<version>.json: " + e.Name())
		}
		c.hash = fmt.Sprintf("%x", sha256.Sum256(raw))
		if out.versions[c.Slug] == nil {
			out.versions[c.Slug] = map[int]Course{}
		}
		out.versions[c.Slug][c.Version] = c
	}
	for _, byVersion := range out.versions {
		best := 0
		for v := range byVersion {
			best = max(best, v)
		}
		out.latest = append(out.latest, byVersion[best])
	}
	sort.Slice(out.latest, func(i, j int) bool {
		if out.latest[i].Order != out.latest[j].Order {
			return out.latest[i].Order < out.latest[j].Order
		}
		return out.latest[i].Slug < out.latest[j].Slug
	})
	return out
}

// Latest returns the newest version of every example Course in catalogue order.
func Latest() []Course { return append([]Course(nil), registry.latest...) }

// Get returns the newest version of one Course.
func Get(slug string) (Course, bool) {
	for _, c := range registry.latest {
		if c.Slug == slug {
			return c, true
		}
	}
	return Course{}, false
}

// AllVersions returns every authored version, oldest first per Course, in catalogue order.
func AllVersions() []Course {
	var out []Course
	for _, latest := range registry.latest {
		var vs []int
		for v := range registry.versions[latest.Slug] {
			vs = append(vs, v)
		}
		sort.Ints(vs)
		for _, v := range vs {
			out = append(out, registry.versions[latest.Slug][v])
		}
	}
	return out
}

// Slugs lists the example Course slugs in catalogue order (the closed analytics enum).
func Slugs() []string {
	out := make([]string, 0, len(registry.latest))
	for _, c := range registry.latest {
		out = append(out, c.Slug)
	}
	return out
}

// Catalogue is the learner-independent body of GET /v1/courses/examples.
func Catalogue() []Summary {
	out := make([]Summary, 0, len(registry.latest))
	for _, c := range registry.latest {
		out = append(out, c.Summary())
	}
	return out
}
