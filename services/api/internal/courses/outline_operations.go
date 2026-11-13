package courses

import (
	"encoding/json"
	"errors"
	"slices"
)

// OutlineOperation edits only an explicitly identified node, never a replacement outline.
type OutlineOperation struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	ParentID string `json:"parent_id,omitempty"`
	Position int    `json:"position,omitempty"`
	Title    string `json:"title,omitempty"`
	Goal     string `json:"goal,omitempty"`
}

// ApplyOutlineOperations preserves all unmentioned authored fields and relative order.
func ApplyOutlineOperations(base Outline, operations []OutlineOperation) (Outline, error) {
	invalid := errors.New("invalid outline operations")
	if base.Validate(false) != nil || len(operations) == 0 || len(operations) > 100 {
		return nil, invalid
	}
	raw, _ := json.Marshal(base)
	var out Outline
	_ = json.Unmarshal(raw, &out)
	used := map[string]bool{}
	seen := map[string]bool{}
	for _, u := range base {
		used[u.ID] = true
		for _, l := range u.Lessons {
			used[l.ID] = true
		}
	}
	locate := func(id string) (int, int) {
		for i, u := range out {
			if u.ID == id {
				return i, -1
			}
			for j, l := range u.Lessons {
				if l.ID == id {
					return i, j
				}
			}
		}
		return -1, -1
	}
	for _, op := range operations {
		if !outlineID.MatchString(op.ID) || seen[op.Kind+":"+op.ID] {
			return nil, invalid
		}
		seen[op.Kind+":"+op.ID] = true
		i, j := locate(op.ID)
		parent, child := locate(op.ParentID)
		switch op.Kind {
		case "added":
			if used[op.ID] || op.Position < 0 {
				return nil, invalid
			}
			used[op.ID] = true
			if op.ParentID == "" {
				if op.Goal != "" || op.Position > len(out) {
					return nil, invalid
				}
				out = slices.Insert(out, op.Position, OutlineUnit{ID: op.ID, Title: op.Title, Lessons: []OutlineLesson{}})
			} else {
				if parent < 0 || child != -1 || op.Position > len(out[parent].Lessons) {
					return nil, invalid
				}
				out[parent].Lessons = slices.Insert(out[parent].Lessons, op.Position, OutlineLesson{ID: op.ID, Title: op.Title, Goal: op.Goal})
			}
		case "removed":
			if i < 0 || op.ParentID != "" || op.Position != 0 || op.Title != "" || op.Goal != "" {
				return nil, invalid
			}
			if j < 0 {
				out = slices.Delete(out, i, i+1)
			} else {
				out[i].Lessons = slices.Delete(out[i].Lessons, j, j+1)
			}
		case "retitled":
			if i < 0 || op.ParentID != "" || op.Position != 0 || op.Goal != "" {
				return nil, invalid
			}
			if j < 0 {
				out[i].Title = op.Title
			} else {
				out[i].Lessons[j].Title = op.Title
			}
		case "moved":
			if i < 0 || op.Position < 0 || op.Title != "" || op.Goal != "" {
				return nil, invalid
			}
			if j < 0 {
				if op.ParentID != "" || op.Position >= len(out) {
					return nil, invalid
				}
				u := out[i]
				out = slices.Delete(out, i, i+1)
				out = slices.Insert(out, op.Position, u)
			} else {
				if parent < 0 || child != -1 {
					return nil, invalid
				}
				l := out[i].Lessons[j]
				out[i].Lessons = slices.Delete(out[i].Lessons, j, j+1)
				if op.Position > len(out[parent].Lessons) {
					return nil, invalid
				}
				out[parent].Lessons = slices.Insert(out[parent].Lessons, op.Position, l)
			}
		default:
			return nil, invalid
		}
		if out.Validate(false) != nil {
			return nil, invalid
		}
	}
	if out.Hash() == base.Hash() {
		return nil, invalid
	}
	return out, nil
}
