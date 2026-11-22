//go:build integration

package accounts_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/accountstest"
	"github.com/datamonsterr/duolify/services/api/internal/backup"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// scriptedDeleter is a fake Clerk client: it fails while `down` is set and records each call.
type scriptedDeleter struct {
	mu    sync.Mutex
	down  bool
	calls []string
}

func (s *scriptedDeleter) DeleteUser(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, id)
	if s.down {
		return errors.New("clerk unreachable")
	}
	return nil
}

func TestDeleteClerkUserAfterAccountErasure(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("clerkerase"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	ledger := accountstest.NewDeletionLedger()
	auth.ConfigureDeletionLedger(ledger)
	auth.ConfigureWebOrigins([]string{webOrigin})
	call := func(method, path string, body any, token string, web bool) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req := httptest.NewRequestWithContext(ctx, method, path, strings.NewReader(string(b)))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if web {
			req.Header.Set(accounts.ClientHeader, accounts.ClientWeb)
		}
		w := httptest.NewRecorder()
		auth.ServeHTTP(w, req)
		return w
	}
	signUp := func(email, clerkID string) string {
		t.Helper()
		if w := call("POST", "/v1/auth/code", map[string]any{"email": email, "age_confirmed": true}, "", false); w.Code != 202 {
			t.Fatal(w.Code, w.Body)
		}
		w := call("POST", "/v1/auth/verify", map[string]any{"email": email, "code": mail.Code(email), "age_confirmed": true}, "", false)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body)
		}
		var tokens accounts.Tokens
		if err := json.Unmarshal(w.Body.Bytes(), &tokens); err != nil {
			t.Fatal(err)
		}
		if clerkID != "" {
			if _, err := db.Pool.Exec(ctx, `INSERT INTO auth_provider_identities(provider,subject,learner_id,verified_email) SELECT 'clerk',$2,learner_id,$1 FROM auth_identities WHERE email=$1`, email, clerkID); err != nil {
				t.Fatal(err)
			}
		}
		return tokens.AccessToken
	}
	erase := func(token string, web bool) *httptest.ResponseRecorder {
		t.Helper()
		w := call("POST", "/v1/me/deletion-request", map[string]string{"confirmation": "DELETE"}, token, web)
		if w.Code != 200 {
			t.Fatal("erase:", w.Code, w.Body)
		}
		return w
	}
	queued := func() []string {
		t.Helper()
		rows, err := db.Pool.Query(ctx, "SELECT clerk_user_id FROM clerk_user_deletion_requests ORDER BY clerk_user_id")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, id)
		}
		return ids
	}
	learners := func() int {
		var n int
		if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM learners").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// Erasure of local data never depends on Clerk: the learner and the Clerk binding are gone
	// while Clerk is down, and the request to delete the Clerk user is queued, not attempted.
	clerk := &scriptedDeleter{down: true}
	eraser := accounts.NewClerkUserEraser(db.Pool, clerk)
	w := erase(signUp("sso@example.test", "user_clerk_one"), true)
	if learners() != 0 {
		t.Fatal("learner survived erasure")
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body["clerk_user_deletion_queued"] != true {
		t.Fatal("response does not record the queued Clerk deletion", w.Body, err)
	}
	if strings.Contains(w.Body.String(), "user_clerk_one") {
		t.Fatal("Clerk user id leaked into the response")
	}
	if len(clerk.calls) != 0 {
		t.Fatal("the learner's request waited on Clerk")
	}
	if got := queued(); len(got) != 1 || got[0] != "user_clerk_one" {
		t.Fatal("not queued:", got)
	}
	// Web erasure also clears the refresh cookie (task 1).
	if c := w.Header().Get("Set-Cookie"); !strings.Contains(c, "Max-Age=0") {
		t.Fatal("deletion kept the cookie:", c)
	}

	// Clerk unreachable: the request stays queued and its attempts are counted.
	if n, err := eraser.ProcessClerkUserDeletions(ctx); n != 0 || err == nil || strings.Contains(err.Error(), "user_clerk_one") {
		t.Fatal("failure not reported without the id:", n, err)
	}
	var attempts int
	if err := db.Pool.QueryRow(ctx, "SELECT attempts FROM clerk_user_deletion_requests").Scan(&attempts); err != nil || attempts != 1 {
		t.Fatal("attempt not counted", attempts, err)
	}

	// Advance the retry clock past backoff, then recover Clerk.
	if _, err := db.Pool.Exec(ctx, "UPDATE clerk_user_deletion_requests SET last_attempt_at=now()-interval '1 hour'"); err != nil {
		t.Fatal(err)
	}
	// Clerk back: the retry deletes the user once and drains the queue; repeating is a no-op.
	clerk.mu.Lock()
	clerk.down = false
	clerk.mu.Unlock()
	if n, err := eraser.ProcessClerkUserDeletions(ctx); n != 1 || err != nil {
		t.Fatal(n, err)
	}
	if len(queued()) != 0 {
		t.Fatal("queue not drained")
	}
	before := len(clerk.calls)
	if n, err := eraser.ProcessClerkUserDeletions(ctx); n != 0 || err != nil || len(clerk.calls) != before {
		t.Fatal("repeat run was not a no-op", n, err)
	}

	// A learner without a Clerk binding (email-code only) queues nothing.
	erase(signUp("email-only@example.test", ""), false)
	if len(queued()) != 0 {
		t.Fatal("queued a Clerk deletion for an email-only learner:", queued())
	}

	// No Clerk configured: requests wait, and Clerk is never called.
	erase(signUp("later@example.test", "user_clerk_two"), false)
	disabled := accounts.NewClerkUserEraser(db.Pool, nil)
	if disabled.Enabled() {
		t.Fatal("eraser enabled without Clerk")
	}
	if n, err := disabled.ProcessClerkUserDeletions(ctx); n != 0 || err != nil {
		t.Fatal(n, err)
	}
	if got := queued(); len(got) != 1 {
		t.Fatal("request lost while Clerk was off:", got)
	}
	if n, err := eraser.ProcessClerkUserDeletions(ctx); n != 1 || err != nil {
		t.Fatal("not processed once Clerk was configured", n, err)
	}

	// Restoring a backup re-applies a tombstone and must queue the Clerk deletion as well.
	restoredEmail := "restored@example.test"
	signUp(restoredEmail, "user_clerk_three")
	var learnerID string
	if err := db.Pool.QueryRow(ctx, "SELECT learner_id::text FROM auth_identities WHERE email=$1", restoredEmail).Scan(&learnerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO account_deletion_tombstones(learner_reference) VALUES($1)", accounts.LearnerReference(learnerID)); err != nil {
		t.Fatal(err)
	}
	if err := accounts.ApplyDeletionTombstones(ctx, db.Pool); err != nil {
		t.Fatal(err)
	}
	if got := queued(); len(got) != 1 || got[0] != "user_clerk_three" {
		t.Fatal("tombstone replay did not queue the Clerk deletion:", got)
	}
	t.Run("external provider intent failure does not acknowledge erasure", func(t *testing.T) {
		token := signUp("failed-intent@example.test", "user_failed_intent")
		auth.ConfigureDeletionLedger(failingClerkLedger{ledger})
		w := call("POST", "/v1/me/deletion-request", map[string]string{"confirmation": "DELETE"}, token, false)
		if w.Code != 503 {
			t.Fatal("accepted erasure without recoverable provider intent", w.Code, w.Body)
		}
		if w := call("GET", "/v1/me", nil, token, false); w.Code != 200 {
			t.Fatal("SQL erasure committed without provider intent", w.Code)
		}
		auth.ConfigureDeletionLedger(ledger)
		erase(token, false)
	})
	t.Run("restore before Clerk linking preserves external deletion", func(t *testing.T) {
		token := signUp("pre-link@example.test", "user_after_backup")
		var learnerID string
		if err := db.Pool.QueryRow(ctx, "SELECT learner_id::text FROM auth_identities WHERE email='pre-link@example.test'").Scan(&learnerID); err != nil {
			t.Fatal(err)
		}
		erase(token, false)
		if _, err := db.Pool.Exec(ctx, "DELETE FROM clerk_user_deletion_requests"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool.Exec(ctx, "INSERT INTO learners(id,display_name) VALUES($1::uuid,'Restored')", learnerID); err != nil {
			t.Fatal(err)
		}
		if err := accounts.ReplayDeletionLedger(ctx, db.Pool, ledger); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, id := range queued() {
			if id == "user_after_backup" {
				found = true
			}
		}
		if !found {
			t.Error("accepted Clerk erasure lost across restore before linking")
		}
		if _, err := db.Pool.Exec(ctx, "DELETE FROM clerk_user_deletion_requests"); err != nil {
			t.Fatal(err)
		}
		if err := accounts.ReplayDeletionLedger(ctx, db.Pool, ledger); err != nil {
			t.Fatal(err)
		}
		found = false
		for _, id := range queued() {
			if id == "user_after_backup" {
				found = true
			}
		}
		if !found {
			t.Error("Clerk erasure needs a restored learner")
		}
	})
	t.Run("pending Clerk identity cannot be reused", func(t *testing.T) {
		if _, err := db.Pool.Exec(ctx, "INSERT INTO clerk_user_deletion_requests(clerk_user_id) VALUES('user_after_backup') ON CONFLICT DO NOTHING"); err != nil {
			t.Fatal(err)
		}
		auth.ConfigureClerk(clerkFixture{"erased": {UserID: "user_after_backup", Email: "new@example.test", EmailVerified: true}}, "pk_test_fixture")
		start := call("POST", "/v1/auth/sso/start", map[string]any{"redirect_uri": "duolify://sso/callback", "code_challenge": accounts.PKCEChallenge(strings.Repeat("v", 43)), "age_confirmed": true}, "", false)
		var attempt accounts.SSOTransaction
		if start.Code != 200 || json.Unmarshal(start.Body.Bytes(), &attempt) != nil {
			t.Fatal(start.Code, start.Body)
		}
		w := call("POST", "/v1/auth/sso/handoff", map[string]any{"state": attempt.State, "session_token": "erased"}, "", false)
		if w.Code != 401 {
			t.Error("identity awaiting deletion reused", w.Code, w.Body)
		}
		var n int
		if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM auth_identities WHERE email='new@example.test'").Scan(&n); err != nil || n != 0 {
			t.Error("created identity", n, err)
		}
	})
	t.Run("failed batch does not starve later deletions", func(t *testing.T) {
		if _, err := db.Pool.Exec(ctx, "DELETE FROM clerk_user_deletion_requests"); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 50; i++ {
			if _, err := db.Pool.Exec(ctx, "INSERT INTO clerk_user_deletion_requests(clerk_user_id,requested_at) VALUES($1,now()-interval '1 day')", fmt.Sprintf("bad-%02d", i)); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := db.Pool.Exec(ctx, "INSERT INTO clerk_user_deletion_requests(clerk_user_id) VALUES('healthy')"); err != nil {
			t.Fatal(err)
		}
		fair := accounts.NewClerkUserEraser(db.Pool, selectiveDeleter{})
		for i := 0; i < 20; i++ {
			_, _ = fair.ProcessClerkUserDeletions(ctx)
		}
		var healthy bool
		if err := db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM clerk_user_deletion_requests WHERE clerk_user_id='healthy')").Scan(&healthy); err != nil || healthy {
			t.Fatal("healthy deletion starved", err)
		}
		if len(queued()) != 50 {
			t.Fatal("failed rows were lost", queued())
		}
		var attempts int
		if err := db.Pool.QueryRow(ctx, "SELECT sum(attempts) FROM clerk_user_deletion_requests").Scan(&attempts); err != nil || attempts != 50 {
			t.Fatal("backoff did not defer repeated failures", attempts, err)
		}
	})
	t.Run("down migration refuses pending identifiers", func(t *testing.T) {
		raw, err := os.ReadFile("../../migrations/20261122150000_clerk_user_deletion_queue.sql")
		if err != nil {
			t.Fatal(err)
		}
		_, down, ok := strings.Cut(string(raw), "-- +goose Down")
		if !ok {
			t.Fatal("missing down migration")
		}
		tx, err := db.Pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err = tx.Exec(ctx, down); err == nil {
			t.Fatal("downgrade silently discarded pending erasures")
		}
	})

}

type selectiveDeleter struct{}

func (selectiveDeleter) DeleteUser(_ context.Context, id string) error {
	if strings.HasPrefix(id, "bad-") {
		return errors.New("permanent provider failure")
	}
	return nil
}

type failingClerkLedger struct{ backup.DeletionLedger }

func (failingClerkLedger) AppendClerk(context.Context, string) error {
	return errors.New("independent storage unavailable")
}
