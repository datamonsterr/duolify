package accounts

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/jackc/pgx/v5/pgtype"
)

// RigPartSpec v1 allowlist. The data is mirrored from
// apps/client/src/components/mascot/rig-part-spec.v1.json; mascot_test.go fails if they drift.
type mascotSwatch struct{ Body, BodyEdge, Trim, Eye string }

var mascotSwatches = map[string]mascotSwatch{
	"teal":   {"#1A9B82", "#147A66", "#0F5D4E", "#DDF8F0"},
	"blue":   {"#2F8FE8", "#2271BD", "#1A5490", "#DDF1FF"},
	"violet": {"#8B5CF6", "#6D41D1", "#52309F", "#F0E7FF"},
	"rose":   {"#E5484D", "#B93337", "#8C2427", "#FFE6E4"},
	"orange": {"#D96A08", "#AE5506", "#824004", "#FFEBD6"},
	"pink":   {"#D6409F", "#AB3180", "#7F2360", "#FFE3F3"},
}

const (
	mascotVisor      = "#0E2606"
	mascotAccent     = "#FFC233"
	mascotMaxAccs    = 2
	mascotDefaultRaw = `{"mode":"default","accessories":[],"companion_visible":true}`
)

var (
	mascotAccessories = map[string]bool{"headphones": true, "bowtie": true, "sprout": true, "scarf": true, "badge": true}
)

type mascotPalette struct {
	Body     string `json:"body"`
	BodyEdge string `json:"bodyEdge"`
	Trim     string `json:"trim"`
	Visor    string `json:"visor"`
	Eye      string `json:"eye"`
	Accent   string `json:"accent"`
}

func (p mascotPalette) allowed() bool {
	for _, s := range mascotSwatches {
		if strings.EqualFold(p.Body, s.Body) && strings.EqualFold(p.BodyEdge, s.BodyEdge) &&
			strings.EqualFold(p.Trim, s.Trim) && strings.EqualFold(p.Eye, s.Eye) &&
			strings.EqualFold(p.Visor, mascotVisor) && strings.EqualFold(p.Accent, mascotAccent) {
			return true
		}
	}
	return false
}

// MascotPatch is a partial update; absent fields keep their stored value.
type MascotPatch struct {
	Mode             *string          `json:"mode"`
	Palette          *json.RawMessage `json:"palette"`
	Accessories      *[]string        `json:"accessories"`
	CharacterID      *json.RawMessage `json:"character_id"`
	CompanionVisible *bool            `json:"companion_visible"`
}

// mascotValue is the stored and returned object.
type mascotValue struct {
	Mode             string         `json:"mode"`
	Palette          *mascotPalette `json:"palette,omitempty"`
	Accessories      []string       `json:"accessories"`
	CharacterID      *string        `json:"character_id,omitempty"`
	CompanionVisible bool           `json:"companion_visible"`
}

// ParseMascotPatch rejects unknown fields and invalid values without echoing input.
func ParseMascotPatch(raw json.RawMessage) (MascotPatch, bool) {
	var p MascotPatch
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || string(bytes.TrimSpace(raw)) == "null" {
		return p, false
	}
	if p.Mode != nil && *p.Mode != "default" && *p.Mode != "character" {
		return p, false
	}
	if p.Accessories != nil {
		seen := map[string]bool{}
		if len(*p.Accessories) > mascotMaxAccs {
			return p, false
		}
		for _, a := range *p.Accessories {
			if !mascotAccessories[a] || seen[a] {
				return p, false
			}
			seen[a] = true
		}
	}
	if p.Palette != nil && string(*p.Palette) != "null" {
		var pal mascotPalette
		d := json.NewDecoder(bytes.NewReader(*p.Palette))
		d.DisallowUnknownFields()
		if d.Decode(&pal) != nil || !pal.allowed() {
			return p, false
		}
	}
	if p.CharacterID != nil && string(*p.CharacterID) != "null" {
		var id string
		if json.Unmarshal(*p.CharacterID, &id) != nil || !uuidPattern.MatchString(id) {
			return p, false
		}
	}
	return p, true
}

// MergeMascot applies a patch over the stored object and re-checks cross-field rules.
func MergeMascot(stored json.RawMessage, p MascotPatch) (mascotValue, bool) {
	v := mascotValue{Mode: "default", Accessories: []string{}, CompanionVisible: true}
	if len(stored) > 0 {
		_ = json.Unmarshal(stored, &v)
		if v.Accessories == nil {
			v.Accessories = []string{}
		}
	}
	if p.Mode != nil {
		v.Mode = *p.Mode
	}
	if p.Palette != nil {
		v.Palette = nil
		if string(*p.Palette) != "null" {
			var pal mascotPalette
			_ = json.Unmarshal(*p.Palette, &pal)
			v.Palette = &pal
		}
	}
	if p.Accessories != nil {
		v.Accessories = *p.Accessories
	}
	if p.CharacterID != nil {
		v.CharacterID = nil
		if string(*p.CharacterID) != "null" {
			var id string
			_ = json.Unmarshal(*p.CharacterID, &id)
			v.CharacterID = &id
		}
	}
	if p.CompanionVisible != nil {
		v.CompanionVisible = *p.CompanionVisible
	}
	if v.Mode == "character" && v.CharacterID == nil {
		return v, false
	}
	if v.Mode == "default" {
		v.CharacterID = nil
	}
	return v, true
}

// MascotCharacterUsable reports whether the Learner owns an active (not deleted) Character.
func MascotCharacterUsable(ctx context.Context, q db.DBTX, learnerID pgtype.UUID, characterID string) bool {
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM characters WHERE id=$1::uuid AND learner_id=$2::uuid AND deleted_at IS NULL)`, characterID, learnerID).Scan(&ok)
	return err == nil && ok
}

// ResolveMascot returns the stored object, falling back to built-in Tinko when the chosen
// Character is deleted or unavailable. The stored value is left untouched.
func ResolveMascot(ctx context.Context, q db.DBTX, learnerID pgtype.UUID, stored json.RawMessage) json.RawMessage {
	v, ok := MergeMascot(stored, MascotPatch{})
	if !ok || v.Mode == "character" && (v.CharacterID == nil || !MascotCharacterUsable(ctx, q, learnerID, *v.CharacterID)) {
		v = mascotValue{Mode: "default", Palette: v.Palette, Accessories: v.Accessories, CompanionVisible: v.CompanionVisible}
		if v.Accessories == nil {
			v.Accessories = []string{}
		}
	}
	out, _ := json.Marshal(v)
	return out
}

// MascotSwatchCount and MascotAccessoryCount let the parity test detect allowlist drift.
func MascotSwatchCount() int    { return len(mascotSwatches) }
func MascotAccessoryCount() int { return len(mascotAccessories) }
