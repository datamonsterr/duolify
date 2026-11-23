package characters

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/jackc/pgx/v5/pgconn"
)

// SelectorCreateRequest carries a v2 document built from catalogue selectors.
type SelectorCreateRequest struct {
	Spec json.RawMessage `json:"spec"`
}

// write is a validated Character document ready for screening and storage.
type write struct {
	schema    int
	doc       *SpecV2
	projected CharacterSpec
	compiled  string
	checksum  *string
}

func nullableID(id string) any {
	if id == "" {
		return nil
	}
	return id
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// ProjectV1 is the lossy v1 view of a v2 document for consumers that only need
// names, persona and voice (tutor, narration, labels). Appearance is neutral.
func ProjectV1(doc *SpecV2) CharacterSpec {
	fb := BundledFallbackSpec()
	return CharacterSpec{
		SchemaVersion: 1, RigVersion: "original-v1",
		Info: doc.Info, Personality: doc.Personality,
		Appearance:  fb.Appearance,
		Expressions: fb.Expressions,
		VoiceKey:    doc.VoiceKey,
	}
}

// fillRecord resolves any stored document into the record's views.
func fillRecord(r *Record, raw []byte, sum *string) error {
	reading, err := ReadSpec(raw)
	if err != nil {
		return err
	}
	r.Document = reading.Spec
	r.ReadOnly = reading.ReadOnly
	r.Checksum = derefStr(sum)
	if reading.SourceVersion == 1 {
		old, err := ValidateSpecJSON(raw)
		if err != nil {
			return err
		}
		r.Spec = *old
		return nil
	}
	r.Spec = ProjectV1(reading.Spec)
	return nil
}

// prepareWrite validates a submitted document (v1 or v2) and compiles it.
func prepareWrite(raw []byte) (write, error) {
	var head struct {
		SchemaVersion int `json:"schema_version"`
	}
	_ = json.Unmarshal(raw, &head)
	if head.SchemaVersion == SchemaV2 {
		doc, err := ValidateSpecV2JSON(raw)
		if err != nil {
			return write{}, err
		}
		svg, err := CompileV2(doc, "avatar", "idle", "idle")
		if err != nil {
			return write{}, err
		}
		sum := sha256.Sum256([]byte(svg))
		hexSum := hex.EncodeToString(sum[:])
		return write{schema: SchemaV2, doc: doc, projected: ProjectV1(doc), compiled: svg, checksum: &hexSum}, nil
	}
	spec, err := ValidateSpecJSON(raw)
	if err != nil {
		return write{}, err
	}
	svg, err := CompileSVG(spec, "idle")
	if err != nil {
		return write{}, err
	}
	return write{schema: 1, doc: UpgradeV1(spec), projected: *spec, compiled: svg}, nil
}

// CreateBySelectors creates a Character from selector choices. It is free but
// shares the hourly attempt budget, screening and the 10 Character cap.
func (s *Service) CreateBySelectors(ctx context.Context, owner string, in SelectorCreateRequest) (Record, error) {
	op, err := s.beginAttempt(ctx, owner, "")
	if err != nil {
		return Record{}, err
	}
	reject := func(reason string) (Record, error) {
		reason = boundedReason(reason)
		_, _ = s.pool.Exec(ctx, `UPDATE character_screenings SET verdict='rejected',reason_code=$2 WHERE id=$1`, op, reason)
		s.emit(ctx, owner, "character_rejected", map[string]any{"operation": "selector_create", "reason_code": reason})
		return Record{}, errors.New(reason)
	}
	var head struct {
		SchemaVersion int `json:"schema_version"`
	}
	if json.Unmarshal(in.Spec, &head) != nil || head.SchemaVersion != SchemaV2 {
		return reject("invalid_spec")
	}
	w, err := prepareWrite(in.Spec)
	if err != nil {
		return reject("invalid_spec")
	}
	if s.screener == nil {
		return reject("screening_unavailable")
	}
	screenCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	response, err := s.screener.ScreenCharacter(screenCtx, &pb.ScreenCharacterRequest{SpecJson: string(in.Spec), CompiledAppearance: w.compiled})
	cancel()
	s.emitScreenUsage(ctx, owner, response)
	if err != nil || response == nil || response.PolicyVersion != "character-v1" || len(response.Attempts) == 0 || len(response.Attempts) > 2 {
		return reject("screening_unavailable")
	}
	last := response.Attempts[len(response.Attempts)-1]
	if response.Approved && (last == nil || !last.Delivered || last.UsageUnavailable || uint64(last.InputTokens)+uint64(last.CacheWriteTokens)+uint64(last.CacheReadTokens) > 18000 || last.OutputTokens > 200) {
		return reject("screening_unavailable")
	}
	if !response.Approved {
		return reject(response.ReasonCode)
	}
	if response.ReasonCode != "" {
		return reject("screening_unavailable")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Record{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id string
	// The count trigger serializes on the learner row and raises limit_exceeded.
	if err = tx.QueryRow(ctx, `INSERT INTO characters(learner_id) VALUES($1) RETURNING id::text`, owner).Scan(&id); err != nil {
		if isLimit(err) {
			return Record{}, errors.New("limit_exceeded")
		}
		return Record{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO character_versions(character_id,version,spec,compile_sha256) VALUES($1,1,$2,$3)`, id, in.Spec, w.checksum); err != nil {
		return Record{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE character_screenings SET verdict='approved',policy_version=$2,character_id=$3 WHERE id=$1`, op, response.PolicyVersion, id); err != nil {
		return Record{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Record{}, err
	}
	s.emit(ctx, owner, "character_created", map[string]any{"character_id": id, "character_version": 1, "method": "selectors", "schema_version": SchemaV2, "kind": w.doc.Body.Kind})
	return Record{ID: id, Version: 1, Spec: w.projected, Document: w.doc, Checksum: derefStr(w.checksum)}, nil
}

func (s *Service) emitScreenUsage(ctx context.Context, owner string, response *pb.ScreenCharacterResponse) {
	if response == nil {
		return
	}
	for _, a := range response.Attempts {
		if a == nil {
			continue
		}
		s.emit(ctx, owner, "$ai_generation", map[string]any{"feature": "character_screen", "provider": a.Provider, "model": a.Model, "input_tokens": a.InputTokens, "output_tokens": a.OutputTokens, "cache_write_tokens": a.CacheWriteTokens, "cache_read_tokens": a.CacheReadTokens, "latency_ms": a.LatencyMs, "attempt": a.Attempt, "fallback_used": a.FallbackUsed, "succeeded": a.Delivered, "usage_available": !a.UsageUnavailable, "error_code": a.ErrorCode})
	}
}

func isLimit(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Message == "limit_exceeded"
}
