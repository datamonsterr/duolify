//go:build integration

package social

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

type capture struct {
	mu     sync.Mutex
	events []map[string]any
}

func (c *capture) Capture(id, name string, props map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, map[string]any{"id": id, "name": name, "props": props})
	return nil
}
func (*capture) Close() error { return nil }
func (c *capture) named(name string) []map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []map[string]any
	for _, e := range c.events {
		if e["name"] == name {
			out = append(out, e)
		}
	}
	return out
}

func TestUsernameCanonicalUniquenessProjectionAndHold(t *testing.T) {
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("duolify"), postgres.WithUsername("duolify"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	mail := &accounts.MailSink{}
	auth, err := accounts.New(db.Pool, "social-test-secret-at-least-32-bytes!!", mail)
	if err != nil {
		t.Fatal(err)
	}
	sink := &capture{}
	svc := New(db.Pool, analytics.WithConsent(sink, analytics.DatabaseConsent(db.Pool)))
	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{Accounts: auth, Social: svc}, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()
	call := func(method, path, body, token string) (int, map[string]any) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	login := func(email string) (string, string) {
		t.Helper()
		if status, _ := call("POST", "/v1/auth/code", `{"email":"`+email+`","age_confirmed":true}`, ""); status != 202 {
			t.Fatal(status)
		}
		status, out := call("POST", "/v1/auth/verify", `{"email":"`+email+`","age_confirmed":true,"code":"`+mail.Code(email)+`"}`, "")
		if status != 200 {
			t.Fatal(status, out)
		}
		var id string
		if err = db.Pool.QueryRow(ctx, "SELECT learner_id::text FROM auth_identities WHERE email=$1", email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return out["access_token"].(string), id
	}
	aToken, aID := login("a@example.test")
	bToken, bID := login("b@example.test")
	cToken, _ := login("c@example.test")
	if _, err = db.Pool.Exec(ctx, `UPDATE learner_profiles SET analytics_opt_out=false`); err != nil {
		t.Fatal(err)
	}
	claim := func(token, name string) (int, map[string]any) {
		return call("PUT", "/v1/social/username", `{"username":"`+name+`"}`, token)
	}

	if status, _ := call("GET", "/v1/social/identity", "", ""); status != 401 {
		t.Fatalf("anonymous identity: %d", status)
	}
	// Two accounts race for the same name in different cases: exactly one wins.
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	codes := make(chan string, 2)
	start := make(chan struct{})
	for token, name := range map[string]string{aToken: "Maya.Learns", bToken: "maya.LEARNS"} {
		wg.Go(func() {
			<-start
			status, out := claim(token, name)
			statuses <- status
			if c, ok := out["code"].(string); ok {
				codes <- c
			}
		})
	}
	close(start)
	wg.Wait()
	close(statuses)
	close(codes)
	wins, losses := 0, 0
	for s := range statuses {
		switch s {
		case 200:
			wins++
		case 409:
			losses++
		default:
			t.Fatalf("unexpected status %d", s)
		}
	}
	if wins != 1 || losses != 1 || <-codes != "username_taken" {
		t.Fatalf("race: wins=%d losses=%d", wins, losses)
	}
	var rows int
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM learner_usernames WHERE canonical='maya.learns'`).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("canonical unique index: %d %v", rows, err)
	}
	// The DB itself refuses a duplicate canonical form, independent of the API.
	if _, err = db.Pool.Exec(ctx, `INSERT INTO learner_usernames(learner_id,username) VALUES($1,'MAYA.learns')`, cIDOf(ctx, t, db, "c@example.test")); err == nil {
		t.Fatal("unique index allowed a case duplicate")
	}

	winner, winnerID, loser, loserToken := aToken, aID, bToken, bToken
	var owner string
	if err = db.Pool.QueryRow(ctx, `SELECT learner_id::text FROM learner_usernames WHERE canonical='maya.learns'`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if owner == bID {
		winner, winnerID, loser, loserToken = bToken, bID, aToken, aToken
	}
	_ = loser

	// Display case is kept and own identity reads back.
	status, out := call("GET", "/v1/social/identity", "", winner)
	id, _ := out["username"].(map[string]any)
	if status != 200 || id == nil || (id["username"] != "Maya.Learns" && id["username"] != "maya.LEARNS") {
		t.Fatalf("identity: %d %v", status, out)
	}
	// Only letters win: reserved, profanity-config, confusable and malformed names are refused.
	for name, want := range map[string]string{"Support": "username_reserved", "supp0rt": "username_reserved", "su.pport": "username_reserved", "ab": "username_invalid", "9lives": "username_invalid", "bad__name": "username_invalid", "trail.": "username_invalid", "has space": "username_invalid", "MAYA.learns": "username_taken", "Ｍaya.Learns": "username_taken"} {
		if status, out = claim(cToken, name); status < 400 || out["code"] != want {
			t.Fatalf("%q: %d %v want %s", name, status, out, want)
		}
	}
	if _, err = db.Pool.Exec(ctx, `UPDATE username_config SET profanity=ARRAY['heck']`); err != nil {
		t.Fatal(err)
	}
	if status, out = claim(cToken, "Oh_h3ck_no"); status != 422 || out["code"] != "username_not_allowed" {
		t.Fatalf("configured profanity: %d %v", status, out)
	}
	// Lists are configuration, not code: a newly reserved word applies immediately.
	if _, err = db.Pool.Exec(ctx, `UPDATE username_config SET reserved=reserved||ARRAY['teacher']`); err != nil {
		t.Fatal(err)
	}
	if status, out = claim(cToken, "Teacher"); status != 422 || out["code"] != "username_reserved" {
		t.Fatalf("configured reserved: %d %v", status, out)
	}
	// One username per learner for this ticket (rename is E6-02).
	if status, out = claim(winner, "Another.Name"); status != 409 || out["code"] != "username_already_set" {
		t.Fatalf("second claim: %d %v", status, out)
	}

	// Public projection exposes only username, display name, avatar and permitted streak.
	if _, err = db.Pool.Exec(ctx, `UPDATE learner_profiles SET goal='private goal',topic='private topic' WHERE learner_id=$1`, winnerID); err != nil {
		t.Fatal(err)
	}
	status, out = call("GET", "/v1/social/profiles/MAYA.LEARNS", "", cToken)
	if status != 200 {
		t.Fatalf("profile: %d %v", status, out)
	}
	for key := range out {
		if key != "username" && key != "display_name" && key != "avatar" && key != "streak" {
			t.Fatalf("projection leaked %q: %v", key, out)
		}
	}
	if _, has := out["streak"]; has {
		t.Fatal("streak shown to a non-friend")
	}
	raw, _ := json.Marshal(out)
	for _, secret := range []string{"private goal", "private topic", "example.test"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("projection leaked %q", secret)
		}
	}
	if status, out = call("GET", "/v1/social/profiles/nobody.here", "", cToken); status != 404 || out["error"] == nil {
		t.Fatalf("unknown profile: %d %v", status, out)
	}
	if status, _ = call("GET", "/v1/social/profiles/nobody.here", "", ""); status != 401 {
		t.Fatalf("anonymous profile: %d", status)
	}

	// Analytics: reason codes and lengths only, learner UUID, opt-out respected.
	set, rejected := sink.named("username_set"), sink.named("username_rejected")
	if len(set) != 1 || set[0]["id"] != winnerID || set[0]["props"].(map[string]any)["username_length"] != 11 {
		t.Fatalf("username_set: %v", set)
	}
	if len(rejected) < 8 {
		t.Fatalf("username_rejected: %d", len(rejected))
	}
	rawEvents, _ := json.Marshal(sink.events)
	for _, text := range []string{"maya", "Maya", "support", "Support", "supp0rt", "heck"} {
		if strings.Contains(string(rawEvents), text) {
			t.Fatalf("analytics carried learner text %q: %s", text, rawEvents)
		}
	}
	before := len(sink.events)
	if _, err = db.Pool.Exec(ctx, `UPDATE learner_profiles SET analytics_opt_out=true`); err != nil {
		t.Fatal(err)
	}
	claim(cToken, "Quiet.One")
	claim(cToken, "admin")
	if len(sink.events) != before {
		t.Fatal("opted-out learner produced analytics")
	}
	_ = loserToken

	// Deleting the account releases the mapping but holds the name for 30 days.
	if _, err = db.Pool.Exec(ctx, `DELETE FROM learners WHERE id=$1`, winnerID); err != nil {
		t.Fatal(err)
	}
	var held int
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM username_holds WHERE canonical='maya.learns' AND held_until>now()+interval '29 days' AND previous_owner_ref=encode(sha256(convert_to($1::text,'UTF8')),'hex')`, winnerID).Scan(&held); err != nil || held != 1 {
		t.Fatalf("hold missing: %d %v", held, err)
	}
	other := bToken
	if winner == bToken {
		other = aToken
	}
	if status, out = claim(other, "Maya.Learns"); status != 409 || out["code"] != "username_taken" {
		t.Fatalf("held name claimable: %d %v", status, out)
	}
	// Once the hold lapses the name is free; expiry is evaluated, not manually purged.
	if _, err = db.Pool.Exec(ctx, `UPDATE username_holds SET held_until=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	if status, out = claim(other, "Maya.Learns"); status != 200 {
		t.Fatalf("expired hold still blocks: %d %v", status, out)
	}
}

func cIDOf(ctx context.Context, t *testing.T, db *store.Store, email string) string {
	t.Helper()
	var id string
	if err := db.Pool.QueryRow(ctx, "SELECT learner_id::text FROM auth_identities WHERE email=$1", email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
