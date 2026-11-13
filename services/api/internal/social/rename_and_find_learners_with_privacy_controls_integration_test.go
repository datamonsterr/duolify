//go:build integration

package social

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

type rig struct {
	t     *testing.T
	ctx   context.Context
	db    *store.Store
	sink  *capture
	mail  *accounts.MailSink
	srv   *httptest.Server
	count int
}

func newRig(t *testing.T) *rig {
	t.Helper()
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("duolify"), postgres.WithUsername("duolify"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err = migrations.Up(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	db, err := store.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	mail := &accounts.MailSink{}
	auth, err := accounts.New(db.Pool, "social-test-secret-at-least-32-bytes!!", mail)
	if err != nil {
		t.Fatal(err)
	}
	sink := &capture{}
	svc := New(db.Pool, analytics.WithConsent(sink, analytics.DatabaseConsent(db.Pool)))
	srv := httptest.NewServer(httpapi.New(httpapi.Dependencies{Accounts: auth, Social: svc}, slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(srv.Close)
	return &rig{t: t, ctx: ctx, db: db, sink: sink, mail: mail, srv: srv}
}

func (r *rig) call(method, path, body, token string) (int, map[string]any, http.Header) {
	r.t.Helper()
	req, err := http.NewRequestWithContext(r.ctx, method, r.srv.URL+path, strings.NewReader(body))
	if err != nil {
		r.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := r.srv.Client().Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out, resp.Header
}

// login creates a learner with analytics enabled and returns its token and ID.
func (r *rig) login(email string) (string, string) {
	r.t.Helper()
	if status, _, _ := r.call("POST", "/v1/auth/code", `{"email":"`+email+`","age_confirmed":true}`, ""); status != 202 {
		r.t.Fatal(status)
	}
	status, out, _ := r.call("POST", "/v1/auth/verify", `{"email":"`+email+`","age_confirmed":true,"code":"`+r.mail.Code(email)+`"}`, "")
	if status != 200 {
		r.t.Fatal(status, out)
	}
	id := cIDOf(r.ctx, r.t, r.db, email)
	if _, err := r.db.Pool.Exec(r.ctx, `UPDATE learner_profiles SET analytics_opt_out=false WHERE learner_id=$1`, id); err != nil {
		r.t.Fatal(err)
	}
	return out["access_token"].(string), id
}

func (r *rig) claim(token, name string) (int, map[string]any) {
	s, o, _ := r.call("PUT", "/v1/social/username", `{"username":"`+name+`"}`, token)
	return s, o
}
func (r *rig) rename(token, name string) (int, map[string]any) {
	s, o, _ := r.call("PATCH", "/v1/social/username", `{"username":"`+name+`"}`, token)
	return s, o
}
func (r *rig) search(token, q string) (int, []map[string]any, http.Header) {
	s, o, h := r.call("GET", "/v1/social/search?q="+url.QueryEscape(q), "", token)
	var list []map[string]any
	if raw, ok := o["results"].([]any); ok {
		for _, item := range raw {
			list = append(list, item.(map[string]any))
		}
	}
	return s, list, h
}
func (r *rig) exec(sql string, args ...any) {
	r.t.Helper()
	if _, err := r.db.Pool.Exec(r.ctx, sql, args...); err != nil {
		r.t.Fatal(err)
	}
}
func (r *rig) nextLearner() string {
	r.count++
	return fmt.Sprintf("seed%d@example.test", r.count)
}

func TestRenameCooldownHoldAndReclaim(t *testing.T) {
	r := newRig(t)
	aToken, aID := r.login("a@example.test")
	bToken, _ := r.login("b@example.test")
	if status, out := r.rename(aToken, "Nope.Yet"); status != 409 || out["code"] != "username_not_set" {
		t.Fatalf("rename without a username: %d %v", status, out)
	}
	if status, _ := r.claim(aToken, "Rena.Old"); status != 200 {
		t.Fatal(status)
	}
	// Renaming to the identical name is a no-op: no cooldown, no event.
	if status, _ := r.rename(aToken, "Rena.Old"); status != 200 {
		t.Fatalf("no-op rename: %d", status)
	}
	var changed *time.Time
	if err := r.db.Pool.QueryRow(r.ctx, `SELECT changed_at FROM learner_usernames`).Scan(&changed); err != nil || changed != nil {
		t.Fatalf("no-op rename touched changed_at: %v %v", changed, err)
	}
	if len(r.sink.named("username_changed")) != 0 {
		t.Fatal("no-op rename emitted an event")
	}
	status, out := r.rename(aToken, "Rena.New")
	if status != 200 || out["username"].(map[string]any)["username"] != "Rena.New" {
		t.Fatalf("rename: %d %v", status, out)
	}
	var sameLearner string
	if err := r.db.Pool.QueryRow(r.ctx, `SELECT learner_id::text FROM learner_usernames WHERE canonical='rena.new'`).Scan(&sameLearner); err != nil || sameLearner != aID {
		t.Fatalf("references must stay learner-id based: %q %v", sameLearner, err)
	}
	// The old name is held for 30 days against its previous owner's hash only.
	var held int
	if err := r.db.Pool.QueryRow(r.ctx, `SELECT count(*) FROM username_holds WHERE canonical='rena.old' AND held_until>now()+interval '29 days' AND previous_owner_ref=encode(sha256(convert_to($1::text,'UTF8')),'hex')`, aID).Scan(&held); err != nil || held != 1 {
		t.Fatalf("rename hold missing: %d %v", held, err)
	}
	events := r.sink.named("username_changed")
	if len(events) != 1 || events[0]["id"] != aID {
		t.Fatalf("username_changed: %v", events)
	}
	if raw, _ := json.Marshal(events[0]["props"]); strings.Contains(strings.ToLower(string(raw)), "rena") {
		t.Fatalf("event leaked the name: %s", raw)
	}

	// Cooldown: refused with the date it ends.
	status, out = r.rename(aToken, "Rena.Third")
	if status != 409 || out["code"] != "username_cooldown" {
		t.Fatalf("cooldown: %d %v", status, out)
	}
	next, err := time.Parse(time.RFC3339, fmt.Sprint(out["next_change_at"]))
	if err != nil || next.Before(time.Now().Add(29*24*time.Hour)) || next.After(time.Now().Add(31*24*time.Hour)) {
		t.Fatalf("next_change_at: %v %v", out["next_change_at"], err)
	}
	if !strings.Contains(fmt.Sprint(out["error"]), next.UTC().Format("January 2, 2006")) {
		t.Fatalf("message must name the date: %v", out["error"])
	}

	// Someone else cannot take the held name; the previous owner can, without waiting.
	if status, out = r.claim(bToken, "rena.OLD"); status != 409 || out["code"] != "username_taken" {
		t.Fatalf("held name claimable: %d %v", status, out)
	}
	if status, out = r.rename(aToken, "Rena.Old"); status != 200 {
		t.Fatalf("previous owner reclaim: %d %v", status, out)
	}
	if err = r.db.Pool.QueryRow(r.ctx, `SELECT count(*) FROM username_holds WHERE canonical='rena.old'`).Scan(&held); err != nil || held != 0 {
		t.Fatalf("reclaimed hold not released: %d %v", held, err)
	}
	if err = r.db.Pool.QueryRow(r.ctx, `SELECT count(*) FROM username_holds WHERE canonical='rena.new'`).Scan(&held); err != nil || held != 1 {
		t.Fatalf("new hold missing after reclaim: %d %v", held, err)
	}
	// Reclaim restarts the cooldown.
	if status, out = r.rename(aToken, "Rena.Third"); status != 409 || out["code"] != "username_cooldown" {
		t.Fatalf("cooldown after reclaim: %d %v", status, out)
	}
	// After the cooldown a case-only change is allowed, writes no hold and starts it again.
	r.exec(`UPDATE learner_usernames SET changed_at=now()-interval '31 days'`)
	if status, out = r.rename(aToken, "RENA.old"); status != 200 {
		t.Fatalf("case change: %d %v", status, out)
	}
	if err = r.db.Pool.QueryRow(r.ctx, `SELECT count(*) FROM username_holds WHERE canonical='rena.old'`).Scan(&held); err != nil || held != 0 {
		t.Fatalf("case-only change wrote a hold: %d %v", held, err)
	}
	if status, out = r.rename(aToken, "Rena.Third"); status != 409 || out["code"] != "username_cooldown" {
		t.Fatalf("case change must restart cooldown: %d %v", status, out)
	}
	// Rename applies the claim rules: shape, reserved, held by others and taken.
	r.exec(`UPDATE learner_usernames SET changed_at=now()-interval '31 days'`)
	for name, code := range map[string]string{"a": "username_invalid", "bad__name": "username_invalid", "Support": "username_reserved", "supp0rt": "username_reserved"} {
		if status, out = r.rename(aToken, name); status != 422 || out["code"] != code {
			t.Fatalf("rename %q: %d %v", name, status, out)
		}
	}
	if status, _ = r.claim(bToken, "Taken.Name"); status != 200 {
		t.Fatal(status)
	}
	if status, out = r.rename(aToken, "taken.NAME"); status != 409 || out["code"] != "username_taken" {
		t.Fatalf("rename onto taken: %d %v", status, out)
	}
	// Once the hold lapses the released name is free for everyone.
	r.exec(`UPDATE username_holds SET held_until=now()-interval '1 second'`)
	if status, out = r.rename(bToken, "Rena.New"); status != 200 {
		t.Fatalf("lapsed hold: %d %v", status, out)
	}
	if status, out, _ = r.call("PATCH", "/v1/social/username", `{"username":"x","extra":1}`, aToken); status != 422 || out["code"] != "username_invalid" {
		t.Fatalf("strict body: %d %v", status, out)
	}
}

func TestRenameAndClaimRacesForReleasedNames(t *testing.T) {
	r := newRig(t)
	aToken, _ := r.login("a@example.test")
	bToken, _ := r.login("b@example.test")
	cToken, _ := r.login("c@example.test")
	dToken, _ := r.login("d@example.test")
	r.claim(aToken, "Held.Name")
	if status, _ := r.rename(aToken, "Away.Now"); status != 200 {
		t.Fatal(status)
	}
	// The previous owner (reclaim) and a stranger (claim) race for the held name.
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(map[string]int)
	var mu sync.Mutex
	for who, fn := range map[string]func() (int, map[string]any){
		"owner":    func() (int, map[string]any) { return r.rename(aToken, "Held.Name") },
		"stranger": func() (int, map[string]any) { return r.claim(bToken, "held.name") },
		"renamer":  func() (int, map[string]any) { return r.rename(bToken, "held.name") },
	} {
		wg.Go(func() {
			<-start
			s, _ := fn()
			mu.Lock()
			results[who] = s
			mu.Unlock()
		})
	}
	close(start)
	wg.Wait()
	if results["owner"] != 200 || results["stranger"] == 200 || results["renamer"] == 200 {
		t.Fatalf("only the previous owner may win a held name: %v", results)
	}
	// Two learners renaming onto the same free name: exactly one winner.
	r.claim(cToken, "Cee.One")
	r.claim(dToken, "Dee.One")
	start = make(chan struct{})
	statuses := make(chan int, 2)
	for token := range map[string]bool{cToken: true, dToken: true} {
		wg.Go(func() {
			<-start
			s, _ := r.rename(token, "Free.Target")
			statuses <- s
		})
	}
	close(start)
	wg.Wait()
	close(statuses)
	var ok, conflict int
	for s := range statuses {
		switch s {
		case 200:
			ok++
		case 409:
			conflict++
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("rename race: %d winners, %d conflicts", ok, conflict)
	}
	var owners int
	if err := r.db.Pool.QueryRow(r.ctx, `SELECT count(*) FROM learner_usernames WHERE canonical='free.target'`).Scan(&owners); err != nil || owners != 1 {
		t.Fatalf("owners: %d %v", owners, err)
	}
}

func (r *rig) seed(name string, discoverable bool, policy string) string {
	r.t.Helper()
	var id string
	if err := r.db.Pool.QueryRow(r.ctx, `INSERT INTO learners(display_name) VALUES($1) RETURNING id::text`, "Display "+name).Scan(&id); err != nil {
		r.t.Fatal(err)
	}
	r.exec(`INSERT INTO learner_usernames(learner_id,username) VALUES($1::uuid,$2)`, id, name)
	r.exec(`INSERT INTO learner_social_settings(learner_id,discoverable,request_policy) VALUES($1::uuid,$2,$3)`, id, discoverable, policy)
	return id
}

func names(list []map[string]any) []string {
	var out []string
	for _, item := range list {
		out = append(out, item["username"].(string))
	}
	return out
}

func TestSearchBoundsPrivacyAndProjection(t *testing.T) {
	r := newRig(t)
	token, myID := r.login("me@example.test")
	r.claim(token, "Maya.Self")
	r.seed("Maya", true, "everyone")
	r.seed("Mayan_One", true, "nobody")
	r.seed("Mayaxone", true, "everyone")
	r.seed("Quiet.Maya", false, "everyone")
	r.seed("May.Hidden", false, "everyone")
	r.seed("May.Ghost", false, "nobody")
	r.seed("May.Friends", false, "friends_of_friends")
	for i := range 25 {
		r.seed(fmt.Sprintf("Bulk%02d", i), true, "everyone")
	}

	status, list, _ := r.search(token, "may")
	if status != 200 {
		t.Fatal(status)
	}
	// Exact match first, then alphabetical; self, nondiscoverable names excluded.
	if got := strings.Join(names(list), ","); got != "Maya,Mayan_One,Mayaxone" {
		t.Fatalf("prefix results: %s", got)
	}
	for _, item := range list {
		if len(item) != 3 || item["display_name"] == nil || item["avatar"] == nil {
			t.Fatalf("only username, display_name and avatar may be shown: %v", item)
		}
	}
	// "_" is a literal in a prefix, never a wildcard.
	if _, list, _ = r.search(token, "mayan_"); strings.Join(names(list), ",") != "Mayan_One" {
		t.Fatalf("underscore wildcard: %v", names(list))
	}
	if _, list, _ = r.search(token, "@MAYA"); len(list) == 0 || list[0]["username"] != "Maya" {
		t.Fatalf("normalization failed: %v", names(list))
	}
	// Exact lookup: nondiscoverable only when requests are open to everyone.
	for q, want := range map[string]int{"may.hidden": 1, "MAY.HIDDEN": 1, "may.ghost": 0, "may.friends": 0, "nobody.here": 0, "quiet.maya": 1, "quiet": 0, "maya.self": 0} {
		if _, list, _ = r.search(token, q); len(list) != want {
			t.Fatalf("search %q: %v", q, names(list))
		}
	}
	// Short, malformed and wildcard queries.
	if status, _, _ = r.search(token, "ma"); status != 422 {
		t.Fatalf("short: %d", status)
	}
	if status, _, _ = r.search(token, " m "); status != 422 {
		t.Fatalf("trimmed short: %d", status)
	}
	if status, _, _ = r.search(token, strings.Repeat("x", 80)); status != 200 {
		t.Fatalf("long: %d", status)
	}
	for _, q := range []string{"%%%", "ma%", "may'--", "ma y", "may\x00"} {
		if status, list, _ = r.search(token, q); status != 200 || len(list) != 0 {
			t.Fatalf("malformed %q: %d %v", q, status, names(list))
		}
	}
	// Capped at 20.
	if _, list, _ = r.search(token, "bulk"); len(list) != 20 {
		t.Fatalf("cap: %d", len(list))
	}
	if status, _, _ = r.call("GET", "/v1/social/search?q=may", "", ""); status != 401 {
		t.Fatalf("anonymous search: %d", status)
	}
	// Profile uses the exact-match rule.
	if status, _, _ := r.call("GET", "/v1/social/profiles/may.ghost", "", token); status != 404 {
		t.Fatalf("profile of restricted learner: %d", status)
	}
	if status, _, _ := r.call("GET", "/v1/social/profiles/may.hidden", "", token); status != 200 {
		t.Fatalf("profile of exact-findable learner: %d", status)
	}
	var idx string
	if err := r.db.Pool.QueryRow(r.ctx, `SELECT indexdef FROM pg_indexes WHERE tablename='learner_usernames' AND indexdef ILIKE '%text_pattern_ops%'`).Scan(&idx); err != nil || !strings.Contains(idx, "canonical") {
		t.Fatalf("prefix index missing: %q %v", idx, err)
	}

	// Analytics: lengths and counts only, never the query or any name; opt-out silences.
	before := len(r.sink.named("username_searched"))
	r.search(token, "mayan_")
	events := r.sink.named("username_searched")
	if len(events) != before+1 {
		t.Fatalf("events: %d", len(events))
	}
	last := events[len(events)-1]
	props := last["props"].(map[string]any)
	if last["id"] != myID || props["query_length"] != 6 || props["result_count"] != 1 {
		t.Fatalf("event: %v", last)
	}
	if raw, _ := json.Marshal(props); strings.Contains(strings.ToLower(string(raw)), "mayan") {
		t.Fatalf("event leaked the query: %s", raw)
	}
	r.exec(`UPDATE learner_profiles SET analytics_opt_out=true WHERE learner_id=$1`, myID)
	r.search(token, "mayan_")
	if len(r.sink.named("username_searched")) != before+1 {
		t.Fatal("opted-out learner produced a search event")
	}
}

func TestSearchPrivacyFlipsWithSettings(t *testing.T) {
	r := newRig(t)
	aToken, _ := r.login("a@example.test")
	bToken, _ := r.login("b@example.test")
	r.claim(aToken, "Flip.Ann")
	r.claim(bToken, "Flip.Bob")
	if _, list, _ := r.search(aToken, "flip.b"); len(list) != 1 {
		t.Fatalf("discoverable default: %v", names(list))
	}
	if status, out, _ := r.call("PATCH", "/v1/social/settings", `{"discoverable":false}`, bToken); status != 200 || out["discoverable"] != false {
		t.Fatalf("settings: %d %v", status, out)
	}
	if _, list, _ := r.search(aToken, "flip.b"); len(list) != 0 {
		t.Fatalf("hidden from prefix: %v", names(list))
	}
	if _, list, _ := r.search(aToken, "flip.bob"); len(list) != 1 {
		t.Fatalf("exact while requests open: %v", names(list))
	}
	r.call("PATCH", "/v1/social/settings", `{"request_policy":"nobody"}`, bToken)
	if _, list, _ := r.search(aToken, "flip.bob"); len(list) != 0 {
		t.Fatalf("exact while requests closed: %v", names(list))
	}
}

func TestPrivacySettingsAPIAndEvents(t *testing.T) {
	r := newRig(t)
	token, id := r.login("a@example.test")
	// No username needed; defaults are returned without creating state.
	status, out, _ := r.call("GET", "/v1/social/settings", "", token)
	if status != 200 || out["discoverable"] != true || out["request_policy"] != "everyone" || out["show_streak_to_friends"] != true {
		t.Fatalf("defaults: %d %v", status, out)
	}
	if status, _, _ = r.call("GET", "/v1/social/settings", "", ""); status != 401 {
		t.Fatalf("anonymous: %d", status)
	}
	status, out, _ = r.call("PATCH", "/v1/social/settings", `{"discoverable":false,"request_policy":"friends_of_friends","show_streak_to_friends":false}`, token)
	if status != 200 || out["discoverable"] != false || out["request_policy"] != "friends_of_friends" || out["show_streak_to_friends"] != false {
		t.Fatalf("patch: %d %v", status, out)
	}
	events := r.sink.named("privacy_setting_changed")
	got := map[string]any{}
	for _, e := range events {
		p := e["props"].(map[string]any)
		got[p["setting"].(string)] = p["value"]
	}
	if len(events) != 3 || got["discoverable"] != "off" || got["request_policy"] != "friends_of_friends" || got["show_streak_to_friends"] != "off" {
		t.Fatalf("events: %v", got)
	}
	// Unchanged values emit nothing; invalid input changes nothing.
	r.call("PATCH", "/v1/social/settings", `{"discoverable":false}`, token)
	if len(r.sink.named("privacy_setting_changed")) != 3 {
		t.Fatal("unchanged setting emitted an event")
	}
	for _, body := range []string{`{"request_policy":"public"}`, `{"discoverable":"yes"}`, `{"unknown":true}`, `{}`, `[]`, `{"discoverable":true} {}`} {
		if status, out, _ = r.call("PATCH", "/v1/social/settings", body, token); status != 422 {
			t.Fatalf("body %s: %d %v", body, status, out)
		}
	}
	var discoverable bool
	var policy string
	if err := r.db.Pool.QueryRow(r.ctx, `SELECT discoverable,request_policy FROM learner_social_settings WHERE learner_id=$1`, id).Scan(&discoverable, &policy); err != nil || discoverable || policy != "friends_of_friends" {
		t.Fatalf("invalid patches changed state: %v %q %v", discoverable, policy, err)
	}
	// Opted-out learners keep their settings but produce no events.
	r.exec(`UPDATE learner_profiles SET analytics_opt_out=true WHERE learner_id=$1`, id)
	r.call("PATCH", "/v1/social/settings", `{"discoverable":true}`, token)
	if len(r.sink.named("privacy_setting_changed")) != 3 {
		t.Fatal("opted-out learner produced a settings event")
	}
}

func TestSearchAndAttemptRateLimitsAreAtomicAndPerLearner(t *testing.T) {
	r := newRig(t)
	aToken, aID := r.login("a@example.test")
	bToken, _ := r.login("b@example.test")
	r.claim(aToken, "Rate.Ann")
	r.seed("Rate.Target", true, "everyone")
	// 40 concurrent searches: exactly the configured 30 pass.
	var wg sync.WaitGroup
	var mu sync.Mutex
	passed, limited := 0, 0
	var retryAfter string
	for range 40 {
		wg.Go(func() {
			s, _, h := r.search(aToken, "rate.t")
			mu.Lock()
			defer mu.Unlock()
			switch s {
			case 200:
				passed++
			case 429:
				limited++
				retryAfter = h.Get("Retry-After")
			}
		})
	}
	wg.Wait()
	if passed != 30 || limited != 10 || retryAfter == "" {
		t.Fatalf("passed=%d limited=%d retry-after=%q", passed, limited, retryAfter)
	}
	if status, out, _ := r.call("GET", "/v1/social/search?q=rate.t", "", aToken); status != 429 || out["code"] != "rate_limited" {
		t.Fatalf("limited body: %d %v", status, out)
	}
	// Other learners are unaffected; a new window frees the learner.
	if status, _, _ := r.search(bToken, "rate.t"); status != 200 {
		t.Fatalf("other learner limited: %d", status)
	}
	r.exec(`UPDATE social_rate_limits SET window_start=now()-interval '61 seconds'`)
	if status, _, _ := r.search(aToken, "rate.t"); status != 200 {
		t.Fatalf("window did not reset: %d", status)
	}
	// Too-short queries are rejected before they use any budget.
	r.exec(`DELETE FROM social_rate_limits`)
	for range 40 {
		r.search(bToken, "ab")
	}
	if status, _, _ := r.search(bToken, "rate.t"); status != 200 {
		t.Fatalf("short queries consumed the budget: %d", status)
	}

	// Claim/rename probing: the budget comes from configuration and also blocks valid names.
	r.exec(`UPDATE username_config SET name_attempts_per_hour=3`)
	r.exec(`DELETE FROM social_rate_limits`)
	for i := range 3 {
		if status, _ := r.rename(aToken, fmt.Sprintf("Probe.%d", i)); status == 429 {
			t.Fatalf("attempt %d limited early", i)
		}
	}
	status, out, h := r.call("PATCH", "/v1/social/username", `{"username":"Probe.Last"}`, aToken)
	if status != 429 || out["code"] != "rate_limited" || h.Get("Retry-After") == "" {
		t.Fatalf("probing not limited: %d %v", status, out)
	}
	if status, out, _ = r.call("PUT", "/v1/social/username", `{"username":"Probe.Put"}`, bToken); status == 429 {
		t.Fatalf("limit leaked across learners: %v", out)
	}
	for range 3 {
		r.claim(bToken, "Probe.Taken")
	}
	if status, _ = r.claim(bToken, "Fresh.Name"); status != 429 {
		t.Fatalf("claim attempts share the same cap: %d", status)
	}
	var rows int
	if err := r.db.Pool.QueryRow(r.ctx, `SELECT count(*) FROM social_rate_limits WHERE learner_id=$1`, aID).Scan(&rows); err != nil || rows == 0 {
		t.Fatalf("limiter state not in Postgres: %d %v", rows, err)
	}
}

func TestRenameHoldsPrivacyAndLimitsFollowAccountErasure(t *testing.T) {
	r := newRig(t)
	token, id := r.login("a@example.test")
	r.claim(token, "Erase.One")
	r.rename(token, "Erase.Two")
	r.call("PATCH", "/v1/social/settings", `{"discoverable":false}`, token)
	r.search(token, "erase")
	r.exec(`DELETE FROM learners WHERE id=$1`, id)
	for _, table := range []string{"learner_usernames", "learner_social_settings", "social_rate_limits"} {
		var n int
		if err := r.db.Pool.QueryRow(r.ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s survived erasure: %d %v", table, n, err)
		}
	}
	// Both names stay reserved for 30 days with no learner link.
	var n int
	if err := r.db.Pool.QueryRow(r.ctx, `SELECT count(*) FROM username_holds WHERE canonical IN ('erase.one','erase.two') AND held_until>now()+interval '29 days'`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("holds: %d %v", n, err)
	}
	if got, ok := accounts.ClassifiedLearnerTables["social_rate_limits"]; !ok || got == "" {
		t.Fatal("social_rate_limits has no retention classification")
	}
}
