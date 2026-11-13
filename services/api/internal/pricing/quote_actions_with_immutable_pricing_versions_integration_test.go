//go:build integration

package pricing_test

import (
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"testing"
)

func TestQuoteSnapshot(t *testing.T) {
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("quotes"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)
	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err = migrations.Up(ctx, url); err != nil {
		t.Fatal(err)
	}
	st, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var owner string
	if err = st.Pool.QueryRow(ctx, "INSERT INTO learners(display_name) VALUES('quote fixture') RETURNING id::text").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	service := pricing.NewQuotes(st.Pool)
	q, err := service.Create(ctx, owner, pricing.QuoteInput{Action: "lesson_content", NarrationCharacters: 1440})
	if err != nil {
		t.Fatal(err)
	}
	if q.ContentCeiling != 140 || q.AudioCeiling != 50 {
		t.Fatalf("bad quote %+v", q)
	}
	if err = service.Accept(ctx, owner, q.ID); err != nil {
		t.Fatal(err)
	}
	// An expired quote cannot acquire consent, and another learner cannot read terms.
	if _, err = service.Get(ctx, "a1111111-1111-4111-8111-111111111111", q.ID); err == nil {
		t.Fatal("cross-owner read")
	}
	var expiredID string
	err = st.Pool.QueryRow(ctx, `INSERT INTO pricing_quotes(learner_id,owner_reference,pricing_version,snapshot,created_at,expires_at) SELECT learner_id,owner_reference,pricing_version,snapshot,now()-interval '2 minutes',now()-interval '1 minute' FROM pricing_quotes LIMIT 1 RETURNING id::text`).Scan(&expiredID)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Accept(ctx, owner, expiredID); err == nil {
		t.Fatal("accepted expired quote")
	}
	// A later catalogue/policy changes new quotes; accepted snapshots remain original.
	_, err = st.Pool.Exec(ctx, `INSERT INTO pricing_versions(version,effective_from,approved,config) SELECT 'quote-flip',now(),true,jsonb_set(jsonb_set(jsonb_set(config,'{version}','"quote-flip"'),'{effective_from}',to_jsonb(now())),'{actions,lesson_content,credits}','141') FROM pricing_versions LIMIT 1;
 INSERT INTO pricing_quote_policies(version,policy) SELECT 'quote-flip',policy FROM pricing_quote_policies LIMIT 1`)
	if err != nil {
		t.Fatal(err)
	}
	next, err := service.Create(ctx, owner, pricing.QuoteInput{Action: "lesson_content", NarrationCharacters: 1})
	if err != nil {
		t.Fatal(err)
	}
	if next.ContentCeiling != 141 || next.AudioCeiling != 1 {
		t.Fatalf("bad flipped quote %+v", next)
	}
	old, err := service.Get(ctx, owner, q.ID)
	if err != nil || old.ContentCeiling != 140 || !old.Accepted {
		t.Fatalf("lost terms %+v %v", old, err)
	}
	for _, sql := range []string{"UPDATE pricing_quotes SET snapshot='{}'", "DELETE FROM pricing_quotes", "TRUNCATE pricing_quotes"} {
		if _, err = st.Pool.Exec(ctx, sql); err == nil {
			t.Fatal("mutable quote")
		}
	}
	if _, err = st.Pool.Exec(ctx, "DELETE FROM learners WHERE id=$1::uuid", owner); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = st.Pool.QueryRow(ctx, "SELECT count(*) FROM pricing_quotes WHERE learner_id IS NULL").Scan(&count); err != nil || count != 3 {
		t.Fatal("deletion removed financial history", err, count)
	}
}
