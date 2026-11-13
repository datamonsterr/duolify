//go:build integration

package social

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/jackc/pgx/v5"
)

func (r *rig) block(p pair, name string) (int, map[string]any) {
	s, o, _ := r.call("POST", "/v1/social/blocks", `{"username":"`+name+`"}`, p.token)
	return s, o
}
func (r *rig) unblock(p pair, name string) (int, map[string]any) {
	s, o, _ := r.call("DELETE", "/v1/social/blocks/"+name, "", p.token)
	return s, o
}
func (r *rig) blocks(p pair) []map[string]any {
	r.t.Helper()
	s, o, _ := r.call("GET", "/v1/social/blocks", "", p.token)
	if s != 200 {
		r.t.Fatal(s, o)
	}
	return rows(o["blocks"])
}
func (r *rig) befriend(a, b pair) {
	r.t.Helper()
	s, o := r.send(a, b.name)
	if s != 200 {
		r.t.Fatal(s, o)
	}
	if s, o = r.act(b, "POST", "/v1/social/friend-requests/"+o["id"].(string)+"/accept"); s != 200 {
		r.t.Fatal(s, o)
	}
}
func (r *rig) eventsFor(id string) int {
	n := 0
	r.sink.mu.Lock()
	defer r.sink.mu.Unlock()
	for _, e := range r.sink.events {
		if e["id"] == id {
			n++
		}
	}
	return n
}

func TestBlockEndsFriendshipAndRequestsBothDirectionsAndStaysSilent(t *testing.T) {
	r := newRig(t)
	a, b := r.friend("Block.Alpha"), r.friend("Block.Bravo")
	c, d := r.friend("Block.Charlie"), r.friend("Block.Delta")
	e, f := r.friend("Block.Echo"), r.friend("Block.Foxtrot")
	g, h := r.friend("Block.Golf"), r.friend("Block.Hotel")
	r.befriend(a, b)
	if s, o := r.send(c, d.name); s != 200 { // pending c->d, d blocks c
		t.Fatal(s, o)
	}
	if s, o := r.send(e, f.name); s != 200 { // pending e->f, e blocks f
		t.Fatal(s, o)
	}
	_, declined := r.send(g, h.name) // declined g->h, h blocks g
	r.act(h, "POST", "/v1/social/friend-requests/"+declined["id"].(string)+"/decline")

	before := r.eventsFor(b.id)
	s, out := r.block(a, "block.bravo") // any case
	if s != 200 || out["status"] != "blocked" {
		t.Fatalf("block: %d %v", s, out)
	}
	if s, o := r.block(d, c.name); s != 200 {
		t.Fatal(s, o)
	}
	if s, o := r.block(e, f.name); s != 200 {
		t.Fatal(s, o)
	}
	if s, o := r.block(h, g.name); s != 200 {
		t.Fatal(s, o)
	}
	if n := r.scalar(`SELECT count(*) FROM friendships`); n != 0 {
		t.Fatalf("friendships=%d", n)
	}
	if n := r.scalar(`SELECT count(*) FROM friend_requests`); n != 0 {
		t.Fatalf("requests=%d", n)
	}
	if n := r.scalar(`SELECT count(*) FROM learner_blocks`); n != 4 {
		t.Fatalf("blocks=%d", n)
	}
	for _, p := range []pair{a, b} {
		if got := r.friends(p); len(got) != 0 {
			t.Fatalf("%s still has friends %v", p.name, got)
		}
	}
	for _, p := range []pair{c, d, e, f} {
		if in, out := r.requests(p); len(in)+len(out) != 0 {
			t.Fatalf("%s still has requests %v %v", p.name, in, out)
		}
	}
	// The blocker's list shows only the public identity; the blocked learner's list is empty.
	list := r.blocks(a)
	if len(list) != 1 || list[0]["username"] != "Block.Bravo" || list[0]["display_name"] == nil || list[0]["avatar"] == nil || list[0]["blocked_at"] == nil || len(list[0]) != 4 {
		t.Fatalf("blocked list: %v", list)
	}
	if got := r.blocks(b); len(got) != 0 {
		t.Fatalf("blocked learner learned of the block: %v", got)
	}
	// Idempotent: a repeat changes nothing and emits nothing more.
	if s, o := r.block(a, b.name); s != 200 || o["status"] != "blocked" {
		t.Fatalf("repeat: %d %v", s, o)
	}
	if n := r.scalar(`SELECT count(*) FROM learner_blocks WHERE blocker_id=$1::uuid`, a.id); n != 1 {
		t.Fatalf("repeat created %d rows", n)
	}
	events := r.sink.named("learner_blocked")
	if len(events) != 4 {
		t.Fatalf("learner_blocked events=%d", len(events))
	}
	for _, ev := range events {
		props := ev["props"].(map[string]any)
		for k, v := range props {
			if s, ok := v.(string); ok && strings.Contains(strings.ToLower(s), "block.") {
				t.Fatalf("event leaks a username in %s=%s", k, s)
			}
		}
		if _, ok := props["was_friend"].(bool); !ok {
			t.Fatalf("was_friend missing: %v", props)
		}
	}
	if ev := r.sink.named("learner_blocked")[0]; ev["id"] != a.id || ev["props"].(map[string]any)["was_friend"] != true {
		t.Fatalf("first event: %v", ev)
	}
	// Nothing at all is sent for or to the blocked learner.
	if after := r.eventsFor(b.id); after != before {
		t.Fatalf("blocked learner got %d new events", after-before)
	}
}

func TestBlockedSendAndLinkFailExactlyLikeAnUnknownUsername(t *testing.T) {
	r := newRig(t)
	blocker, target := r.friend("Quiet.Blocker"), r.friend("Quiet.Target")
	r.befriend(blocker, target)
	_, link := r.act(blocker, "POST", "/v1/social/friend-link")
	token := link["token"].(string)
	if s, o := r.block(blocker, target.name); s != 200 {
		t.Fatal(s, o)
	}
	_, theirs := r.act(target, "POST", "/v1/social/friend-link")
	theirToken := theirs["token"].(string)

	s0, unknown := r.send(target, "Nobody.Here")
	if s0 != 404 || unknown["code"] != "request_unavailable" {
		t.Fatalf("unknown baseline: %d %v", s0, unknown)
	}
	if s, o := r.send(target, blocker.name); s != s0 || !reflect.DeepEqual(o, unknown) {
		t.Fatalf("blocked send differs from unknown: %d %v", s, o)
	}
	if s, o := r.send(blocker, target.name); s != s0 || !reflect.DeepEqual(o, unknown) {
		t.Fatalf("send to the blocked differs from unknown: %d %v", s, o)
	}
	_, unknownLink, _ := r.call("POST", "/v1/social/friend-requests", `{"token":"`+strings.Repeat("A", 43)+`"}`, target.token)
	if s, o := r.sendLink(target, token); s != 404 || !reflect.DeepEqual(o, unknownLink) || o["code"] != "invite_unavailable" {
		t.Fatalf("blocked link send: %d %v want %v", s, o, unknownLink)
	}
	if s, o := r.sendLink(blocker, theirToken); s != 404 || !reflect.DeepEqual(o, unknownLink) {
		t.Fatalf("link of the blocked: %d %v", s, o)
	}
	for _, c := range []struct {
		viewer pair
		token  string
	}{{target, token}, {blocker, theirToken}} {
		s, o, _ := r.call("POST", "/v1/social/friend-link/resolve", `{"token":"`+c.token+`"}`, c.viewer.token)
		if s != 404 || o["code"] != "invite_unavailable" {
			t.Fatalf("resolve across a block: %d %v", s, o)
		}
	}
	if n := r.scalar(`SELECT count(*) FROM friend_requests`); n != 0 {
		t.Fatalf("requests created across a block: %d", n)
	}
	if got := len(r.sink.named("friend_request_sent")); got != 1 { // only the first befriend
		t.Fatalf("friend_request_sent=%d", got)
	}
	// Unblocking restores the ability to ask, not the old friendship.
	if s, _ := r.unblock(blocker, target.name); s != 200 {
		t.Fatal(s)
	}
	if got := r.friends(blocker); len(got) != 0 {
		t.Fatalf("unblock restored friendship: %v", got)
	}
	if s, o, _ := r.call("POST", "/v1/social/friend-link/resolve", `{"token":"`+theirToken+`"}`, blocker.token); s != 200 || o["username"] != "Quiet.Target" {
		t.Fatalf("resolve after unblock: %d %v", s, o)
	}
	if s, o := r.send(target, blocker.name); s != 200 || o["status"] != "pending" {
		t.Fatalf("send after unblock: %d %v", s, o)
	}
}

func TestBlockRaceAgainstSendAndAcceptEndsWithNoFriendshipOrRequest(t *testing.T) {
	r := newRig(t)
	for i := range 6 {
		a, b := r.friend(fmt.Sprintf("Race.A%d", i)), r.friend(fmt.Sprintf("Race.B%d", i))
		var pending string
		switch i % 3 {
		case 0: // block vs accept
			_, o := r.send(a, b.name)
			pending = o["id"].(string)
		case 2: // mutual request vs block
			r.send(a, b.name)
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		run := func(fn func()) {
			wg.Add(1)
			go func() { defer wg.Done(); <-start; fn() }()
		}
		switch i % 3 {
		case 0:
			run(func() { r.act(b, "POST", "/v1/social/friend-requests/"+pending+"/accept") })
			run(func() { r.block(a, b.name) })
		case 1: // block vs send
			run(func() { r.send(a, b.name) })
			run(func() { r.block(b, a.name) })
		default:
			run(func() { r.send(b, a.name) })
			run(func() { r.block(a, b.name) })
		}
		close(start)
		wg.Wait()
		if n := r.scalar(`SELECT count(*) FROM friendships WHERE learner_low IN ($1::uuid,$2::uuid) OR learner_high IN ($1::uuid,$2::uuid)`, a.id, b.id); n != 0 {
			t.Fatalf("round %d left %d friendships", i, n)
		}
		if n := r.scalar(`SELECT count(*) FROM friend_requests WHERE sender_id IN ($1::uuid,$2::uuid) OR recipient_id IN ($1::uuid,$2::uuid)`, a.id, b.id); n != 0 {
			t.Fatalf("round %d left %d requests", i, n)
		}
		if n := r.scalar(`SELECT count(*) FROM learner_blocks WHERE blocker_id IN ($1::uuid,$2::uuid)`, a.id, b.id); n != 1 {
			t.Fatalf("round %d blocks=%d", i, n)
		}
	}
}

// fakeInvites stands in for the E15 invite table: it proves a callback runs inside the
// block transaction and can cancel references in both directions.
func TestOnBlockCallbacksRunInsideTheBlockTransactionAndRollBackOnFailure(t *testing.T) {
	r := newRig(t)
	r.exec(`CREATE TABLE fake_invites(inviter uuid, invitee uuid)`)
	a, b, c := r.friend("Hook.Alpha"), r.friend("Hook.Bravo"), r.friend("Hook.Charlie")
	r.befriend(a, b)
	r.exec(`INSERT INTO fake_invites VALUES($1::uuid,$2::uuid),($2::uuid,$1::uuid),($1::uuid,$3::uuid)`, a.id, b.id, c.id)
	var calls [][2]string
	var insideBlock, outsideBlock, insideFriends int
	r.svc.OnBlock(func(ctx context.Context, tx pgx.Tx, blocker, blocked string) error {
		calls = append(calls, [2]string{blocker, blocked})
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM learner_blocks WHERE blocker_id=$1::uuid AND blocked_id=$2::uuid`, blocker, blocked).Scan(&insideBlock)
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM friendships`).Scan(&insideFriends)
		_ = r.db.Pool.QueryRow(ctx, `SELECT count(*) FROM learner_blocks`).Scan(&outsideBlock)
		_, err := tx.Exec(ctx, `DELETE FROM fake_invites WHERE (inviter=$1::uuid AND invitee=$2::uuid) OR (inviter=$2::uuid AND invitee=$1::uuid)`, blocker, blocked)
		return err
	})
	if s, o := r.block(a, b.name); s != 200 {
		t.Fatal(s, o)
	}
	if len(calls) != 1 || calls[0] != [2]string{a.id, b.id} {
		t.Fatalf("calls=%v", calls)
	}
	if insideBlock != 1 || outsideBlock != 0 || insideFriends != 0 {
		t.Fatalf("callback not inside the transaction after friendship removal: block=%d outside=%d friends=%d", insideBlock, outsideBlock, insideFriends)
	}
	if n := r.scalar(`SELECT count(*) FROM fake_invites`); n != 1 {
		t.Fatalf("invites left=%d (only the unrelated a->c invite may remain)", n)
	}
	// A repeat block is a no-op and does not call back again.
	r.block(a, b.name)
	if len(calls) != 1 {
		t.Fatalf("repeat block called back: %v", calls)
	}
	// A failing callback rolls back the block, the friendship removal and earlier callbacks.
	r.befriend(a, c)
	r.svc.OnBlock(func(context.Context, pgx.Tx, string, string) error { return fmt.Errorf("invite store down") })
	if s, _ := r.block(a, c.name); s != 503 {
		t.Fatalf("callback failure status=%d", s)
	}
	if n := r.scalar(`SELECT count(*) FROM learner_blocks WHERE blocked_id=$1::uuid`, c.id); n != 0 {
		t.Fatalf("block survived a failed callback: %d", n)
	}
	if got := r.friends(a); len(got) != 1 || got[0]["username"] != "Hook.Charlie" {
		t.Fatalf("friendship lost with the block: %v", got)
	}
	if n := r.scalar(`SELECT count(*) FROM fake_invites`); n != 1 {
		t.Fatalf("invites=%d after rollback", n)
	}
	if len(r.sink.named("learner_blocked")) != 1 {
		t.Fatal("learner_blocked emitted for a rolled-back block")
	}
}

func TestSharedAuthorizationContractForInvites(t *testing.T) {
	r := newRig(t)
	a, b, c := r.friend("Auth.Alpha"), r.friend("Auth.Bravo"), r.friend("Auth.Charlie")
	r.befriend(a, b)
	check := func(q Querier, fn func(context.Context, Querier, string, string) (bool, error), x, y string, want bool) {
		t.Helper()
		if got, err := fn(r.ctx, q, x, y); err != nil || got != want {
			t.Fatalf("got %v, %v want %v", got, err, want)
		}
	}
	tx, err := r.db.Pool.Begin(r.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(r.ctx) }()
	for _, q := range []Querier{r.db.Pool, tx} {
		check(q, BlockedEitherWay, a.id, b.id, false)
		check(q, CanInteract, a.id, b.id, true)
		check(q, CanInteract, a.id, "11111111-1111-4111-8111-111111111111", true) // unknown ids are not blocks
	}
	_ = tx.Rollback(r.ctx)
	if s, o := r.block(b, a.name); s != 200 { // direction must not matter
		t.Fatal(s, o)
	}
	for _, pair := range [][2]string{{a.id, b.id}, {b.id, a.id}} {
		check(r.db.Pool, BlockedEitherWay, pair[0], pair[1], true)
		check(r.db.Pool, CanInteract, pair[0], pair[1], false)
		check(r.db.Pool, CanInvite, pair[0], pair[1], false)
	}
	check(r.db.Pool, CanInteract, a.id, c.id, true)
	// CanInvite adds the invitee's course_invites_from on top of the block rule.
	check(r.db.Pool, CanInvite, a.id, c.id, false) // default "friends": strangers cannot invite
	r.befriend(a, c)
	check(r.db.Pool, CanInvite, a.id, c.id, true)
	for setting, want := range map[string][2]bool{"nobody": {false, false}, "everyone": {true, true}, "friends": {true, false}} {
		if s, o, _ := r.call("PATCH", "/v1/social/settings", `{"course_invites_from":"`+setting+`"}`, c.token); s != 200 || o["course_invites_from"] != setting {
			t.Fatalf("patch %s: %d %v", setting, s, o)
		}
		d := r.friend("Auth.Stranger" + setting[:2])
		check(r.db.Pool, CanInvite, a.id, c.id, want[0])
		check(r.db.Pool, CanInvite, d.id, c.id, want[1])
	}
	if got := r.sink.named("privacy_setting_changed"); len(got) != 3 || got[0]["props"].(map[string]any)["setting"] != "course_invites_from" {
		t.Fatalf("setting events: %v", got)
	}
	if s, o, _ := r.call("PATCH", "/v1/social/settings", `{"course_invites_from":"friends_of_friends"}`, c.token); s != 422 || o["code"] != "settings_invalid" {
		t.Fatalf("invalid value: %d %v", s, o)
	}
}

func TestBlockAPIIsNeutralBoundedAndOwnedByTheSession(t *testing.T) {
	r := newRig(t)
	a, b, c := r.friend("Api.Alpha"), r.friend("Api.Bravo"), r.friend("Api.Charlie")
	if s, _, _ := r.call("POST", "/v1/social/blocks", `{"username":"Api.Bravo"}`, ""); s != 401 {
		t.Fatalf("anonymous block: %d", s)
	}
	if s, _, _ := r.call("GET", "/v1/social/blocks", "", ""); s != 401 {
		t.Fatalf("anonymous list: %d", s)
	}
	r.seed("Ghost.Hidden", false, "nobody")
	var want map[string]any
	for _, name := range []string{"Nobody.Here", "Api.Alpha", "Ghost.Hidden", "x"} { // unknown, self, hidden, invalid
		s, o := r.block(a, name)
		if s != 404 || o["code"] != "block_unavailable" {
			t.Fatalf("%s: %d %v", name, s, o)
		}
		if want != nil && !reflect.DeepEqual(want, o) {
			t.Fatalf("%s response differs: %v vs %v", name, o, want)
		}
		want = o
	}
	for _, body := range []string{``, `{}`, `{"username":""}`, `{"username":"a","x":1}`, `[]`} {
		if s, o, _ := r.call("POST", "/v1/social/blocks", body, a.token); s != 404 || !reflect.DeepEqual(o, want) {
			t.Fatalf("body %q: %d %v", body, s, o)
		}
	}
	if n := r.scalar(`SELECT count(*) FROM learner_blocks`); n != 0 {
		t.Fatalf("blocks=%d", n)
	}
	// A hidden learner who asked to be my friend can still be blocked.
	ghost := r.friend("Ghost.Asker")
	r.exec(`UPDATE learner_social_settings SET discoverable=false, request_policy='nobody' WHERE learner_id=$1::uuid`, ghost.id)
	r.exec(`INSERT INTO learner_social_settings(learner_id,discoverable,request_policy) VALUES($1::uuid,false,'nobody') ON CONFLICT DO NOTHING`, ghost.id)
	if s, o := r.send(ghost, a.name); s != 200 {
		t.Fatal(s, o)
	}
	if s, o := r.block(a, ghost.name); s != 200 {
		t.Fatalf("block requester: %d %v", s, o)
	}
	// Limit comes from config; an existing block is still idempotent at the limit.
	r.exec(`UPDATE username_config SET block_max=2`)
	if s, o := r.block(a, b.name); s != 200 {
		t.Fatal(s, o)
	}
	if s, o := r.block(a, c.name); s != 409 || o["code"] != "block_limit" {
		t.Fatalf("limit: %d %v", s, o)
	}
	if s, _ := r.block(a, b.name); s != 200 {
		t.Fatalf("idempotent at limit: %d", s)
	}
	// Unblock only touches the actor's own rows and is idempotent.
	if s, _ := r.unblock(b, a.name); s != 200 {
		t.Fatal(s)
	}
	if n := r.scalar(`SELECT count(*) FROM learner_blocks WHERE blocker_id=$1::uuid`, a.id); n != 2 {
		t.Fatalf("another learner's unblock removed rows: %d", n)
	}
	for range 2 {
		if s, o := r.unblock(a, "api.bravo"); s != 200 || o["status"] != "unblocked" {
			t.Fatalf("unblock: %d %v", s, o)
		}
	}
	for _, name := range []string{"Nobody.Here", "x", "Api.Charlie"} {
		if s, _ := r.unblock(a, name); s != 200 {
			t.Fatalf("unblock %s: %d", name, s)
		}
	}
	if got := r.blocks(a); len(got) != 1 || got[0]["username"] != "Ghost.Asker" {
		t.Fatalf("list: %v", got)
	}
	if got := len(r.sink.named("learner_blocked")); got != 2 {
		t.Fatalf("learner_blocked=%d (no unblock event exists)", got)
	}
}

func TestSettingsAndStreakPrivacyApplyOnTheNextRequest(t *testing.T) {
	r := newRig(t)
	a, b, stranger := r.friend("Priv.Alpha"), r.friend("Priv.Bravo"), r.friend("Priv.Stranger")
	r.befriend(a, b)
	r.svc.WithStreaks(fakeStreakSource{b.id: 9})
	streak := func(viewer pair) any {
		s, o, _ := r.call("GET", "/v1/social/profiles/Priv.Bravo", "", viewer.token)
		if s != 200 {
			t.Fatalf("profile: %d %v", s, o)
		}
		return o["streak"]
	}
	listed := func() any { return r.friends(a)[0]["streak"] }
	if got := streak(a); got == nil || got.(map[string]any)["days"] != float64(9) {
		t.Fatalf("friend profile streak=%v", got)
	}
	if got := listed(); got == nil {
		t.Fatal("friend list lost the streak")
	}
	if streak(stranger) != nil {
		t.Fatal("a non-friend sees a streak")
	}
	r.call("PATCH", "/v1/social/settings", `{"show_streak_to_friends":false}`, b.token)
	if streak(a) != nil || listed() != nil {
		t.Fatal("streak still projected after show_streak_to_friends=off")
	}
	r.call("PATCH", "/v1/social/settings", `{"show_streak_to_friends":true}`, b.token)
	if streak(a) == nil {
		t.Fatal("streak not restored")
	}
	// Request policy: next request after the change is refused neutrally.
	r.call("PATCH", "/v1/social/settings", `{"request_policy":"nobody"}`, stranger.token)
	if s, o := r.send(a, stranger.name); s != 404 || o["code"] != "request_unavailable" {
		t.Fatalf("send to nobody: %d %v", s, o)
	}
	r.call("PATCH", "/v1/social/settings", `{"request_policy":"everyone"}`, stranger.token)
	if s, _ := r.send(a, stranger.name); s != 200 {
		t.Fatalf("send after reopening: %d", s)
	}
	// A block hides the profile, so no streak either.
	r.block(b, a.name)
	if s, _, _ := r.call("GET", "/v1/social/profiles/Priv.Bravo", "", a.token); s != 404 {
		t.Fatalf("blocked profile: %d", s)
	}
}

func TestBlocksFollowAccountErasureInBothDirections(t *testing.T) {
	r := newRig(t)
	a, b, c, d := r.friend("Gone.Alpha"), r.friend("Gone.Bravo"), r.friend("Gone.Charlie"), r.friend("Gone.Delta")
	r.block(a, b.name) // placed by the deleted learner
	r.block(c, a.name) // placed against the deleted learner
	r.block(c, d.name) // unrelated, must survive
	r.exec(`DELETE FROM learners WHERE id=$1::uuid`, a.id)
	if n := r.scalar(`SELECT count(*) FROM learner_blocks`); n != 1 {
		t.Fatalf("blocks after erasure=%d", n)
	}
	if n := r.scalar(`SELECT count(*) FROM learner_blocks WHERE blocker_id=$1::uuid AND blocked_id=$2::uuid`, c.id, d.id); n != 1 {
		t.Fatal("unrelated block lost")
	}
	// No hash of the erased learner is retained anywhere in the blocks table.
	var cols []string
	rowsIter, err := r.db.Pool.Query(r.ctx, `SELECT column_name FROM information_schema.columns WHERE table_name='learner_blocks' ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	for rowsIter.Next() {
		var c string
		_ = rowsIter.Scan(&c)
		cols = append(cols, c)
	}
	rowsIter.Close()
	if want := []string{"blocked_id", "blocker_id", "created_at"}; !slices.Equal(cols, want) {
		t.Fatalf("columns=%v want %v", cols, want)
	}
	if got := accounts.ClassifiedLearnerTables["learner_blocks"]; got != "erased" {
		t.Fatalf("classification=%q", got)
	}
}

func TestBlockEventRespectsAnalyticsOptOut(t *testing.T) {
	r := newRig(t)
	a, b := r.friend("Optout.Alpha"), r.friend("Optout.Bravo")
	r.exec(`UPDATE learner_profiles SET analytics_opt_out=true WHERE learner_id=$1::uuid`, a.id)
	if s, o := r.block(a, b.name); s != 200 {
		t.Fatal(s, o)
	}
	if got := r.sink.named("learner_blocked"); len(got) != 0 {
		t.Fatalf("opted-out learner emitted %v", got)
	}
}
