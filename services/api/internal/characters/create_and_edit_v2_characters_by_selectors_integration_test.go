//go:build integration

package characters_test

import (
	"encoding/json"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/characters"
)

func TestSelectorCreateAndV2Edit(t *testing.T) {
	f := backuptest.New(t)
	f.Seed(t)
	ctx := t.Context()
	owner := "a1111111-1111-4111-8111-111111111111"
	s := characters.NewService(f.DB.Pool, f.ApplicationStorage(), f.Events())
	s.ConfigureScreening(newScreeningRPC(t))
	var ledgerBefore int
	if err := f.DB.Pool.QueryRow(ctx, `SELECT count(*) FROM credit_entries`).Scan(&ledgerBefore); err != nil {
		t.Fatal(err)
	}
	// The seeded learner already owns one v1 Character.
	var v1ID string
	if err := f.DB.Pool.QueryRow(ctx, `SELECT id::text FROM characters WHERE learner_id=$1`, owner).Scan(&v1ID); err != nil {
		t.Fatal(err)
	}

	doc := characters.DefaultSpecV2()
	doc.Info.Name = "Selector Sam"
	raw, _ := json.Marshal(doc)
	rec, err := s.CreateBySelectors(ctx, owner, characters.SelectorCreateRequest{Spec: raw})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Version != 1 || rec.Document == nil || rec.Document.SchemaVersion != characters.SchemaV2 {
		t.Fatalf("bad record %+v", rec)
	}
	var sum1 string
	if err := f.DB.Pool.QueryRow(ctx, `SELECT compile_sha256 FROM character_versions WHERE character_id=$1 AND version=1`, rec.ID).Scan(&sum1); err != nil || len(sum1) != 64 {
		t.Fatalf("checksum %q %v", sum1, err)
	}
	if rec.Checksum != sum1 {
		t.Fatal("record checksum differs from stored")
	}

	// Edit as v2: compare-and-swap, new immutable version, new checksum.
	doc.Body.PrimaryColor = "#E76F51"
	edited, _ := json.Marshal(doc)
	if _, err = s.Edit(ctx, owner, rec.ID, characters.EditRequest{ExpectedVersion: 5, Spec: edited}); err == nil || err.Error() != "version_conflict" {
		t.Fatalf("stale edit: %v", err)
	}
	got, err := s.Edit(ctx, owner, rec.ID, characters.EditRequest{ExpectedVersion: 1, Spec: edited})
	if err != nil || got.Version != 2 || got.Checksum == sum1 {
		t.Fatalf("v2 edit %+v %v", got, err)
	}

	// First v2 edit of a stored v1 Character keeps v1 immutable and writes v2 as version 2.
	var v1Before []byte
	if err = f.DB.Pool.QueryRow(ctx, `SELECT spec FROM character_versions WHERE character_id=$1 AND version=1`, v1ID).Scan(&v1Before); err != nil {
		t.Fatal(err)
	}
	read, err := s.Get(ctx, owner, v1ID)
	if err != nil || read.Document == nil || read.Document.SchemaVersion != characters.SchemaV2 {
		t.Fatalf("v1 read-time upgrade %+v %v", read, err)
	}
	up := *read.Document
	up.Info.Name = "Upgraded"
	upRaw, _ := json.Marshal(up)
	if rec2, err := s.Edit(ctx, owner, v1ID, characters.EditRequest{ExpectedVersion: 1, Spec: upRaw}); err != nil || rec2.Version != 2 {
		t.Fatalf("upgrade edit %+v %v", rec2, err)
	}
	var v1After []byte
	var v1Sum *string
	if err = f.DB.Pool.QueryRow(ctx, `SELECT spec,compile_sha256 FROM character_versions WHERE character_id=$1 AND version=1`, v1ID).Scan(&v1After, &v1Sum); err != nil {
		t.Fatal(err)
	}
	if string(v1Before) != string(v1After) || v1Sum != nil {
		t.Fatal("stored v1 version changed")
	}

	// Documents from a newer build degrade to read-only.
	var futureID string
	if err = f.DB.Pool.QueryRow(ctx, `INSERT INTO characters(learner_id) VALUES($1) RETURNING id::text`, owner).Scan(&futureID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(ctx, `INSERT INTO character_versions(character_id,version,spec) VALUES($1,1,'{"schema_version":99}')`, futureID); err != nil {
		t.Fatal(err)
	}
	fr, err := s.Get(ctx, owner, futureID)
	if err != nil || !fr.ReadOnly {
		t.Fatalf("future read %+v %v", fr, err)
	}
	if _, err = s.Edit(ctx, owner, futureID, characters.EditRequest{ExpectedVersion: 1, Spec: raw}); err == nil {
		t.Fatal("write to read-only document allowed")
	}

	// Selector create is free, rejects invalid documents, and honours the 10 Character cap.
	bad := *doc
	bad.Body.Species = "no-such-species"
	badRaw, _ := json.Marshal(bad)
	if _, err = s.CreateBySelectors(ctx, owner, characters.SelectorCreateRequest{Spec: badRaw}); err == nil || err.Error() != "invalid_spec" {
		t.Fatalf("invalid doc: %v", err)
	}
	var count int
	_ = f.DB.Pool.QueryRow(ctx, `SELECT count(*) FROM characters WHERE learner_id=$1 AND deleted_at IS NULL`, owner).Scan(&count)
	var last error
	for i := count; i <= 10; i++ {
		_, last = s.CreateBySelectors(ctx, owner, characters.SelectorCreateRequest{Spec: raw})
	}
	if last == nil || last.Error() != "limit_exceeded" {
		t.Fatalf("cap not enforced: %v", last)
	}
	var ledgerAfter int
	_ = f.DB.Pool.QueryRow(ctx, `SELECT count(*) FROM credit_entries`).Scan(&ledgerAfter)
	if ledgerAfter != ledgerBefore {
		t.Fatal("selector create touched the credit ledger")
	}
}
