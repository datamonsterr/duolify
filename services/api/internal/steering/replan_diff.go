package steering

import (
	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"google.golang.org/protobuf/proto"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ReplanDiff describes a lesson change in its destination (source for removals).
type ReplanDiff struct {
	Kind       string `json:"kind"`
	LessonID   string `json:"lesson_id,omitempty"`
	UnitID     string `json:"unit_id"`
	UnitTitle  string `json:"unit_title"`
	Before     string `json:"before,omitempty"`
	After      string `json:"after,omitempty"`
	FromUnitID string `json:"from_unit_id,omitempty"`
	Position   int    `json:"position"`
}

var replanID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

type outlineLocation struct {
	unit, lesson int
	u            *pb.Unit
	l            *pb.LessonRef
}

func outlineLocations(o *pb.CourseOutline) (map[string]outlineLocation, bool) {
	loc := map[string]outlineLocation{}
	seen := map[string]bool{}
	text := func(s string, n int) bool {
		return utf8.ValidString(s) && strings.TrimSpace(s) != "" && utf8.RuneCountInString(s) <= n && !strings.ContainsRune(s, 0)
	}
	id := func(s string) bool {
		if !replanID.MatchString(s) || seen[s] {
			return false
		}
		seen[s] = true
		return true
	}
	if o == nil || len(o.Units) == 0 || len(o.Units) > 10 {
		return nil, false
	}
	for i, u := range o.Units {
		if u == nil || !id(u.UnitId) || !text(u.Title, 500) || len(u.Lessons) == 0 || len(u.Lessons) > 20 {
			return nil, false
		}
		for j, l := range u.Lessons {
			if l == nil || !id(l.LessonId) || !text(l.Title, 500) || !text(l.Goal, 4000) {
				return nil, false
			}
			loc[l.LessonId] = outlineLocation{i, j, u, l}
		}
	}
	return loc, true
}

// ValidateReplan independently checks schema and every protected lesson's fields, parent and position.
// Generated contents are never written by this flow. Protected unit headings/positions also stay fixed.
func ValidateReplan(base, out *pb.CourseOutline, protected []string) ([]ReplanDiff, error) {
	old, valid := outlineLocations(base)
	if !valid {
		return nil, refusal("invalid_proposal")
	}
	next, valid := outlineLocations(out)
	if !valid {
		return nil, refusal("invalid_proposal")
	}
	if base.CourseId != out.CourseId || base.Title != out.Title || base.Description != out.Description {
		return nil, refusal("protected_lesson_changed")
	}
	for _, id := range protected {
		a, ok := old[id]
		b, found := next[id]
		if !ok || !found || !proto.Equal(a.l, b.l) || a.unit != b.unit || a.lesson != b.lesson || a.u.UnitId != b.u.UnitId || a.u.Title != b.u.Title {
			return nil, refusal("protected_lesson_changed")
		}
	}
	diffs := []ReplanDiff{}
	// Iterate the authoritative order, never a Go map, to keep the persisted diff stable.
	for _, u := range base.Units {
		for _, l := range u.Lessons {
			a := old[l.LessonId]
			b, ok := next[l.LessonId]
			d := ReplanDiff{LessonID: l.LessonId, UnitID: u.UnitId, UnitTitle: u.Title, Position: a.lesson, Before: l.Title}
			if !ok {
				d.Kind = "removed"
				diffs = append(diffs, d)
				continue
			}
			d.UnitID = b.u.UnitId
			d.UnitTitle = b.u.Title
			d.Position = b.lesson
			d.After = b.l.Title
			if a.u.UnitId != b.u.UnitId || a.unit != b.unit || a.lesson != b.lesson {
				d.Kind = "moved"
				d.FromUnitID = a.u.UnitId
				diffs = append(diffs, d)
			}
			if l.Title != b.l.Title || l.Goal != b.l.Goal {
				d.Kind = "retitled"
				diffs = append(diffs, d)
			}
		}
	}
	for _, u := range out.Units {
		for j, l := range u.Lessons {
			if _, ok := old[l.LessonId]; !ok {
				diffs = append(diffs, ReplanDiff{Kind: "added", LessonID: l.LessonId, UnitID: u.UnitId, UnitTitle: u.Title, Position: j, After: l.Title})
			}
		}
	}
	// Unit-only heading changes must also be visible in review.
	for _, u := range out.Units {
		for _, a := range base.Units {
			if a.UnitId == u.UnitId && a.Title != u.Title {
				diffs = append(diffs, ReplanDiff{Kind: "retitled", UnitID: u.UnitId, UnitTitle: u.Title, Before: a.Title, After: u.Title})
			}
		}
	}
	if len(diffs) == 0 {
		return nil, refusal("invalid_proposal")
	}
	return diffs, nil
}
