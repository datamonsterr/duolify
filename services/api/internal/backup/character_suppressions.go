package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

const characterSuppressionPrefix = "character-suppressions/v1/"

// RecordCharacterSuppression persists a content-only tombstone before the database commits.
// Only a hash and closed reason code survive account/private-library deletion.
func (l ModerationLedger) RecordCharacterSuppression(ctx context.Context, id, reason string) error {
	hash := strings.TrimPrefix(id, "snapshot-")
	if l.Objects == nil || id != "snapshot-"+hash || !blockReference.MatchString(hash) || (reason != "unsafe" && reason != "legal") {
		return errLedger
	}
	raw, _ := json.Marshal(map[string]string{"snapshot_id": id, "reason_code": reason})
	return l.Objects.Put(ctx, characterSuppressionPrefix+hash+".json", bytes.NewReader(raw), int64(len(raw)))
}

// ReplayCharacterSuppressions is run before serving restored data, including pre-takedown dumps.
func ReplayCharacterSuppressions(ctx context.Context, pool *pgxpool.Pool, store BlockStore) error {
	if store == nil {
		return errLedger
	}
	objects, err := store.List(ctx, characterSuppressionPrefix)
	if err != nil {
		return fmt.Errorf("%w: %w", errLedger, err)
	}
	for _, object := range objects {
		raw, e := store.GetCurrent(ctx, object.Key)
		if e != nil {
			return fmt.Errorf("%w: %w", errLedger, e)
		}
		var rec struct {
			ID     string `json:"snapshot_id"`
			Reason string `json:"reason_code"`
		}
		if json.Unmarshal(raw, &rec) != nil || !strings.HasPrefix(rec.ID, "snapshot-") || !blockReference.MatchString(strings.TrimPrefix(rec.ID, "snapshot-")) || (rec.Reason != "unsafe" && rec.Reason != "legal") || object.Key != characterSuppressionPrefix+strings.TrimPrefix(rec.ID, "snapshot-")+".json" {
			return errLedger
		}
		if _, e = pool.Exec(ctx, `INSERT INTO character_suppressions(snapshot_id,reason_code) VALUES($1,$2) ON CONFLICT DO NOTHING`, rec.ID, rec.Reason); e != nil {
			return errors.New("character suppression replay failed")
		}
	}
	return nil
}
