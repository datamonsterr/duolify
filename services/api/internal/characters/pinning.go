package characters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/jackc/pgx/v5"
)

const (
	maxSegmentText = 280
	maxSegments    = 12
)

var segmentPoses = []string{"idle", "talk", "point", "think", "celebrate", "wave", "shrug"}
var assetLike = regexp.MustCompile(`(?i)(://|^//|data:|javascript:|url\(|www\.)`)

// PinJobCastTx resolves and stores the effective Cast inside the caller's job
// transaction. Existing snapshots win, so worker retries and Retry variants
// reuse the exact payload while later edits or selections affect new jobs only.
func (s *Service) PinJobCastTx(ctx context.Context, tx pgx.Tx, owner, course, jobKey string) (LessonCastSnapshot, error) {
	var out LessonCastSnapshot
	if err := s.checkCourse(ctx, owner, course); err != nil {
		return out, err
	}
	read := func() error {
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT snapshot FROM lesson_cast_snapshots WHERE course_id=$1 AND lesson_key=$2 AND learner_id=$3`, course, jobKey, owner).Scan(&raw); err != nil {
			return err
		}
		return json.Unmarshal(raw, &out)
	}
	err := read()
	if err == nil || !errors.Is(err, pgx.ErrNoRows) {
		return s.SafeCast(ctx, out), err
	}
	effective, err := s.effectiveCast(ctx, owner, course)
	if err != nil {
		return out, err
	}
	effective = s.SafeCast(ctx, effective)
	raw, err := json.Marshal(effective)
	if err != nil {
		return out, err
	}
	if err = PinAssetReferences(ctx, tx, effective); err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO lesson_cast_snapshots(course_id,learner_id,lesson_key,snapshot) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, course, owner, jobKey, raw); err != nil {
		return out, err
	}
	err = read()
	return s.SafeCast(ctx, out), err
}

// SnapshotTx reads an already pinned job Cast without resolving current data.
func (s *Service) SnapshotTx(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, owner, course, jobKey string) (LessonCastSnapshot, error) {
	var out LessonCastSnapshot
	var raw []byte
	err := q.QueryRow(ctx, `SELECT snapshot FROM lesson_cast_snapshots WHERE course_id=$1 AND lesson_key=$2 AND learner_id=$3`, course, jobKey, owner).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	return s.SafeCast(ctx, out), nil
}

// PersonasJSON is the immutable data-only payload sent to the engine.
func PersonasJSON(cast LessonCastSnapshot) (string, error) {
	raw, err := json.Marshal(cast)
	return string(raw), err
}

// ValidateSegments accepts bounded plain text only for pinned speakers, catalog
// poses and expressions, and times inside the Slide.
func ValidateSegments(segments []*pb.CharacterSegment, cast LessonCastSnapshot, slideMs uint32) error {
	if len(segments) > maxSegments {
		return errors.New("too many segments")
	}
	for _, segment := range segments {
		if segment == nil {
			return errors.New("empty segment")
		}
		if _, ok := cast.Speakers[segment.SpeakerKey]; !ok {
			return fmt.Errorf("unknown speaker %q", segment.SpeakerKey)
		}
		if !slices.Contains(validExpressionsV2, segment.Expression) {
			return errors.New("unsupported expression")
		}
		if segment.Pose != "" && !slices.Contains(segmentPoses, segment.Pose) {
			return errors.New("unsupported pose")
		}
		if !PlainSegmentText(segment.Text) {
			return errors.New("segment text must be bounded plain text")
		}
		if segment.DurationMs == 0 || uint64(segment.StartMs)+uint64(segment.DurationMs) > uint64(slideMs) {
			return errors.New("segment outside slide clock")
		}
	}
	return nil
}

// PlainSegmentText rejects markup, control characters and asset references.
func PlainSegmentText(text string) bool {
	if strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > maxSegmentText || assetLike.MatchString(text) {
		return false
	}
	for _, r := range text {
		if unicode.IsControl(r) || strings.ContainsRune("<>&{}`\\", r) {
			return false
		}
	}
	return true
}

// ProjectSegments adds readable speaker labels from the pinned Cast.
func ProjectSegments(segments []*pb.CharacterSegment, cast LessonCastSnapshot) []SlideSegment {
	out := make([]SlideSegment, 0, len(segments))
	for _, s := range segments {
		out = append(out, SlideSegment{SpeakerKey: s.SpeakerKey, SpeakerLabel: cast.Speakers[s.SpeakerKey].Spec.Info.Name, Text: s.Text, Expression: s.Expression, Pose: s.Pose, StartMs: int(s.StartMs), DurationMs: int(s.DurationMs)})
	}
	return out
}
