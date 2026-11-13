//go:build integration

package player_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/datamonsterr/duolify/services/api/internal/player/playerfixture"
)

func TestSynchronizeOfflinePlaybackProgressOnceIntegration(t *testing.T) {
	f := backuptest.New(t)
	f.Seed(t)
	// Exercise the real constraint migration against populated legacy rows.
	tx, err := f.DB.Pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(t.Context()) }()
	var before, after string
	const snapshot = "SELECT coalesce(jsonb_agg(to_jsonb(e) ORDER BY action_key), '[]'::jsonb)::text FROM lesson_playback_sync_events e"
	if err := tx.QueryRow(t.Context(), snapshot).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(t.Context(), `ALTER TABLE lesson_playback_sync_events DROP CONSTRAINT lesson_playback_sync_events_pkey; ALTER TABLE lesson_playback_sync_events ADD PRIMARY KEY(action_key)`); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../migrations/20261110190000_player_sync_owner_keys.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(t.Context(), strings.Split(string(migration), "-- +goose Down")[0]); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(t.Context(), snapshot).Scan(&after); err != nil || before != after || !strings.Contains(after, "act-11111111") {
		t.Fatalf("migration changed legacy events: %s => %s (%v)", before, after, err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Fixture positions predate the two explicitly ordered progress events.
	if _, err := f.DB.Pool.Exec(t.Context(), "UPDATE lesson_playback_positions SET occurred_at=now()-interval '1 hour'"); err != nil {
		t.Fatal(err)
	}
	mail := new(accounts.MailSink)
	auth, err := accounts.New(f.DB.Pool, strings.Repeat("sync-cursor", 4), mail)
	if err != nil {
		t.Fatal(err)
	}
	handler := func() http.Handler {
		return httpapi.New(httpapi.Dependencies{
			Accounts: auth,
			Player: player.New(player.Options{
				Pool:        f.DB.Pool,
				Objects:     f.ApplicationObjects(),
				AssetOrigin: "https://assets.example.test",
			}),
		}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}
	server := httptest.NewServer(handler())
	defer server.Close()

	call := func(method, path, token, body string) (int, []byte) {
		req, _ := http.NewRequestWithContext(t.Context(), method, server.URL+path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = response.Body.Close() }()
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, raw
	}

	login := func(email string) string {
		input := `{"email":"` + email + `","age_confirmed":true}`
		if code, raw := call("POST", "/v1/auth/code", "", input); code != 202 {
			t.Fatal(code, string(raw))
		}
		code, raw := call("POST", "/v1/auth/verify", "", `{"email":"`+email+`","age_confirmed":true,"code":"`+mail.Code(email)+`"}`)
		var token struct {
			Access string `json:"access_token"`
		}
		if err := json.Unmarshal(raw, &token); err != nil || code != 200 {
			t.Fatal(code, string(raw), err)
		}
		return token.Access
	}

	owner := login("synthetic@example.test")
	other := login("other-sync@example.test")
	const lessonID = "f3333333-3333-4333-8333-333333333333"
	const syncPath = "/v1/lessons/" + lessonID + "/sync"
	const posPath = "/v1/lessons/" + lessonID + "/position"

	t0 := time.Now().UTC().Add(-2 * time.Minute).Format(time.RFC3339)
	t1 := time.Now().UTC().Add(-1 * time.Minute).Format(time.RFC3339)

	// 1. Authorization checks
	event1 := fmt.Sprintf(`{"action_key":"act-unique-001","version":1,"slide_id":"sky-1","time_ms":1000,"occurred_at":"%s"}`, t0)
	if code, raw := call("POST", syncPath, "", event1); code != 401 {
		t.Fatalf("expected 401 unauthenticated, got %d: %s", code, raw)
	}
	if code, raw := call("POST", syncPath, other, event1); code != 404 {
		t.Fatalf("expected 404 unenrolled, got %d: %s", code, raw)
	}

	// 2. Out-of-order merge:
	// Synchronize event2 (later occurred_at t1 on sky-2) FIRST
	event2 := fmt.Sprintf(`{"action_key":"act-unique-002","version":1,"slide_id":"sky-2","time_ms":1500,"occurred_at":"%s"}`, t1)
	if code, raw := call("POST", syncPath, owner, event2); code != 200 {
		t.Fatalf("sync event2 failed, got %d: %s", code, raw)
	}

	// Verify position is now at sky-2
	code, raw := call("GET", posPath, owner, "")
	if code != 200 || !bytes.Contains(raw, []byte(`"slide_id":"sky-2"`)) || !bytes.Contains(raw, []byte(`"time_ms":1500`)) {
		t.Fatalf("expected position at sky-2 after event2, got %d: %s", code, raw)
	}

	// Now synchronize event1 (earlier occurred_at t0 on sky-1) SECOND (out of order)
	if code, raw := call("POST", syncPath, owner, event1); code != 200 {
		t.Fatalf("sync event1 failed, got %d: %s", code, raw)
	}

	// Verify position is STILL at sky-2 (out-of-order event did not regress position)
	code, raw = call("GET", posPath, owner, "")
	if code != 200 || !bytes.Contains(raw, []byte(`"slide_id":"sky-2"`)) || !bytes.Contains(raw, []byte(`"time_ms":1500`)) {
		t.Fatalf("position regressed after earlier event1, got %d: %s", code, raw)
	}

	// Verify both events exist in lesson_playback_sync_events table
	var count int
	if err := f.DB.Pool.QueryRow(t.Context(), "SELECT count(*) FROM lesson_playback_sync_events WHERE action_key LIKE 'act-unique-%'").Scan(&count); err != nil || count != 2 {
		t.Fatalf("expected 2 unique sync events in db, got %d (err: %v)", count, err)
	}

	// 3. Postgres event uniqueness & idempotent replay:
	// Resubmitting event2 with the same action_key returns 200 but does not insert duplicate
	if code, raw := call("POST", syncPath, owner, event2); code != 200 {
		t.Fatalf("idempotent resubmit failed, got %d: %s", code, raw)
	}
	if err := f.DB.Pool.QueryRow(t.Context(), "SELECT count(*) FROM lesson_playback_sync_events WHERE action_key LIKE 'act-unique-%'").Scan(&count); err != nil || count != 2 {
		t.Fatalf("duplicate event was inserted, count: %d (err: %v)", count, err)
	}

	// 4. Foreign event / action key owned by another learner rejection
	// If another learner calls sync for owner's lesson, it is rejected (404 foreign lesson)
	if code, raw := call("POST", syncPath, other, event1); code != 404 && code != 403 && code != 400 {
		t.Fatalf("expected foreign lesson rejection, got %d: %s", code, raw)
	}

	// Now publish a lesson for 'other', and let 'other' try to submit 'act-unique-001' (owned by 'owner')
	var otherUUID string
	if err := f.DB.Pool.QueryRow(t.Context(), `SELECT l.id::text FROM learners l JOIN auth_identities a ON a.learner_id=l.id WHERE a.email='other-sync@example.test'`).Scan(&otherUUID); err != nil {
		t.Fatal(err)
	}
	otherLessonID, err := playerfixture.Seed(t.Context(), f.DB.Pool, f.ApplicationObjects(), otherUUID)
	if err != nil {
		t.Fatal(err)
	}
	foreignSyncPath := "/v1/lessons/" + otherLessonID + "/sync"
	// Identical keys are independent between learners, without key-existence errors.
	if code, raw := call("POST", foreignSyncPath, other, event1); code != 200 || !bytes.Contains(raw, []byte(`"acked_keys":["act-unique-001"]`)) {
		t.Fatalf("expected independently acknowledged key, got %d: %s", code, raw)
	}
	if err := f.DB.Pool.QueryRow(t.Context(), "SELECT count(*) FROM lesson_playback_sync_events WHERE action_key='act-unique-001'").Scan(&count); err != nil || count != 2 {
		t.Fatalf("expected both learners' events, count=%d err=%v", count, err)
	}

	// 5. Malformed events fail
	for _, malformed := range []string{
		`{"action_key":"","version":1,"slide_id":"sky-1","time_ms":500,"occurred_at":"` + t1 + `"}`,
		`{"action_key":"bad key!","version":1,"slide_id":"sky-1","time_ms":500,"occurred_at":"` + t1 + `"}`,
		`{"action_key":"act-m-1","version":2,"slide_id":"sky-1","time_ms":500,"occurred_at":"` + t1 + `"}`,
		`{"action_key":"act-m-2","version":1,"slide_id":"nonexistent","time_ms":500,"occurred_at":"` + t1 + `"}`,
		`{"action_key":"act-m-3","version":1,"slide_id":"sky-1","time_ms":-5,"occurred_at":"` + t1 + `"}`,
		`{"action_key":"act-m-4","version":1,"slide_id":"sky-1","time_ms":3001,"occurred_at":"` + t1 + `"}`,
		`{"action_key":"act-m-5","version":1,"slide_id":"sky-1","time_ms":500,"occurred_at":"invalid-date"}`,
		`{"action_key":"act-m-6","version":1,"slide_id":"sky-1","time_ms":500,"occurred_at":"` + t1 + `","completed":true}`,
	} {
		if code, raw := call("POST", syncPath, owner, `{"events":[`+malformed+`]}`); code != 200 || !bytes.Contains(raw, []byte(`"acked_keys":[]`)) || !bytes.Contains(raw, []byte(`"rejected_keys":[`)) {
			t.Fatalf("expected individual rejection for %s, got %d: %s", malformed, code, raw)
		}
	}

	// A wrong JSON type, unknown field, or invalid event must not poison good events.
	batch := `{"events":[{"action_key":"bad-type","time_ms":"wrong"},{"action_key":"bad-extra","completed":true},` + strings.Replace(event1, "act-unique-001", "good-batch", 1) + `]}`
	if code, raw := call("POST", syncPath, owner, batch); code != 200 || !bytes.Contains(raw, []byte(`"acked_keys":["good-batch"]`)) || !bytes.Contains(raw, []byte(`"rejected_keys":["bad-type","bad-extra"]`)) {
		t.Fatalf("mixed batch did not isolate invalid events: %d %s", code, raw)
	}
	if code, raw := call("POST", syncPath, owner, strings.Repeat(" ", 65537)); code != 413 {
		t.Fatalf("expected 413 for oversized request, got %d: %s", code, raw)
	}
	for i, timestamp := range []string{"9999-01-01T00:00:00Z", "0001-01-01T00:00:00Z"} {
		key := fmt.Sprintf("clamped-%d", i)
		event := fmt.Sprintf(`{"action_key":"%s","version":1,"slide_id":"sky-1","time_ms":1,"occurred_at":"%s"}`, key, timestamp)
		before := time.Now().UTC()
		if code, raw := call("POST", syncPath, owner, event); code != 200 {
			t.Fatalf("clamp: %d %s", code, raw)
		}
		var occurred time.Time
		if err := f.DB.Pool.QueryRow(t.Context(), "SELECT occurred_at FROM lesson_playback_sync_events WHERE action_key=$1", key).Scan(&occurred); err != nil {
			t.Fatal(err)
		}
		if occurred.After(before.Add(2*time.Minute+time.Second)) || occurred.Before(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("unbounded timestamp: %v", occurred)
		}
	}

	// 6. Data export and deletion tombstone
	if code, raw := call("GET", "/v1/me/data-export", owner, ""); code != 200 || !bytes.Contains(raw, []byte("lesson_playback_sync_events")) {
		t.Fatalf("expected lesson_playback_sync_events in export, got %d", code)
	}

	var learnerUUID string
	if err := f.DB.Pool.QueryRow(t.Context(), `SELECT l.id::text FROM learners l JOIN auth_identities a ON a.learner_id=l.id WHERE a.email='synthetic@example.test'`).Scan(&learnerUUID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.DB.Pool.Exec(t.Context(), "INSERT INTO account_deletion_tombstones(learner_reference) VALUES($1)", accounts.LearnerReference(learnerUUID)); err != nil {
		t.Fatal(err)
	}
	if err := accounts.ApplyDeletionTombstones(t.Context(), f.DB.Pool); err != nil {
		t.Fatal(err)
	}
	if err := f.DB.Pool.QueryRow(t.Context(), "SELECT count(*) FROM lesson_playback_sync_events WHERE learner_id=$1::uuid", learnerUUID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("expected 0 sync events after deletion tombstone, got %d", count)
	}
}
