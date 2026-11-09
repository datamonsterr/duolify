// Package playerfixture supplies synthetic content to disposable test servers only.
// Production never imports this package or publishes fixtures on learner reads.
package playerfixture

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"

	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed slides/*.html
var slides embed.FS

// AssetOrigin is a synthetic public asset origin, never a live service.
const AssetOrigin = "https://assets.example.test"

// ManifestKey pins the Git-backed fixture content version.
const ManifestKey = "lessons/fixture-v1/manifest.json"

// Documents returns a fresh copy of the immutable fixture objects.
func Documents() map[string][]byte {
	m := player.ObjectManifest{Version: 1, Slides: []player.ObjectSlide{}}
	out := map[string][]byte{}
	captions := []string{"The sky is a map waiting to be explored.", "Stars are distant suns, shining across space.", "Notice how the sky changes as Earth turns."}
	for i, name := range []string{"one", "two", "three"} {
		raw, _ := slides.ReadFile("slides/" + name + ".html")
		key := "lessons/fixture-v1/" + name + ".html"
		out[key] = raw
		m.Slides = append(m.Slides, player.ObjectSlide{ID: fmt.Sprintf("sky-%d", i+1), DurationMS: 3000, HTMLKey: key, Assets: []string{}, Checkpoints: []player.Checkpoint{{ID: "explanation", AtMS: 1500}}, NarrationText: captions[i]})
	}
	out[ManifestKey], _ = json.Marshal(m)
	return out
}

// Writer publishes fixture bytes into disposable object storage.
type Writer interface {
	Put(context.Context, string, []byte) error
}

// Publish validates fixture documents before committing the metadata reference.
func Publish(ctx context.Context, pool *pgxpool.Pool, objects Writer, owner, lesson string) error {
	documents := Documents()
	var manifest player.ObjectManifest
	if err := json.Unmarshal(documents[ManifestKey], &manifest); err != nil {
		return err
	}
	if err := player.ValidateManifest(manifest, AssetOrigin); err != nil {
		return err
	}
	for key, raw := range documents {
		if key != ManifestKey {
			if err := player.ValidateHTML(string(raw), AssetOrigin); err != nil {
				return err
			}
		}
	}
	for key, raw := range documents {
		if err := objects.Put(ctx, key, raw); err != nil {
			return err
		}
	}
	_, err := pool.Exec(ctx, `INSERT INTO lesson_manifests(lesson_id,learner_id,version,manifest_key) VALUES($1,$2,1,$3) ON CONFLICT(lesson_id) DO NOTHING`, lesson, owner, ManifestKey)
	return err
}

// Seed enrolls a synthetic fixture course through existing owned metadata. It
// neither invokes a model nor mutates any real learner course or wallet.
func Seed(ctx context.Context, pool *pgxpool.Pool, objects Writer, owner string) (string, error) {
	var lesson string
	if err := pool.QueryRow(ctx, `SELECT l.id::text FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id JOIN courses c ON c.id=u.course_id WHERE l.learner_id::text=$1 AND l.authored_id='player-fixture' AND c.deleted_at IS NULL LIMIT 1`, owner).Scan(&lesson); err == nil {
		return lesson, Publish(ctx, pool, objects, owner, lesson)
	}
	quote, err := pricing.NewQuotes(pool).Create(ctx, owner, pricing.QuoteInput{Action: "cached_replay"})
	if err != nil {
		return "", err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	creation, course, unit := uuid.NewString(), uuid.NewString(), uuid.NewString()
	lesson = uuid.NewString()
	if _, err = tx.Exec(ctx, `INSERT INTO course_creations(id,learner_id,draft_id,revision,quote_id,request,state,course_id) VALUES($1,$2,$3,1,$4,'{}','enrolled',$5)`, creation, owner, uuid.NewString(), quote.ID, course); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO courses(id,learner_id,creation_id,title) VALUES($1,$2,$3,'Everyday astronomy')`, course, owner, creation); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO course_units(id,learner_id,course_id,authored_id,position,title) VALUES($1,$2,$3,'player-fixture',0,'Meet the sky')`, unit, owner, course); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO course_lesson_stubs(id,learner_id,unit_id,authored_id,position,title,goal,status) VALUES($1,$2,$3,'player-fixture',0,'Why the sky changes','Explain how Earth turns','ready_to_generate')`, lesson, owner, unit); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO course_enrollments(learner_id,course_id) VALUES($1,$2)`, owner, course); err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return lesson, Publish(ctx, pool, objects, owner, lesson)
}
