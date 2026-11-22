package accounts

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// ObjectPurgeRetention keeps an erased learner's or purged Course's objects longer than the 14-day
// nightly dumps, so a restorable dump never points at deleted objects (same rule as shared
// snapshot retention).
const ObjectPurgeRetention = 15 * 24 * time.Hour

// QueuePrivateObjectPurges records, in the caller's transaction, every private generated-lesson
// object prefix the learner owns, in the course_object_purges ledger that
// courses.SweepPurgedObjects drains after the retention (skipping any prefix a live manifest still
// names). It must run before the learner row (and its cascade) is deleted. Published snapshot
// prefixes (lessons/published/, retained by shared_asset_retention), content-hash narration audio
// (reference counted) and content-hash character assets are shared and never ledgered here.
func QueuePrivateObjectPurges(ctx context.Context, tx pgx.Tx, learnerID string, retention time.Duration) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO course_object_purges(prefix, purge_after)
		SELECT DISTINCT p, now() + make_interval(secs => $2) FROM (
			SELECT 'lessons/generated/' || g.id::text || '/' AS p FROM generated_lessons g WHERE g.learner_id = $1::uuid
			UNION
			SELECT substring(m.manifest_key from '^lessons/generated/[0-9a-f-]{36}/')
			FROM lesson_manifests m WHERE m.learner_id = $1::uuid AND m.manifest_key LIKE 'lessons/generated/%'
		) x WHERE p IS NOT NULL
		ON CONFLICT (prefix) DO NOTHING
	`, learnerID, retention.Seconds())
	return err
}
