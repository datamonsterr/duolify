package sharing

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Every independent publication, Course copy and historical Lesson counts, including a forked copy.
// Asset-hash row locks serialize this fresh recount with every reference-creating transaction.
const characterRefs = `SELECT
 (SELECT count(*) FROM publication_drafts d JOIN courses c ON c.id=d.course_id WHERE c.deleted_at IS NULL AND d.snapshot::text LIKE '%'||$1||'%')+
 (SELECT count(*) FROM course_copies cc JOIN courses c ON c.id=cc.copy_course_id WHERE c.deleted_at IS NULL AND cc.character_cast::text LIKE '%'||$1||'%')+
 (SELECT count(*) FROM lesson_cast_snapshots s JOIN courses c ON c.id=s.course_id WHERE c.deleted_at IS NULL AND s.snapshot::text LIKE '%'||$1||'%')`

func (s *Service) sweepCharacterAssets(ctx context.Context, now time.Time) error {
	// Soft-deleted Course content has the same grace as its assets. Active copies own independent payloads.
	for _, statement := range []string{`DELETE FROM lesson_cast_snapshots s USING courses c WHERE c.id=s.course_id AND c.deleted_at<$1::timestamptz-interval '30 days'`, `UPDATE course_copies cc SET character_cast=NULL FROM courses c WHERE c.id=cc.copy_course_id AND c.deleted_at<$1::timestamptz-interval '30 days'`} {
		if _, err := s.pool.Exec(ctx, statement, now); err != nil {
			return err
		}
	}
	rows, err := s.pool.Query(ctx, `SELECT content_hash FROM shared_character_assets ORDER BY content_hash`)
	if err != nil {
		return err
	}
	hashes, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, hash := range hashes {
		if err = s.sweepCharacterAsset(ctx, hash, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) sweepCharacterAsset(ctx context.Context, hash string, now time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var key string
	var released, purge, purged *time.Time
	if err = tx.QueryRow(ctx, `SELECT object_key,released_at,purge_after,purged_at FROM shared_character_assets WHERE content_hash=$1 FOR UPDATE`, hash).Scan(&key, &released, &purge, &purged); err != nil {
		return err
	}
	var refs int64
	if err = tx.QueryRow(ctx, characterRefs, key).Scan(&refs); err != nil {
		return err
	}
	switch {
	case refs > 0:
		_, err = tx.Exec(ctx, `UPDATE shared_character_assets SET ref_count=$2,released_at=NULL,purge_after=NULL WHERE content_hash=$1`, hash, refs)
	case released == nil && purged == nil:
		_, err = tx.Exec(ctx, `UPDATE shared_character_assets SET ref_count=0,released_at=$2,purge_after=$3 WHERE content_hash=$1`, hash, now, PurgeAt(now, max(30, s.cfg.AssetPurgeDays)))
	case purged == nil && purge != nil && !now.Before(*purge) && s.assets != nil:
		if err = s.assets.Delete(ctx, key); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE shared_character_assets SET ref_count=0,purged_at=$2 WHERE content_hash=$1`, hash, now)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
