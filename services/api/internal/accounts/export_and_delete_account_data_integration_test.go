//go:build integration

package accounts_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestExportDeleteCoverageRetentionAndRestoreTombstone(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("privacy"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	db, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mail := new(accounts.MailSink)
	auth, err := accounts.New(db.Pool, strings.Repeat("test-key", 5), mail)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path string, body any, token string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req := httptest.NewRequestWithContext(ctx, method, path, strings.NewReader(string(b)))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		auth.ServeHTTP(w, req)
		return w
	}
	email := "privacy@example.test"
	if w := call("POST", "/v1/auth/code", map[string]any{"email": email, "age_confirmed": true}, ""); w.Code != 202 {
		t.Fatalf("code: %d %s", w.Code, w.Body)
	}
	w := call("POST", "/v1/auth/verify", map[string]any{"email": email, "code": mail.Code(email), "age_confirmed": true}, "")
	if w.Code != 200 {
		t.Fatalf("verify: %d %s", w.Code, w.Body)
	}
	var tokens accounts.Tokens
	if err = json.Unmarshal(w.Body.Bytes(), &tokens); err != nil {
		t.Fatal(err)
	}
	var learnerID string
	if err = db.Pool.QueryRow(ctx, "SELECT learner_id::text FROM auth_identities WHERE email=$1", email).Scan(&learnerID); err != nil {
		t.Fatal(err)
	}
	_, err = db.Pool.Exec(ctx, `INSERT INTO auth_provider_identities(provider,subject,learner_id,verified_email) VALUES('google','private-subject',$1::uuid,$2)`, learnerID, email)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference) VALUES($1::uuid,80,'purchase','fixture-payment')`, learnerID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Pool.Exec(ctx, `INSERT INTO pricing_quotes(learner_id,owner_reference,pricing_version,snapshot,created_at,expires_at) SELECT $1::uuid,'fixture-pseudonym',version,'{"credits":12}'::jsonb,now(),now()+interval '10 minutes' FROM pricing_quote_policies LIMIT 1`, learnerID)
	if err != nil {
		t.Fatal(err)
	}

	// Every table with a direct learner reference must have an export/deletion classification.
	var linked []string
	rows, err := db.Pool.Query(ctx, `SELECT DISTINCT tc.table_name FROM information_schema.table_constraints tc JOIN information_schema.key_column_usage kcu USING(constraint_catalog,constraint_schema,constraint_name,table_name) JOIN information_schema.constraint_column_usage ccu USING(constraint_catalog,constraint_schema,constraint_name) WHERE tc.constraint_type='FOREIGN KEY' AND tc.table_schema='public' AND ccu.table_name='learners' AND ccu.column_name='id' ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err = rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		linked = append(linked, n)
	}
	if len(linked) == 0 {
		t.Fatal("expected foreign key tables referencing learners")
	}
	classified := accounts.ClassifiedLearnerTables
	for _, name := range linked {
		if _, ok := classified[name]; !ok {
			t.Errorf("learner-linked table %s has no export/deletion policy", name)
		}
	}

	export := call("GET", "/v1/me/data-export", nil, tokens.AccessToken)
	if export.Code != 200 {
		t.Fatalf("export: %d %s", export.Code, export.Body)
	}
	var payload map[string]json.RawMessage
	if err = json.Unmarshal(export.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	var exportedTables map[string]json.RawMessage
	_ = json.Unmarshal(payload["tables"], &exportedTables)
	var providerRows []map[string]any
	_ = json.Unmarshal(exportedTables["auth_provider_identities"], &providerRows)
	if len(providerRows) != 1 || providerRows[0]["subject"] != nil {
		t.Fatalf("unsafe/missing provider export: %s", exportedTables["auth_provider_identities"])
	}
	for _, name := range []string{"learners", "learner_profiles", "auth_identities", "auth_provider_identities", "auth_sessions", "credit_entries", "pricing_quotes"} {
		if len(exportedTables[name]) == 0 || string(exportedTables[name]) == "null" {
			t.Fatalf("export missing %s", name)
		}
	}
	for _, forbidden := range []string{"refresh_token", "code_hash", "state_hash", "nonce_hash", "token_hash", "private-subject"} {
		if strings.Contains(export.Body.String(), forbidden) {
			t.Fatalf("export exposed %q", forbidden)
		}
	}
	if w = call("POST", "/v1/me/deletion-request", map[string]string{"confirmation": "no"}, tokens.AccessToken); w.Code != 400 {
		t.Fatalf("unconfirmed delete status=%d", w.Code)
	}
	if w = call("POST", "/v1/me/deletion-request", map[string]string{"confirmation": "DELETE"}, tokens.AccessToken); w.Code != 200 {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	var entryLearner any
	var owner string
	if err = db.Pool.QueryRow(ctx, "SELECT learner_id,owner_reference FROM credit_entries WHERE reference='fixture-payment'").Scan(&entryLearner, &owner); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256([]byte(learnerID))
	if entryLearner != nil || owner != hex.EncodeToString(h[:]) {
		t.Fatalf("ledger not pseudonymized: learner=%v owner=%q", entryLearner, owner)
	}
	var tombstone int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM account_deletion_tombstones WHERE learner_reference=$1", owner).Scan(&tombstone); err != nil || tombstone != 1 {
		t.Fatalf("tombstone=%d err=%v", tombstone, err)
	}
	if w = call("GET", "/v1/me", nil, tokens.AccessToken); w.Code != 401 {
		t.Fatalf("deleted access still valid: %d", w.Code)
	}
	if _, err = db.Pool.Exec(ctx, `INSERT INTO learners(id,display_name) VALUES($1::uuid,'Restored private account')`, learnerID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, `INSERT INTO auth_identities(email,learner_id) VALUES($1,$2::uuid)`, email, learnerID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, `INSERT INTO learner_profiles(learner_id) VALUES($1::uuid)`, learnerID); err != nil {
		t.Fatal(err)
	}
	if err = accounts.ApplyDeletionTombstones(ctx, db.Pool); err != nil {
		t.Fatal(err)
	}
	var restoredPersonal int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM learners WHERE id=$1::uuid", learnerID).Scan(&restoredPersonal); err != nil || restoredPersonal != 0 {
		t.Fatalf("restore did not replay tombstone: count=%d err=%v", restoredPersonal, err)
	}
}
