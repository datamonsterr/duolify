package sharing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// AssetStore lists and deletes published lesson objects for the retention job.
type AssetStore interface {
	List(ctx context.Context, prefix string) ([]string, error)
	Delete(ctx context.Context, key string) error
}

// SweepResult counts what one retention pass did.
type SweepResult struct{ Tracked, Released, Revived, Purged int }

// BlockLedger records severe moderation blocks outside the database so a restore can re-apply them.
type BlockLedger interface {
	RecordBlock(ctx context.Context, courseRef, actionRef string) error
	LiftBlock(ctx context.Context, actionRef string) error
}

// Criterion is one release-readiness line.
type Criterion struct {
	Key    string `json:"key"`
	Met    bool   `json:"met"`
	Detail string `json:"detail"`
}

// Readiness reports what is satisfied before the Marketplace flag may be lifted. It never lifts it.
type Readiness struct {
	Ready              bool        `json:"ready"`
	MarketplaceEnabled bool        `json:"marketplace_enabled"`
	Criteria           []Criterion `json:"criteria"`
}

// minAssetPurgeDays keeps published objects longer than nightly dumps (14 days), so a restorable dump
// never points at purged objects.
const minAssetPurgeDays = 15

// WithAssetStore returns a copy that can purge released objects.
func (s *Service) WithAssetStore(a AssetStore) *Service { c := *s; c.assets = a; return &c }

// WithBlockLedger returns a copy that records severe blocks in the backup ledger.
func (s *Service) WithBlockLedger(l BlockLedger) *Service { c := *s; c.ledger = l; return &c }

// LedgerRef is the ledger reference of an id: a hash, never the id.
func LedgerRef(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

// PurgeAt is when a prefix released at now may be purged.
func PurgeAt(now time.Time, days int) time.Time { return now.Add(time.Duration(days) * 24 * time.Hour) }

const publishedRoot = "lessons/published/"

var publishedPrefix = regexp.MustCompile(`^lessons/published/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/`)

// PinPrefix is the object prefix of one published version.
func PinPrefix(draftID string) string { return publishedRoot + draftID + "/" }

// PrefixOfKey returns the published prefix a manifest key lives under.
func PrefixOfKey(key string) (string, bool) {
	p := publishedPrefix.FindString(key)
	return p, p != ""
}

// pinPrefix registers the version and cancels any pending release. The row lock it takes lasts to
// commit, so a purge of this prefix waits for the pinning transaction and then recounts.
func pinPrefix(ctx context.Context, tx pgx.Tx, draftID string) error {
	_, err := tx.Exec(ctx, `INSERT INTO shared_asset_retention(prefix) VALUES($1)
ON CONFLICT (prefix) DO UPDATE SET released_at=NULL, purge_after=NULL, purged_at=NULL`, PinPrefix(draftID))
	return err
}

// liveRefs counts the live copies that pin the prefix or hold a lesson manifest under it.
const liveRefs = `SELECT count(*) FROM (
  SELECT cc.copy_course_id AS id FROM course_copies cc JOIN courses c ON c.id=cc.copy_course_id AND c.deleted_at IS NULL WHERE cc.draft_id=$2::uuid
  UNION
  SELECT c.id FROM lesson_manifests m JOIN course_lesson_stubs st ON st.id=m.lesson_id JOIN course_units u ON u.id=st.unit_id
    JOIN courses c ON c.id=u.course_id AND c.deleted_at IS NULL WHERE starts_with(m.manifest_key,$1)
) x`

// SweepAssets is the retention job. It tracks every referenced version, releases a version whose last
// live copy ended, revives one that gained a copy again, and purges a released version once its period
// is over. Every decision is made under the row lock after a fresh recount.
func (s *Service) SweepAssets(ctx context.Context, now time.Time) (SweepResult, error) {
	var res SweepResult
	if _, err := s.pool.Exec(ctx, `INSERT INTO shared_asset_retention(prefix)
SELECT DISTINCT p FROM (
  SELECT 'lessons/published/'||draft_id::text||'/' AS p FROM course_copies WHERE draft_id IS NOT NULL
  UNION ALL
  SELECT substring(manifest_key from '^lessons/published/[0-9a-f-]{36}/') FROM lesson_manifests WHERE manifest_key LIKE 'lessons/published/%'
) x WHERE p IS NOT NULL ON CONFLICT DO NOTHING`); err != nil {
		return res, err
	}
	rows, err := s.pool.Query(ctx, `SELECT prefix FROM shared_asset_retention ORDER BY prefix`)
	if err != nil {
		return res, err
	}
	prefixes, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return res, err
	}
	for _, prefix := range prefixes {
		if err = s.sweepOne(ctx, prefix, now, &res); err != nil {
			return res, err
		}
	}
	return res, nil
}

func (s *Service) sweepOne(ctx context.Context, prefix string, now time.Time, res *SweepResult) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var released, purgeAfter, purged *time.Time
	if err = tx.QueryRow(ctx, `SELECT released_at,purge_after,purged_at FROM shared_asset_retention WHERE prefix=$1 FOR UPDATE`, prefix).Scan(&released, &purgeAfter, &purged); err != nil {
		if err == pgx.ErrNoRows {
			return nil
		}
		return err
	}
	res.Tracked++
	var refs int
	draft := strings.TrimSuffix(strings.TrimPrefix(prefix, publishedRoot), "/")
	if err = tx.QueryRow(ctx, liveRefs, prefix, draft).Scan(&refs); err != nil {
		return err
	}
	switch {
	case refs > 0 && released != nil:
		res.Revived++
		_, err = tx.Exec(ctx, `UPDATE shared_asset_retention SET ref_count=$2,released_at=NULL,purge_after=NULL WHERE prefix=$1`, prefix, refs)
	case refs > 0:
		_, err = tx.Exec(ctx, `UPDATE shared_asset_retention SET ref_count=$2 WHERE prefix=$1`, prefix, refs)
	case released == nil && purged == nil:
		res.Released++
		_, err = tx.Exec(ctx, `UPDATE shared_asset_retention SET ref_count=0,released_at=$2,purge_after=$3 WHERE prefix=$1`, prefix, now, PurgeAt(now, s.cfg.AssetPurgeDays))
	case purged == nil && purgeAfter != nil && !now.Before(*purgeAfter) && s.assets != nil:
		keys, lerr := s.assets.List(ctx, prefix)
		if lerr != nil {
			return lerr
		}
		for _, key := range keys {
			if lerr = s.assets.Delete(ctx, key); lerr != nil {
				return lerr
			}
		}
		res.Purged++
		_, err = tx.Exec(ctx, `UPDATE shared_asset_retention SET ref_count=0,purged_at=$2 WHERE prefix=$1`, prefix, now)
	default:
		_, err = tx.Exec(ctx, `UPDATE shared_asset_retention SET ref_count=0 WHERE prefix=$1`, prefix)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Unpublish makes the course private: it revokes the share link, delists the listing, revokes pending
// invites and stops new enrollments. Existing copies are never touched. It is idempotent.
func (s *Service) Unpublish(ctx context.Context, owner, courseID string) error {
	if !validID(owner) || !validID(courseID) {
		return ErrUnavailable
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = owns(ctx, tx, owner, courseID); err != nil {
		return err
	}
	var from string
	if err = tx.QueryRow(ctx, `SELECT visibility::text FROM courses WHERE id=$1`, courseID).Scan(&from); err != nil {
		return err
	}
	for _, q := range []string{
		`UPDATE publication_links SET revoked_at=now() WHERE course_id=$1 AND revoked_at IS NULL`,
		`UPDATE marketplace_listings SET delisted_at=now() WHERE course_id=$1 AND delisted_at IS NULL`,
		`UPDATE course_invites SET status='revoked',resolved_at=now() WHERE course_id=$1 AND status='pending'`,
		`UPDATE courses SET visibility='private' WHERE id=$1 AND visibility<>'private'`,
	} {
		if _, err = tx.Exec(ctx, q, courseID); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	s.invalidateCatalogue(ctx)
	if from != "private" {
		s.emit(owner, "course_visibility_changed", map[string]any{"course_id": courseID, "from": from, "to": "private"})
	}
	return nil
}

// MaintainAssets runs the retention job daily until ctx ends.
func (s *Service) MaintainAssets(ctx context.Context) {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		if r, err := s.SweepAssets(ctx, time.Now()); err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "shared asset retention failed", "error", err)
		} else if err == nil && (r.Released+r.Purged+r.Revived) > 0 {
			slog.InfoContext(ctx, "shared asset retention", "released", r.Released, "purged", r.Purged, "revived", r.Revived)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// EvaluateReadiness is the pure release report over configuration. Criteria keep one stable order.
func EvaluateReadiness(cfg Config, erasureAnonymizes bool) Readiness {
	limit := func(key string, n int, what string) Criterion {
		return Criterion{key, n > 0, fmt.Sprintf("%s limited to %d", what, n)}
	}
	criteria := []Criterion{
		{"approval.cdn_policy", cfg.ReleaseCDNPolicyApproved, "Owner approval of the CDN policy (SHARING_RELEASE_CDN_POLICY_APPROVED)"},
		{"approval.legal", cfg.ReleaseLegalApproved, "Owner and legal approval of the sharing policy (SHARING_RELEASE_LEGAL_APPROVED)"},
		limit("caps.enroll", cfg.MaxEnrollmentsPerDay, "New copies per learner per day"),
		limit("caps.invite", cfg.InviteSendsPerDay, "Invites per owner per day"),
		{"caps.publish", cfg.MarketplaceMaxListingsPerOwner > 0 && cfg.MaxRepublishesPerDay > 0, fmt.Sprintf("Listings per owner %d, new versions per course per day %d", cfg.MarketplaceMaxListingsPerOwner, cfg.MaxRepublishesPerDay)},
		limit("caps.report", cfg.ReportMaxPerDay, "Reports per account per day"),
		limit("caps.review", cfg.ReviewMaxPerDay, "Reviews per account per day"),
		{"erasure.attribution", erasureAnonymizes, "Deleting an owner account clears creator attribution on copies"},
		{"noindex", true, "Public previews and marketplace responses send X-Robots-Tag: noindex"},
		{"retention.assets", cfg.AssetPurgeDays >= minAssetPurgeDays, fmt.Sprintf("Published objects purged %d days after the last copy ends (minimum %d)", cfg.AssetPurgeDays, minAssetPurgeDays)},
	}
	ready := true
	for _, c := range criteria {
		ready = ready && c.Met
	}
	return Readiness{Ready: ready, MarketplaceEnabled: cfg.MarketplaceEnabled, Criteria: criteria}
}

// ReleaseReadiness reports the release criteria to a moderator. It reads state and changes nothing.
func (s *Service) ReleaseReadiness(ctx context.Context, moderator string) (Readiness, error) {
	if err := s.requireModerator(ctx, moderator); err != nil {
		return Readiness{}, err
	}
	var anonymizes bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_constraint k JOIN pg_attribute a ON a.attrelid=k.conrelid AND a.attnum=ANY(k.conkey)
WHERE k.conrelid='course_copies'::regclass AND k.confrelid='learners'::regclass AND k.contype='f' AND k.confdeltype='n' AND a.attname='source_owner_id')`).Scan(&anonymizes)
	if err != nil {
		return Readiness{}, err
	}
	return EvaluateReadiness(s.cfg, anonymizes), nil
}
