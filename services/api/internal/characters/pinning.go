package characters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
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

var segmentExpressions = map[string]bool{"idle": true, "happy": true, "sad": true, "thinking": true, "celebrate": true}
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
		return out, err
	}
	effective, err := s.effectiveCast(ctx, owner, course)
	if err != nil {
		return out, err
	}
	raw, err := json.Marshal(effective)
	if err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO lesson_cast_snapshots(course_id,learner_id,lesson_key,snapshot) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, course, owner, jobKey, raw); err != nil {
		return out, err
	}
	return out, read()
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
	return out, json.Unmarshal(raw, &out)
}

// PersonasJSON is the immutable data-only payload sent to the engine.
func PersonasJSON(cast LessonCastSnapshot) (string, error) {
	raw, err := json.Marshal(cast)
	return string(raw), err
}

// ValidateSegments accepts bounded plain text only for pinned speakers, the five
// supported expressions and times inside the Slide. Assets are never accepted.
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
		if !segmentExpressions[segment.Expression] {
			return errors.New("unsupported expression")
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
