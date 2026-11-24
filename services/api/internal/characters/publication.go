package characters

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RigAsset is a content-addressed immutable compiler output. No library link is exported.
type RigAsset struct {
	ObjectKey string `json:"object_key"`
	SHA256    string `json:"sha256"`
}

// ErrPublishedCast refuses overrides until the existing confirmed steering fork.
var ErrPublishedCast = errors.New("published_cast_locked")

func contentIdentity(spec CharacterSpec, doc *SpecV2, schema int, rig, voice string) string {
	if doc != nil {
		// v1-only snapshots keep their identity; a document extends it.
		raw, _ := json.Marshal(struct {
			Spec       CharacterSpec
			Document   *SpecV2
			Schema     int
			Rig, Voice string
		}{spec, doc, schema, rig, voice})
		h := sha256.Sum256(raw)
		return "snapshot-" + hex.EncodeToString(h[:])
	}
	raw, _ := json.Marshal(struct {
		Spec       CharacterSpec
		Schema     int
		Rig, Voice string
	}{spec, schema, rig, voice})
	h := sha256.Sum256(raw)
	return "snapshot-" + hex.EncodeToString(h[:])
}

// ExportCast is an explicit content-only projection, detached from private identity/version records.
// Version metadata and all spec content participate in each independent snapshot identity.
func ExportCast(in LessonCastSnapshot) (LessonCastSnapshot, error) {
	out := LessonCastSnapshot{CastVersion: 1, CastSource: "published", SchemaVersion: in.SchemaVersion, RigVersion: in.RigVersion, VoiceConfigVersion: in.VoiceConfigVersion, Speakers: map[string]PinnedSpeaker{}, FallbackUsed: in.FallbackUsed}
	if in.SchemaVersion != 1 || in.RigVersion != "original-v1" || in.VoiceConfigVersion == "" || len(in.VoiceConfigVersion) > 100 || len(in.Speakers) < 1 || len(in.Speakers) > 3 {
		return out, errors.New("invalid_spec")
	}
	if _, ok := in.Speakers["guide"]; !ok {
		return out, errors.New("invalid_cast")
	}
	for key, speaker := range in.Speakers {
		if !ValidTutorSpeaker(key) || key == "" || speaker.SpeakerKey != key || speaker.Role != map[bool]string{true: "guide", false: "supporter"}[key == "guide"] || speaker.Suppressed {
			return out, errors.New("invalid_cast")
		}
		if err := ValidateSpec(&speaker.Spec); err != nil {
			return out, err
		}
		// JSON roundtrip prevents a caller from mutating the projected slices later.
		raw, _ := json.Marshal(speaker.Spec)
		var spec CharacterSpec
		if err := json.Unmarshal(raw, &spec); err != nil {
			return out, err
		}
		var doc *SpecV2
		if speaker.Document != nil {
			if err := ValidateSpecV2(speaker.Document); err != nil {
				return out, err
			}
			raw, _ := json.Marshal(speaker.Document)
			doc = new(SpecV2)
			if err := json.Unmarshal(raw, doc); err != nil {
				return out, err
			}
		}
		out.Speakers[key] = PinnedSpeaker{SpeakerKey: key, Role: speaker.Role, CharacterID: contentIdentity(spec, doc, in.SchemaVersion, in.RigVersion, in.VoiceConfigVersion), CharacterVersion: 1, Spec: spec, Document: doc, IsProvisional: speaker.IsProvisional}
	}
	docs, err := CompiledAssets(out)
	if err != nil {
		return out, err
	}
	for key, raw := range docs {
		sum := sha256.Sum256(raw)
		out.RigAssets = append(out.RigAssets, RigAsset{ObjectKey: key, SHA256: hex.EncodeToString(sum[:])})
	}
	sort.Slice(out.RigAssets, func(i, j int) bool { return out.RigAssets[i].ObjectKey < out.RigAssets[j].ObjectKey })
	return out, nil
}

// CompiledAssets uses only retained trusted rig readers; no model or TTS is involved.
func CompiledAssets(cast LessonCastSnapshot) (map[string][]byte, error) {
	docs := map[string][]byte{}
	for _, speaker := range cast.Speakers {
		for _, expression := range []string{"idle", "happy", "sad", "thinking", "celebrate"} {
			svg, err := CompileSVG(&speaker.Spec, expression)
			if err != nil {
				return nil, err
			}
			sum := sha256.Sum256([]byte(svg))
			key := fmt.Sprintf("lessons/character-rigs/%s/%x.svg", cast.RigVersion, sum)
			docs[key] = []byte(svg)
		}
	}
	return docs, nil
}

// ValidateExportCast rejects unknown fields, private links, tampered hashes/assets and invalid slots.
func ValidateExportCast(raw []byte) error {
	var cast LessonCastSnapshot
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&cast) != nil || !errors.Is(d.Decode(new(any)), io.EOF) {
		return errors.New("invalid_export")
	}
	var object struct {
		Speakers map[string]struct {
			Spec json.RawMessage `json:"spec"`
		} `json:"speakers"`
	}
	if json.Unmarshal(raw, &object) != nil {
		return errors.New("invalid_export")
	}
	for _, speaker := range object.Speakers {
		if _, err := ValidateSpecJSON(speaker.Spec); err != nil {
			return errors.New("invalid_export")
		}
	}
	projected, err := ExportCast(cast)
	if err != nil || !reflect.DeepEqual(cast, projected) {
		return errors.New("invalid_export")
	}
	return nil
}

// SafeRuntimeCast overlays invalid/taken-down slots without changing immutable stored content.
func SafeRuntimeCast(in LessonCastSnapshot, suppressed map[string]bool) LessonCastSnapshot {
	out := in
	out.Speakers = map[string]PinnedSpeaker{}
	for key, speaker := range in.Speakers {
		if suppressed[speaker.CharacterID] || speaker.Suppressed || ValidateSpec(&speaker.Spec) != nil || in.SchemaVersion != 1 || in.RigVersion != "original-v1" {
			id := map[string]string{"guide": "default-guide", "supporter": "default-supporter", "supporter_2": "default-companion"}[key]
			spec, ok := builtIn(id)
			if !ok {
				continue
			}
			speaker = PinnedSpeaker{SpeakerKey: key, Role: map[bool]string{true: "guide", false: "supporter"}[key == "guide"], CharacterID: id, CharacterVersion: 1, Spec: spec, IsProvisional: true, Suppressed: true}
			out.FallbackUsed = true
		}
		out.Speakers[key] = speaker
	}
	if _, ok := out.Speakers["guide"]; !ok {
		out.Speakers["guide"] = PinnedSpeaker{SpeakerKey: "guide", Role: "guide", CharacterID: "default-guide", CharacterVersion: 1, Spec: DefaultGuideFixture(), IsProvisional: true, Suppressed: true}
		out.FallbackUsed = true
	}
	return out
}

// SafeCast checks the independently persisted moderation overlay at every runtime boundary.
func (s *Service) SafeCast(ctx context.Context, in LessonCastSnapshot) LessonCastSnapshot {
	denied := map[string]bool{}
	for _, speaker := range in.Speakers {
		id := speaker.CharacterID
		contentID := contentIdentity(speaker.Spec, speaker.Document, in.SchemaVersion, in.RigVersion, in.VoiceConfigVersion)
		var found bool
		err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM character_suppressions WHERE snapshot_id=$1 OR snapshot_id=$2)`, id, contentID).Scan(&found)
		// Unavailable safety state fails safe, without a provider call.
		denied[id] = err != nil || found
	}
	return SafeRuntimeCast(in, denied)
}

// SafetyProjection is the player boundary; malformed snapshots yield safe defaults.
func (s *Service) SafetyProjection(ctx context.Context, raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var cast LessonCastSnapshot
	if json.Unmarshal(raw, &cast) != nil {
		cast = LessonCastSnapshot{}
	}
	safe := s.SafeCast(ctx, cast)
	result, _ := json.Marshal(safe)
	return result
}

// PublishedCopyCast reads independent copy content, never private library rows.
func PublishedCopyCast(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, owner, course string) (LessonCastSnapshot, bool, bool, error) {
	var raw []byte
	var forked bool
	err := q.QueryRow(ctx, `SELECT character_cast,forked_at IS NOT NULL FROM course_copies WHERE copy_course_id=$1 AND learner_id=$2`, course, owner).Scan(&raw, &forked)
	if errors.Is(err, pgx.ErrNoRows) {
		return LessonCastSnapshot{}, false, false, nil
	}
	if err != nil {
		return LessonCastSnapshot{}, false, false, err
	}
	var cast LessonCastSnapshot
	if len(raw) > 0 && string(raw) != "null" {
		err = json.Unmarshal(raw, &cast)
	}
	return cast, true, forked, err
}

// PinAssetReferences serializes reference creation against the content-hash purge lock.
func PinAssetReferences(ctx context.Context, tx pgx.Tx, cast LessonCastSnapshot) error {
	for _, asset := range cast.RigAssets {
		if _, err := tx.Exec(ctx, `UPDATE shared_character_assets SET released_at=NULL,purge_after=NULL,purged_at=NULL WHERE content_hash=$1`, asset.SHA256); err != nil {
			return err
		}
	}
	return nil
}

// PoolSafetyProjection lets player wiring share the same safe reader without changing storage.
func PoolSafetyProjection(pool *pgxpool.Pool) func(context.Context, json.RawMessage) json.RawMessage {
	return NewService(pool, nil, nil).SafetyProjection
}
