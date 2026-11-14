package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

const blockPrefix = "moderation-blocks/v1/"

var blockReference = regexp.MustCompile(`^[0-9a-f]{64}$`)

// BlockStore is the read-only ledger view the restore drill needs.
type BlockStore interface {
	List(context.Context, string) ([]Object, error)
	GetCurrent(context.Context, string) ([]byte, error)
}

// ModerationLedger stores severe moderation blocks outside database snapshots, next to the deletion
// ledger. Records are conditional atomic Puts to deterministic keys, never rewritten or deleted, and
// hold only SHA-256 references: no course id, author or reason text.
type ModerationLedger struct{ Objects Objects }

// BlockKey is the ledger key of a block record.
func BlockKey(actionRef string) string { return blockPrefix + actionRef + ".json" }

// LiftKey is the ledger key of a lift marker written when an appeal overturns the action.
func LiftKey(actionRef string) string { return blockPrefix + actionRef + ".lifted.json" }

var errLedger = errors.New("moderation ledger unavailable or invalid reference")

func (l ModerationLedger) put(ctx context.Context, key string, body []byte) error {
	return l.Objects.Put(ctx, key, bytes.NewReader(body), int64(len(body)))
}

// RecordBlock must be durable before the takedown commits; a retry keeps the first record.
func (l ModerationLedger) RecordBlock(ctx context.Context, courseRef, actionRef string) error {
	if l.Objects == nil || !blockReference.MatchString(courseRef) || !blockReference.MatchString(actionRef) {
		return errLedger
	}
	return l.put(ctx, BlockKey(actionRef), []byte(`{"course_ref":"`+courseRef+`","action_ref":"`+actionRef+`"}`))
}

// LiftBlock records that an appeal overturned the action.
func (l ModerationLedger) LiftBlock(ctx context.Context, actionRef string) error {
	if l.Objects == nil || !blockReference.MatchString(actionRef) {
		return errLedger
	}
	return l.put(ctx, LiftKey(actionRef), []byte(`{"action_ref":"`+actionRef+`"}`))
}

// ReplayModerationBlocks re-applies every unlifted severe block to a restored database: the copies of
// the course are blocked again, the course stays taken down and its link and listing stay revoked.
// It returns how many copies it had to block again. Replay is idempotent.
func ReplayModerationBlocks(ctx context.Context, pool *pgxpool.Pool, store BlockStore) (int, error) {
	if store == nil {
		return 0, errLedger
	}
	objects, err := store.List(ctx, blockPrefix)
	if err != nil {
		return 0, errLedger
	}
	var blocks []string
	lifted := map[string]bool{}
	for _, o := range objects {
		switch {
		case !strings.HasPrefix(o.Key, blockPrefix):
		case strings.HasSuffix(o.Key, ".lifted.json"):
			lifted[strings.TrimSuffix(strings.TrimPrefix(o.Key, blockPrefix), ".lifted.json")] = true
		case strings.HasSuffix(o.Key, ".json"):
			blocks = append(blocks, o.Key)
		}
	}
	sort.Strings(blocks)
	total := 0
	for _, key := range blocks {
		raw, err := store.GetCurrent(ctx, key)
		if err != nil {
			return total, errLedger
		}
		var rec struct {
			CourseRef string `json:"course_ref"`
			ActionRef string `json:"action_ref"`
		}
		if json.Unmarshal(raw, &rec) != nil || !blockReference.MatchString(rec.CourseRef) || !blockReference.MatchString(rec.ActionRef) || BlockKey(rec.ActionRef) != key {
			return total, errLedger
		}
		if lifted[rec.ActionRef] {
			continue
		}
		tag, err := pool.Exec(ctx, `UPDATE course_copies SET blocked_at=now(),
 blocked_action_id=(SELECT a.id FROM moderation_actions a WHERE encode(sha256(convert_to(a.id::text,'UTF8')),'hex')=$2)
 WHERE blocked_at IS NULL AND source_course_id IS NOT NULL AND encode(sha256(convert_to(source_course_id::text,'UTF8')),'hex')=$1`, rec.CourseRef, rec.ActionRef)
		if err != nil {
			return total, errors.New("moderation block replay failed")
		}
		total += int(tag.RowsAffected())
		for _, stmt := range []string{
			`UPDATE courses SET moderation_state='taken_down',visibility='private' WHERE encode(sha256(convert_to(id::text,'UTF8')),'hex')=$1`,
			`UPDATE publication_links SET revoked_at=COALESCE(revoked_at,now()) WHERE encode(sha256(convert_to(course_id::text,'UTF8')),'hex')=$1`,
			`UPDATE marketplace_listings SET delisted_at=COALESCE(delisted_at,now()) WHERE encode(sha256(convert_to(course_id::text,'UTF8')),'hex')=$1`,
		} {
			if _, err = pool.Exec(ctx, stmt, rec.CourseRef); err != nil {
				return total, errors.New("moderation block replay failed")
			}
		}
	}
	return total, nil
}

// PinResolution checks that every live copy pin has a retained version and that every published
// object a live copy points to exists in application storage. Ended copies are not checked: their
// objects may already be purged.
func PinResolution(exists func(context.Context, string) (bool, error)) Check {
	return Check{"copy pins resolve", KindObjects, func(ctx context.Context, p *pgxpool.Pool) error {
		var unresolved int
		if err := p.QueryRow(ctx, `SELECT count(*) FROM course_copies cc JOIN courses c ON c.id=cc.copy_course_id AND c.deleted_at IS NULL
WHERE cc.draft_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM shared_asset_retention r WHERE r.prefix='lessons/published/'||cc.draft_id::text||'/' AND r.purged_at IS NULL)`).Scan(&unresolved); err != nil || unresolved != 0 {
			return errDrill
		}
		rows, err := p.Query(ctx, `SELECT DISTINCT m.manifest_key FROM lesson_manifests m JOIN course_lesson_stubs st ON st.id=m.lesson_id JOIN course_units u ON u.id=st.unit_id
JOIN courses c ON c.id=u.course_id AND c.deleted_at IS NULL WHERE starts_with(m.manifest_key,'lessons/published/') ORDER BY 1`)
		if err != nil {
			return errDrill
		}
		var keys []string
		for rows.Next() {
			var k string
			if err = rows.Scan(&k); err != nil {
				rows.Close()
				return errDrill
			}
			keys = append(keys, k)
		}
		rows.Close()
		if rows.Err() != nil {
			return errDrill
		}
		for _, key := range keys {
			if ok, err := exists(ctx, key); err != nil || !ok {
				return errDrill
			}
		}
		return nil
	}}
}
