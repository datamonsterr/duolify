package player

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// GeneratedSlide is the assessment-free projection of an already validated generated Slide.
type GeneratedSlide struct {
	ID, HTML, Narration string
	DurationMS          int
	Regions             []Region `json:",omitempty"`
}

// GeneratedWriter accepts immutable, idempotent object publication.
type GeneratedWriter interface {
	Put(context.Context, string, []byte) error
}

// PublishGenerated validates the complete deck before writing. Content-addressed
// keys make partial writes and database rollback safe to retry without cleanup or
// overwriting another deck. The caller commits the returned reference separately.
func PublishGenerated(ctx context.Context, writer GeneratedWriter, prefix string, slides []GeneratedSlide, origin string) (string, error) {
	if writer == nil {
		return "", errors.New("generated object writer unavailable")
	}
	raw, err := json.Marshal(slides)
	if err != nil {
		return "", err
	}
	key, docs, err := GeneratedObjects(prefix+"/"+checksum(raw), slides, origin)
	if err != nil {
		return "", err
	}
	if len(docs[key]) > maxObjectBytes {
		return "", errInvalid
	}
	for object, document := range docs {
		if object != key {
			if err = writer.Put(ctx, object, document); err != nil {
				return "", err
			}
		}
	}
	if err = writer.Put(ctx, key, docs[key]); err != nil {
		return "", err
	}
	return key, nil
}

// GeneratedObjects materializes existing content only. It never calls an Engine or exposes grading.
func GeneratedObjects(prefix string, slides []GeneratedSlide, origin string) (string, map[string][]byte, error) {
	manifest := ObjectManifest{Version: 1, Slides: []ObjectSlide{}}
	docs := map[string][]byte{}
	for i, slide := range slides {
		if err := ValidateHTML(slide.HTML, origin); err != nil {
			return "", nil, err
		}
		key := fmt.Sprintf("%s/slide-%d.html", prefix, i)
		docs[key] = []byte(slide.HTML)
		manifest.Slides = append(manifest.Slides, ObjectSlide{ID: slide.ID, DurationMS: slide.DurationMS, HTMLKey: key, HTMLSHA256: checksum([]byte(slide.HTML)), Assets: []string{}, Checkpoints: []Checkpoint{}, NarrationText: slide.Narration, Regions: slide.Regions})
	}
	if err := ValidateManifest(manifest, origin); err != nil {
		return "", nil, err
	}
	key := prefix + "/manifest.json"
	raw, err := json.Marshal(manifest)
	if err != nil {
		return "", nil, err
	}
	docs[key] = raw
	return key, docs, nil
}

const generatedAccessSQL = `SELECT g.id::text FROM generated_lessons g JOIN course_lesson_stubs l ON l.id=g.lesson_stub_id
 JOIN course_units u ON u.id=l.unit_id JOIN courses c ON c.id=u.course_id
 JOIN course_enrollments e ON e.course_id=c.id AND e.learner_id=$2::uuid
 WHERE g.lesson_stub_id=$1::uuid AND g.learner_id=$2::uuid AND l.learner_id=g.learner_id AND c.learner_id=g.learner_id
 AND l.status<>'locked' AND c.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM course_copies cc WHERE cc.copy_course_id=c.id AND cc.blocked_at IS NOT NULL)`

// MaterializeGenerated bridges DB-only generated Lessons into the existing versioned player store.
// Authorization is identical to playback and is checked again after object publication.
// Existing manifests always win. No provider, quote, grading or progress operation is performed.
func (s *Service) MaterializeGenerated(ctx context.Context, owner, lesson string) error {
	var generated string
	err := s.opts.Pool.QueryRow(ctx, generatedAccessSQL, lesson, owner).Scan(&generated)
	if err != nil {
		return err
	}
	var exists bool
	if err = s.opts.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM lesson_manifests WHERE lesson_id=$1::uuid AND learner_id=$2::uuid)`, lesson, owner).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	writer, ok := s.opts.Objects.(GeneratedWriter)
	if !ok {
		return errors.New("generated object writer unavailable")
	}
	rows, err := s.opts.Pool.Query(ctx, `SELECT slide_id,html,duration_ms,narration FROM lesson_slides WHERE lesson_stub_id=$1::uuid AND learner_id=$2::uuid ORDER BY position`, lesson, owner)
	if err != nil {
		return err
	}
	var slides []GeneratedSlide
	for rows.Next() {
		var slide GeneratedSlide
		if err = rows.Scan(&slide.ID, &slide.HTML, &slide.DurationMS, &slide.Narration); err != nil {
			rows.Close()
			return err
		}
		slides = append(slides, slide)
	}
	rows.Close()
	if rows.Err() != nil {
		return rows.Err()
	}
	key, err := PublishGenerated(ctx, writer, "lessons/generated/"+generated, slides, s.opts.AssetOrigin)
	if err != nil {
		return err
	}
	// Recheck eligibility under row locks only after all object writes finish.
	// A deletion or moderation block during publication must not expose content.
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var current string
	err = tx.QueryRow(ctx, generatedAccessSQL+" FOR SHARE OF c,l", lesson, owner).Scan(&current)
	if err != nil {
		return err
	}
	if current != generated {
		return errors.New("generated lesson changed during publication")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO lesson_manifests(lesson_id,learner_id,version,manifest_key) VALUES($1,$2,1,$3) ON CONFLICT(lesson_id) DO NOTHING`, lesson, owner, key); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
