package sharing

import (
	"context"
	"errors"
	"time"
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

var errNotImplemented = errors.New("not implemented")

// WithAssetStore returns a copy that can purge released objects.
func (s *Service) WithAssetStore(a AssetStore) *Service { c := *s; c.assets = a; return &c }

// WithBlockLedger returns a copy that records severe blocks in the backup ledger.
func (s *Service) WithBlockLedger(l BlockLedger) *Service { c := *s; c.ledger = l; return &c }

// SweepAssets is the retention job.
func (s *Service) SweepAssets(context.Context, time.Time) (SweepResult, error) {
	return SweepResult{}, errNotImplemented
}

// Unpublish makes the course private and stops new enrollments; copies stay.
func (s *Service) Unpublish(context.Context, string, string) error { return errNotImplemented }

// ReleaseReadiness reports the release criteria to a moderator.
func (s *Service) ReleaseReadiness(context.Context, string) (Readiness, error) {
	return Readiness{}, errNotImplemented
}

// LedgerRef is the ledger reference of an id: a hash, never the id.
func LedgerRef(id string) string { return id }

// PurgeAt is when a prefix released at now may be purged.
func PurgeAt(now time.Time, days int) time.Time { return now }

// PinPrefix is the object prefix of one published version.
func PinPrefix(draftID string) string { return "" }

// PrefixOfKey returns the published prefix a manifest key lives under.
func PrefixOfKey(key string) (string, bool) { return "", false }

// EvaluateReadiness is the pure release report over configuration.
func EvaluateReadiness(cfg Config, erasureAnonymizes bool) Readiness { return Readiness{} }
