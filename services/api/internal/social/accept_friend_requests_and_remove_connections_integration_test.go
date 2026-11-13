//go:build integration

package social

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
)

// pair is a signed-in learner with a claimed username.
type pair struct {
	token, id, name string
}

func (r *rig) friend(name string) pair {
	r.t.Helper()
	token, id := r.login(strings.ToLower(name) + "@example.test")
	if status, out := r.claim(token, name); status != 200 {
		r.t.Fatal(status, out)
	}
	return pair{token: token, id: id, name: name}
}

func (r *rig) send(from pair, to string) (int, map[string]any) {
	s, o, _ := r.call("POST", "/v1/social/friend-requests", `{"username":"`+to+`"}`, from.token)
	return s, o
}
func (r *rig) sendLink(from pair, token string) (int, map[string]any) {
	s, o, _ := r.call("POST", "/v1/social/friend-requests", `{"token":"`+token+`"}`, from.token)
	return s, o
}
func (r *rig) act(p pair, method, path string) (int, map[string]any) {
	s, o, _ := r.call(method, path, "", p.token)
	return s, o
}
func (r *rig) requests(p pair) (in, out []map[string]any) {
	r.t.Helper()
	s, o, _ := r.call("GET", "/v1/social/friend-requests", "", p.token)
	if s != 200 {
		r.t.Fatal(s, o)
	}
	return rows(o["incoming"]), rows(o["outgoing"])
}
func (r *rig) friends(p pair) []map[string]any {
	r.t.Helper()
	s, o, _ := r.call("GET", "/v1/social/friends", "", p.token)
	if s != 200 {
		r.t.Fatal(s, o)
	}
	return rows(o["friends"])
}
func rows(v any) []map[string]any {
	var out []map[string]any
	list, _ := v.([]any)
	for _, item := range list {
		out = append(out, item.(map[string]any))
	}
	return out
}
func (r *rig) scalar(sql string, args ...any) int {
	r.t.Helper()
	var n int
	if err := r.db.Pool.QueryRow(r.ctx, sql, args...).Scan(&n); err != nil {
		r.t.Fatal(err)
	}
	return n
}

func TestSendAcceptRemoveAreIdempotentAndConsented(t *testing.T) {
	r := newRig(t)
	a, b := r.friend("Alice.One"), r.friend("Bob.Two")

	status, first := r.send(a, "bob.two")
	if status != 200 || first["status"] != "pending" || first["id"] == "" {
		t.Fatalf("send: %d %v", status, first)
	}
	// Re-sending the same pending request is a no-op that returns the same request.
	if status, again := r.send(a, "Bob.Two"); status != 200 || again["id"] != first["id"] {
		t.Fatalf("resend: %d %v", status, again)
	}
	if n := len(r.sink.named("friend_request_sent")); n != 1 {
		t.Fatalf("idempotent resend emitted %d friend_request_sent", n)
	}
	// No friendship without recipient consent.
	if got := r.friends(a); len(got) != 0 {
		t.Fatalf("friends before accept: %v", got)
	}
	in, out := r.requests(b)
	if len(in) != 1 || len(out) != 0 || in[0]["username"] != "Alice.One" {
		t.Fatalf("recipient views: %v %v", in, out)
	}
	if _, sent := r.requests(a); len(sent) != 1 || sent[0]["username"] != "Bob.Two" {
		t.Fatalf("sender sent view: %v", sent)
	}
	id := first["id"].(string)
	// Only the recipient may accept; the sender and strangers get the neutral not-found.
	c := r.friend("Cara.Three")
	for _, p := range []pair{a, c} {
		if status, _ := r.act(p, "POST", "/v1/social/friend-requests/"+id+"/accept"); status != 404 {
			t.Fatalf("non-recipient accept: %d", status)
		}
	}
	for range 2 {
		if status, out := r.act(b, "POST", "/v1/social/friend-requests/"+id+"/accept"); status != 200 || out["status"] != "friends" {
			t.Fatalf("accept: %d %v", status, out)
		}
	}
	if n := r.scalar(`SELECT count(*) FROM friendships`); n != 1 {
		t.Fatalf("friendships=%d", n)
	}
	if n := len(r.sink.named("friend_request_accepted")); n != 1 {
		t.Fatalf("accepted events=%d", n)
	}
	fa, fb := r.friends(a), r.friends(b)
	if len(fa) != 1 || fa[0]["username"] != "Bob.Two" || len(fb) != 1 || fb[0]["username"] != "Alice.One" {
		t.Fatalf("friend lists: %v %v", fa, fb)
	}
	// Sending to an existing friend is idempotent success.
	if status, out := r.send(a, "bob.two"); status != 200 || out["status"] != "friends" {
		t.Fatalf("send to friend: %d %v", status, out)
	}
	removeID := fa[0]["id"].(string)
	// A stranger cannot remove it, and learns nothing.
	if status, _ := r.act(c, "DELETE", "/v1/social/friends/"+removeID); status != 200 {
		t.Fatalf("stranger remove: %d", status)
	}
	if n := r.scalar(`SELECT count(*) FROM friendships`); n != 1 {
		t.Fatal("a stranger removed a friendship")
	}
	for range 2 {
		if status, out := r.act(b, "DELETE", "/v1/social/friends/"+removeID); status != 200 || out["status"] != "removed" {
			t.Fatalf("remove: %d %v", status, out)
		}
	}
	if n := r.scalar(`SELECT count(*) FROM friendships`) + r.scalar(`SELECT count(*) FROM friend_requests`); n != 0 {
		t.Fatalf("rows left after remove: %d", n)
	}
	if n := len(r.sink.named("friend_removed")); n != 1 {
		t.Fatalf("removed events=%d (idempotent repeat must not re-emit)", n)
	}
	// After removal the pair starts over.
	if status, out := r.send(b, "alice.one"); status != 200 || out["status"] != "pending" {
		t.Fatalf("send after remove: %d %v", status, out)
	}
	// Analytics: UUID distinct IDs, enums only, no learner text.
	sent := r.sink.named("friend_request_sent")
	if sent[0]["id"] != a.id || sent[0]["props"].(map[string]any)["method"] != "username" {
		t.Fatalf("sent event: %v", sent[0])
	}
	for _, name := range []string{"friend_request_sent", "friend_request_accepted", "friend_removed"} {
		for _, e := range r.sink.named(name) {
			for k, v := range e["props"].(map[string]any) {
				if s, ok := v.(string); ok && (strings.Contains(strings.ToLower(s), "alice") || strings.Contains(strings.ToLower(s), "bob") || strings.Contains(s, id)) {
					t.Fatalf("%s leaks %s=%q", name, k, s)
				}
			}
		}
	}
}

func TestDeclineIsIdempotentSilentAndCoolsDown(t *testing.T) {
	r := newRig(t)
	a, b := r.friend("Dana.One"), r.friend("Eli.Two")
	_, sent := r.send(a, "eli.two")
	id := sent["id"].(string)
	for range 2 {
		if status, out := r.act(b, "POST", "/v1/social/friend-requests/"+id+"/decline"); status != 200 || out["status"] != "declined" {
			t.Fatalf("decline: %d %v", status, out)
		}
	}
	if n := len(r.sink.named("friend_request_declined")); n != 1 {
		t.Fatalf("declined events=%d", n)
	}
	if status, _ := r.act(b, "POST", "/v1/social/friend-requests/"+id+"/accept"); status != 409 {
		t.Fatalf("accept after decline: %d", status)
	}
	// The sender is not told; the declined request just stops being listed as pending.
	if in, out := r.requests(a); len(in)+len(out) != 0 {
		t.Fatalf("sender sees declined request: %v %v", in, out)
	}
	if in, _ := r.requests(b); len(in) != 0 {
		t.Fatal("declined request still incoming")
	}
	// Cooldown: same sender to same recipient gets the neutral error and nothing is created.
	status, refused := r.send(a, "eli.two")
	_, missing := r.send(a, "nobody.here")
	if status != 404 || refused["code"] != "request_unavailable" || !reflect.DeepEqual(refused, missing) {
		t.Fatalf("cooldown not neutral: %d %v vs %v", status, refused, missing)
	}
	if n := r.scalar(`SELECT count(*) FROM friend_requests WHERE status='pending'`); n != 0 {
		t.Fatal("request created during cooldown")
	}
	// The other direction is unaffected.
	if status, out := r.send(b, "dana.one"); status != 200 || out["status"] != "pending" {
		t.Fatalf("reverse send: %d %v", status, out)
	}
	r.exec(`DELETE FROM friend_requests WHERE sender_id=$1`, b.id)
	// After the configured days the sender may try again.
	r.exec(`UPDATE friend_requests SET decided_at=now()-interval '7 days 1 minute' WHERE sender_id=$1`, a.id)
	if status, out := r.send(a, "eli.two"); status != 200 || out["status"] != "pending" {
		t.Fatalf("send after cooldown: %d %v", status, out)
	}
	if n := r.scalar(`SELECT count(*) FROM friend_requests WHERE sender_id=$1`, a.id); n != 1 {
		t.Fatalf("re-send must reuse the pair row, rows=%d", n)
	}
}

func TestRequestPolicyIsEnforcedWithNeutralErrors(t *testing.T) {
	r := newRig(t)
	a, closed, fof, open := r.friend("Fay.One"), r.friend("Gil.Closed"), r.friend("Hal.Fof"), r.friend("Ivy.Open")
	patch := func(p pair, policy string) {
		if s, o, _ := r.call("PATCH", "/v1/social/settings", `{"request_policy":"`+policy+`"}`, p.token); s != 200 {
			t.Fatal(s, o)
		}
	}
	patch(closed, "nobody")
	patch(fof, "friends_of_friends")
	status, base := r.send(a, "no.such.person")
	if status != 404 || base["code"] != "request_unavailable" {
		t.Fatalf("unknown: %d %v", status, base)
	}
	for name, to := range map[string]string{"nobody": "gil.closed", "fof-without-common": "hal.fof", "self": "fay.one"} {
		if s, o := r.send(a, to); s != status || !reflect.DeepEqual(o, base) {
			t.Fatalf("%s differs from unknown: %d %v", name, s, o)
		}
	}
	// friends_of_friends: allowed once sender and recipient share a friend.
	r.exec(`INSERT INTO friendships(learner_low,learner_high) VALUES (LEAST($1::uuid,$2::uuid),GREATEST($1::uuid,$2::uuid)),(LEAST($2::uuid,$3::uuid),GREATEST($2::uuid,$3::uuid))`, a.id, open.id, fof.id)
	if s, o := r.send(a, "hal.fof"); s != 200 || o["status"] != "pending" {
		t.Fatalf("fof with common friend: %d %v", s, o)
	}
	if n := r.scalar(`SELECT count(*) FROM friend_requests WHERE recipient_id=$1`, closed.id); n != 0 {
		t.Fatal("request reached a nobody-policy learner")
	}
	// The policy applies on the next request.
	patch(closed, "everyone")
	if s, o := r.send(a, "gil.closed"); s != 200 || o["status"] != "pending" {
		t.Fatalf("after opening: %d %v", s, o)
	}
}

func TestConfiguredLimitsPendingDailyAndFriends(t *testing.T) {
	r := newRig(t)
	r.exec(`UPDATE username_config SET friend_pending_max=2, friend_sends_per_day=3, friend_max=1`)
	a := r.friend("Jo.Sender")
	people := []pair{r.friend("Kim.One"), r.friend("Lee.Two"), r.friend("Max.Three"), r.friend("Ned.Four")}
	for _, p := range people[:2] {
		if s, o := r.send(a, p.name); s != 200 {
			t.Fatal(s, o)
		}
	}
	s, o := r.send(a, people[2].name)
	if s != 409 || o["code"] != "pending_limit" {
		t.Fatalf("pending cap: %d %v", s, o)
	}
	// A refused send consumes nothing; an idempotent re-send is still fine at the cap.
	if s, _ := r.send(a, people[0].name); s != 200 {
		t.Fatalf("idempotent resend at cap: %d", s)
	}
	// Cancel frees a slot and is idempotent.
	_, out := r.requests(a)
	cancel := "/v1/social/friend-requests/" + out[0]["id"].(string)
	for range 2 {
		if s, _ := r.act(a, "DELETE", cancel); s != 200 {
			t.Fatalf("cancel: %d", s)
		}
	}
	if s, o := r.send(a, people[2].name); s != 200 {
		t.Fatalf("after cancel: %d %v", s, o)
	}
	// Daily cap: three real sends so far (kim, lee, max); the next real send is the fourth.
	r.exec(`DELETE FROM friend_requests WHERE sender_id=$1`, a.id)
	s, o = r.send(a, people[3].name)
	if s != 429 || o["code"] != "rate_limited" {
		t.Fatalf("daily cap: %d %v", s, o)
	}
	// Friend cap, checked on accept for both sides.
	b := people[0]
	r.exec(`DELETE FROM social_rate_limits`)
	_, sent := r.send(a, b.name)
	r.exec(`INSERT INTO friendships(learner_low,learner_high) VALUES (LEAST($1::uuid,$2::uuid),GREATEST($1::uuid,$2::uuid))`, b.id, people[1].id)
	if s, o := r.act(b, "POST", "/v1/social/friend-requests/"+sent["id"].(string)+"/accept"); s != 409 || o["code"] != "friend_limit" {
		t.Fatalf("friend cap: %d %v", s, o)
	}
	if n := r.scalar(`SELECT count(*) FROM friendships`); n != 1 {
		t.Fatal("friendship created over the cap")
	}
}

func TestConcurrentMutualRequestsResolveToOneFriendship(t *testing.T) {
	r := newRig(t)
	for i := range 6 {
		a, b := r.friend(fmt.Sprintf("Mut.A%d", i)), r.friend(fmt.Sprintf("Mut.B%d", i))
		var wg sync.WaitGroup
		codes := make([]int, 2)
		for n, args := range [][2]pair{{a, b}, {b, a}} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				codes[n], _ = r.send(args[0], args[1].name)
			}()
		}
		wg.Wait()
		if codes[0] != 200 || codes[1] != 200 {
			t.Fatalf("round %d: %v", i, codes)
		}
		if n := r.scalar(`SELECT count(*) FROM friendships WHERE learner_low=LEAST($1::uuid,$2::uuid) AND learner_high=GREATEST($1::uuid,$2::uuid)`, a.id, b.id); n != 1 {
			t.Fatalf("round %d friendships=%d", i, n)
		}
		if n := r.scalar(`SELECT count(*) FROM friend_requests WHERE status='pending' AND sender_id IN ($1,$2)`, a.id, b.id); n != 0 {
			t.Fatalf("round %d pending=%d", i, n)
		}
	}
}

func TestPostgresPairUniquenessIsStructural(t *testing.T) {
	r := newRig(t)
	a, b := r.friend("Pair.One"), r.friend("Pair.Two")
	lo, hi := a.id, b.id
	if lo > hi {
		lo, hi = hi, lo
	}
	r.exec(`INSERT INTO friendships(learner_low,learner_high) VALUES ($1,$2)`, lo, hi)
	for name, args := range map[string][]any{"duplicate": {lo, hi}, "reversed": {hi, lo}, "self": {lo, lo}} {
		if _, err := r.db.Pool.Exec(r.ctx, `INSERT INTO friendships(learner_low,learner_high) VALUES ($1,$2)`, args...); err == nil {
			t.Fatalf("%s friendship accepted", name)
		}
	}
	r.exec(`INSERT INTO friend_requests(sender_id,recipient_id,method) VALUES ($1,$2,'username')`, a.id, b.id)
	if _, err := r.db.Pool.Exec(r.ctx, `INSERT INTO friend_requests(sender_id,recipient_id,method) VALUES ($1,$2,'username')`, b.id, a.id); err == nil {
		t.Fatal("two pending requests for one canonical pair")
	}
	if _, err := r.db.Pool.Exec(r.ctx, `INSERT INTO friend_requests(sender_id,recipient_id,method) VALUES ($1,$1,'username')`, a.id); err == nil {
		t.Fatal("self request accepted")
	}
}

func TestFriendLinkValidationRotationRevocationAndPolicy(t *testing.T) {
	r := newRig(t)
	owner, guest := r.friend("Link.Owner"), r.friend("Link.Guest")
	if s, o := r.act(owner, "GET", "/v1/social/friend-link"); s != 200 || o["active"] != false {
		t.Fatalf("no link yet: %d %v", s, o)
	}
	s, made := r.act(owner, "POST", "/v1/social/friend-link")
	token, _ := made["token"].(string)
	if s != 200 || len(token) < 40 || made["expires_at"] == nil {
		t.Fatalf("create: %d %v", s, made)
	}
	// Only the hash is stored.
	if n := r.scalar(`SELECT count(*) FROM friend_links WHERE token_hash=encode(sha256(convert_to($1,'UTF8')),'hex') AND revoked_at IS NULL`, token); n != 1 {
		t.Fatalf("hash row=%d", n)
	}
	var stored string
	if err := r.db.Pool.QueryRow(r.ctx, `SELECT row_to_json(f)::text FROM friend_links f`).Scan(&stored); err != nil || strings.Contains(stored, token) {
		t.Fatalf("token stored in clear: %v", err)
	}
	if s, o := r.act(owner, "GET", "/v1/social/friend-link"); s != 200 || o["active"] != true || o["token"] != nil {
		t.Fatalf("status must not return the token: %d %v", s, o)
	}
	// Resolve previews only the public identity, creates nothing.
	s, who, _ := r.call("POST", "/v1/social/friend-link/resolve", `{"token":"`+token+`"}`, guest.token)
	if s != 200 || who["username"] != "Link.Owner" || len(who) != 3 {
		t.Fatalf("resolve: %d %v", s, who)
	}
	if n := r.scalar(`SELECT count(*) FROM friend_requests`); n != 0 {
		t.Fatal("resolve created a request")
	}
	// Signed-out callers cannot resolve or send.
	if s, _, _ := r.call("POST", "/v1/social/friend-link/resolve", `{"token":"`+token+`"}`, ""); s != 401 {
		t.Fatalf("signed out resolve: %d", s)
	}
	s, sent := r.sendLink(guest, token)
	if s != 200 || sent["status"] != "pending" {
		t.Fatalf("send by link: %d %v", s, sent)
	}
	if e := r.sink.named("friend_request_sent"); len(e) != 1 || e[0]["props"].(map[string]any)["method"] != "invite_link" {
		t.Fatalf("method property: %v", e)
	}
	// The owner opening their own link, a bogus token and a malformed token all look alike.
	_, unknown, _ := r.call("POST", "/v1/social/friend-link/resolve", `{"token":"nope"}`, guest.token)
	if s, o := r.sendLink(owner, token); s != 404 || o["code"] != "invite_unavailable" {
		t.Fatalf("own link: %d %v", s, o)
	}
	if unknown["code"] != "invite_unavailable" {
		t.Fatalf("unknown token: %v", unknown)
	}
	// Rotation invalidates the previous token.
	_, rotated := r.act(owner, "POST", "/v1/social/friend-link")
	if rotated["token"] == token {
		t.Fatal("rotation reused the token")
	}
	if s, o, _ := r.call("POST", "/v1/social/friend-link/resolve", `{"token":"`+token+`"}`, guest.token); s != 404 || o["code"] != "invite_unavailable" {
		t.Fatalf("old token: %d %v", s, o)
	}
	// Expiry and revocation.
	token = rotated["token"].(string)
	r.exec(`UPDATE friend_links SET expires_at=now()-interval '1 second' WHERE revoked_at IS NULL`)
	if s, o, _ := r.call("POST", "/v1/social/friend-link/resolve", `{"token":"`+token+`"}`, guest.token); s != 404 || o["code"] != "invite_unavailable" {
		t.Fatalf("expired: %d %v", s, o)
	}
	_, fresh := r.act(owner, "POST", "/v1/social/friend-link")
	token = fresh["token"].(string)
	if s, _ := r.act(owner, "DELETE", "/v1/social/friend-link"); s != 200 {
		t.Fatalf("revoke: %d", s)
	}
	if s, _ := r.act(owner, "DELETE", "/v1/social/friend-link"); s != 200 {
		t.Fatalf("revoke twice: %d", s)
	}
	if s, o := r.sendLink(guest, token); s != 404 || o["code"] != "invite_unavailable" {
		t.Fatalf("revoked: %d %v", s, o)
	}
	// nobody wins over a link; friends_of_friends does not block it.
	_, again := r.act(owner, "POST", "/v1/social/friend-link")
	token = again["token"].(string)
	r.exec(`UPDATE learner_social_settings SET request_policy='nobody' WHERE learner_id=$1`, owner.id)
	r.exec(`DELETE FROM friend_requests`)
	if s, o := r.sendLink(guest, token); s != 404 || o["code"] != "invite_unavailable" {
		t.Fatalf("nobody must refuse links: %d %v", s, o)
	}
	r.exec(`UPDATE learner_social_settings SET request_policy='friends_of_friends' WHERE learner_id=$1`, owner.id)
	if s, o := r.sendLink(guest, token); s != 200 || o["status"] != "pending" {
		t.Fatalf("fof link: %d %v", s, o)
	}
	// The owner already asked the guest: using the link accepts instead of duplicating.
	r.exec(`DELETE FROM friend_requests`)
	r.send(owner, guest.name)
	if s, o := r.sendLink(guest, token); s != 200 || o["status"] != "friends" {
		t.Fatalf("mutual by link: %d %v", s, o)
	}
}

func TestViewsExposeOnlyPublicFieldsAndErasureRemovesRows(t *testing.T) {
	r := newRig(t)
	a, b := r.friend("View.One"), r.friend("View.Two")
	_, sent := r.send(a, b.name)
	in, _ := r.requests(b)
	allowed := []string{"avatar", "created_at", "display_name", "id", "username"}
	keys := func(m map[string]any) []string {
		var k []string
		for key := range m {
			k = append(k, key)
		}
		sort.Strings(k)
		return k
	}
	if !reflect.DeepEqual(keys(in[0]), allowed) {
		t.Fatalf("request view keys: %v", keys(in[0]))
	}
	r.act(b, "POST", "/v1/social/friend-requests/"+sent["id"].(string)+"/accept")
	f := r.friends(a)[0]
	for key := range f {
		switch key {
		case "id", "username", "display_name", "avatar", "since", "streak", "friend_streak", "nudge":
		default:
			t.Fatalf("friend view leaks %q", key)
		}
	}
	// E6-07 adds the sender's own nudge state, and only that: whether today's nudge is spent.
	for key := range f["nudge"].(map[string]any) {
		if key != "sent" && key != "next_at" {
			t.Fatalf("nudge state leaks %q", key)
		}
	}
	// E6-06 adds the privacy-permitted shared streak object, and only that: counts, state and
	// each friend's own local day. No personal streak is invented without a source.
	if f["streak"] != nil {
		t.Fatalf("unexpected streak: %v", f["streak"])
	}
	shared, ok := f["friend_streak"].(map[string]any)
	if !ok {
		t.Fatalf("friend_streak missing: %v", f)
	}
	for key := range shared {
		switch key {
		case "days", "longest", "state", "you", "friend", "checking_until":
		default:
			t.Fatalf("friend_streak leaks %q", key)
		}
	}
	for _, side := range []string{"you", "friend"} {
		for key := range shared[side].(map[string]any) {
			switch key {
			case "local_date", "time_zone", "done", "protected":
			default:
				t.Fatalf("friend_streak.%s leaks %q", side, key)
			}
		}
	}
	// Hidden entirely when the friend turns show_streak_to_friends off.
	if s, o, _ := r.call("PATCH", "/v1/social/settings", `{"show_streak_to_friends":false}`, b.token); s != 200 {
		t.Fatal(s, o)
	}
	if hidden := r.friends(a)[0]; hidden["friend_streak"] != nil || hidden["streak"] != nil {
		t.Fatalf("streak exposed with the setting off: %v", hidden)
	}
	_, link := r.act(a, "POST", "/v1/social/friend-link")
	_ = link
	r.exec(`DELETE FROM learners WHERE id=$1`, a.id)
	for _, table := range []string{"friendships", "friend_requests", "friend_links"} {
		if n := r.scalar(`SELECT count(*) FROM ` + table); n != 0 {
			t.Fatalf("%s rows after erasure: %d", table, n)
		}
	}
	if got := r.friends(b); len(got) != 0 {
		t.Fatalf("former friend still listed: %v", got)
	}
}

func TestRemovalAndCancelRunInsideACallerTransaction(t *testing.T) {
	r := newRig(t)
	a, b := r.friend("Tx.One"), r.friend("Tx.Two")
	_, sent := r.send(a, b.name)
	r.act(b, "POST", "/v1/social/friend-requests/"+sent["id"].(string)+"/accept")
	svc := New(r.db.Pool, nil)
	tx, err := r.db.Pool.Begin(r.ctx)
	if err != nil {
		t.Fatal(err)
	}
	removed, err := svc.removeFriendshipTx(r.ctx, tx, a.id, b.id)
	if err != nil || !removed {
		t.Fatalf("removeFriendshipTx: %v %v", removed, err)
	}
	if err = tx.Rollback(r.ctx); err != nil {
		t.Fatal(err)
	}
	if n := r.scalar(`SELECT count(*) FROM friendships`); n != 1 {
		t.Fatal("rollback must keep the friendship (single transactional unit)")
	}
	tx, _ = r.db.Pool.Begin(r.ctx)
	if removed, err = svc.removeFriendshipTx(r.ctx, tx, b.id, a.id); err != nil || !removed {
		t.Fatalf("remove reversed: %v %v", removed, err)
	}
	if removed, err = svc.removeFriendshipTx(r.ctx, tx, a.id, b.id); err != nil || removed {
		t.Fatalf("second remove must report nothing removed: %v %v", removed, err)
	}
	if err = tx.Commit(r.ctx); err != nil {
		t.Fatal(err)
	}
	if n := r.scalar(`SELECT count(*) FROM friendships`); n != 0 {
		t.Fatal("commit did not remove")
	}
}
