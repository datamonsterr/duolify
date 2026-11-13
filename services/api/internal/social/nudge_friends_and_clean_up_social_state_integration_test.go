//go:build integration

package social

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/google/uuid"
)

func (r *rig) nudge(from pair, friendshipID, key string) (int, map[string]any, string) {
	r.t.Helper()
	status, out, header := r.call("POST", "/v1/social/friends/"+friendshipID+"/nudge", `{"operation_key":"`+key+`"}`, from.token)
	return status, out, header.Get("Retry-After")
}

func (r *rig) friendshipWith(p pair, name string) string {
	r.t.Helper()
	for _, f := range r.friends(p) {
		if f["username"] == name {
			return f["id"].(string)
		}
	}
	r.t.Fatalf("%s has no friend %s", p.name, name)
	return ""
}

type inboxView struct {
	Unread int
	Items  []map[string]any
}

func (r *rig) inbox(p pair) inboxView {
	r.t.Helper()
	status, out, _ := r.call("GET", "/v1/social/nudges", "", p.token)
	if status != 200 {
		r.t.Fatal(status, out)
	}
	return inboxView{Unread: int(out["unread"].(float64)), Items: rows(out["nudges"])}
}

func (r *rig) setting(p pair, body string) {
	r.t.Helper()
	if status, out, _ := r.call("PATCH", "/v1/social/settings", body, p.token); status != 200 {
		r.t.Fatal(status, out)
	}
}

func (r *rig) nudgeRows() int { return r.scalar(`SELECT count(*) FROM friend_nudges`) }

func newKey() string { return uuid.NewString() }

// Once per pair and sender-local day, replayable by operation key, concurrent-safe, and
// the sender's own time zone decides the day. Directions are independent.
func TestNudgeOncePerPairPerSenderDayIdempotentByOperationKey(t *testing.T) {
	r := newRig(t)
	a, b := r.friend("Nudge.Auckland"), r.friend("Nudge.Angeles")
	r.befriend(a, b)
	r.side(a, zoneA, "2026-11-13", nil, nil)
	r.side(b, zoneB, "2026-11-12", nil, nil)
	r.frozenClock("2026-11-14T10:50:00Z") // Auckland 23:50 on 11-14
	ab, ba := r.friendshipWith(a, b.name), r.friendshipWith(b, a.name)

	key := newKey()
	status, first, _ := r.nudge(a, ab, key)
	if status != 200 || first["status"] != "sent" || first["id"] == nil {
		t.Fatal(status, first)
	}
	// Same operation key: the first result, nothing new.
	status, replay, _ := r.nudge(a, ab, key)
	if status != 200 || replay["id"] != first["id"] || r.nudgeRows() != 1 {
		t.Fatal("replay must return the first nudge", status, replay, r.nudgeRows())
	}
	// A new key the same sender day is refused until the sender's next local midnight.
	status, limited, retry := r.nudge(a, ab, newKey())
	seconds, _ := strconv.Atoi(retry)
	if status != 429 || limited["code"] != "nudge_limited" || seconds < 9*60 || seconds > 10*60+1 {
		t.Fatalf("second nudge = %d %v retry=%q, want 429 nudge_limited in ~10 minutes", status, limited, retry)
	}
	// The other direction is a different pair-day.
	if status, out, _ := r.nudge(b, ba, newKey()); status != 200 {
		t.Fatal(status, out)
	}
	// Auckland midnight passes while it is still 11-14 in UTC: a new sender day.
	r.frozenClock("2026-11-14T11:10:00Z")
	if status, out, _ := r.nudge(a, ab, newKey()); status != 200 {
		t.Fatal("next local day must allow a nudge", status, out)
	}
	if got := r.nudgeRows(); got != 3 {
		t.Fatal("rows", got)
	}
	// The friend list reflects the sender's own day.
	got := r.friends(a)[0]["nudge"].(map[string]any)
	if got["sent"] != true || got["next_at"] == nil {
		t.Fatal(got)
	}

	// Concurrent taps with distinct keys: Postgres lets exactly one through.
	c, d := r.friend("Nudge.Concurrent"), r.friend("Nudge.Receiver")
	r.befriend(c, d)
	cd := r.friendshipWith(c, d.name)
	var wg sync.WaitGroup
	codes := make([]int, 8)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i], _, _ = r.nudge(c, cd, newKey())
		}()
	}
	wg.Wait()
	ok, limitedCount := 0, 0
	for _, code := range codes {
		switch code {
		case 200:
			ok++
		case 429:
			limitedCount++
		}
	}
	if ok != 1 || limitedCount != 7 || r.scalar(`SELECT count(*) FROM friend_nudges WHERE sender_id=$1::uuid`, c.id) != 1 {
		t.Fatalf("concurrent nudges: ok=%d limited=%d codes=%v", ok, limitedCount, codes)
	}
	// Concurrent retries of one key: one row, one event.
	e, f := r.friend("Nudge.Retry"), r.friend("Nudge.Retried")
	r.befriend(e, f)
	ef, same := r.friendshipWith(e, f.name), newKey()
	ids := make([]any, 6)
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, out, _ := r.nudge(e, ef, same)
			ids[i] = out["id"]
		}()
	}
	wg.Wait()
	for _, id := range ids {
		if id != ids[0] || id == nil {
			t.Fatal("retries must share one nudge", ids)
		}
	}
	if n := len(r.eventProps(e.id, "friend_nudge_sent")); n != 1 {
		t.Fatal("one event per created nudge, got", n)
	}
}

func TestNudgeInboxShowsOnlyIdentityAndClearsOnRead(t *testing.T) {
	r := newRig(t)
	a, b, c := r.friend("Inbox.Sender"), r.friend("Inbox.Receiver"), r.friend("Inbox.Second")
	r.befriend(a, b)
	r.befriend(c, b)
	r.frozenClock("2026-11-14T12:00:00Z")
	if s, o, _ := r.nudge(a, r.friendshipWith(a, b.name), newKey()); s != 200 {
		t.Fatal(s, o)
	}
	r.frozenClock("2026-11-14T12:05:00Z")
	if s, o, _ := r.nudge(c, r.friendshipWith(c, b.name), newKey()); s != 200 {
		t.Fatal(s, o)
	}
	box := r.inbox(b)
	if box.Unread != 2 || len(box.Items) != 2 {
		t.Fatal(box)
	}
	// Newest first, deterministic.
	order := []string{box.Items[0]["username"].(string), box.Items[1]["username"].(string)}
	if !slices.Equal(order, []string{c.name, a.name}) {
		t.Fatal("inbox must be newest first", order)
	}
	for _, item := range box.Items {
		keys := make([]string, 0, len(item))
		for k := range item {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		if want := []string{"avatar", "created_at", "display_name", "id", "read", "username"}; !slices.Equal(keys, want) {
			t.Fatalf("inbox item exposes %v, want %v", keys, want)
		}
		if item["read"] != false {
			t.Fatal("new nudge must be unread", item)
		}
	}
	// The sender's inbox is empty: nudges are not mirrored.
	if got := r.inbox(a); got.Unread != 0 || len(got.Items) != 0 {
		t.Fatal(got)
	}
	if s, o, _ := r.call("POST", "/v1/social/nudges/read", `{}`, b.token); s != 200 {
		t.Fatal(s, o)
	}
	after := r.inbox(b)
	if after.Unread != 0 || len(after.Items) != 2 || after.Items[0]["read"] != true {
		t.Fatal("read marks, keeps entries", after)
	}
}

// Blocks, removal, the recipient's setting and a recipient who already finished all
// suppress delivery and the sender learns nothing beyond a neutral answer.
func TestNudgeSuppressionByBlockRemovalSettingAndCompletion(t *testing.T) {
	r := newRig(t)
	r.frozenClock("2026-11-14T12:00:00Z")
	sender := r.friend("Sup.Sender")

	// Recipient hides their streak from friends: accepted quietly, never delivered.
	hidden := r.friend("Sup.Hidden")
	r.befriend(sender, hidden)
	r.setting(hidden, `{"show_streak_to_friends":false}`)
	if s, o, _ := r.nudge(sender, r.friendshipWith(sender, hidden.name), newKey()); s != 200 || o["status"] != "sent" {
		t.Fatal("suppressed nudge must look sent", s, o)
	}
	if got := r.inbox(hidden); len(got.Items) != 0 || got.Unread != 0 {
		t.Fatal("setting off must suppress", got)
	}
	if r.scalar(`SELECT count(*) FROM friend_nudges WHERE recipient_id=$1::uuid AND delivered`, hidden.id) != 0 {
		t.Fatal("suppressed row must not be delivered")
	}

	// A delivered nudge disappears when the recipient later switches the setting off.
	late := r.friend("Sup.Late")
	r.befriend(sender, late)
	if s, o, _ := r.nudge(sender, r.friendshipWith(sender, late.name), newKey()); s != 200 {
		t.Fatal(s, o)
	}
	if got := r.inbox(late); len(got.Items) != 1 {
		t.Fatal(got)
	}
	r.setting(late, `{"show_streak_to_friends":false}`)
	if got := r.inbox(late); len(got.Items) != 0 {
		t.Fatal("setting off hides pending nudges", got)
	}

	// Block: pending nudges go with the friendship; later attempts equal an unknown id.
	blocker := r.friend("Sup.Blocker")
	r.befriend(sender, blocker)
	id := r.friendshipWith(sender, blocker.name)
	if s, o, _ := r.nudge(sender, id, newKey()); s != 200 {
		t.Fatal(s, o)
	}
	if s, o := r.block(blocker, sender.name); s != 200 {
		t.Fatal(s, o)
	}
	if got := r.inbox(blocker); len(got.Items) != 0 {
		t.Fatal(got)
	}
	if n := r.scalar(`SELECT count(*) FROM friend_nudges WHERE recipient_id=$1::uuid OR sender_id=$1::uuid`, blocker.id); n != 0 {
		t.Fatal("block must delete the pair's nudges", n)
	}
	s1, blockedOut, _ := r.nudge(sender, id, newKey())
	s2, unknownOut, _ := r.nudge(sender, uuid.NewString(), newKey())
	if s1 != 404 || s2 != 404 || blockedOut["code"] != "nudge_unavailable" || fmt.Sprint(blockedOut) != fmt.Sprint(unknownOut) {
		t.Fatalf("blocked %d %v vs unknown %d %v", s1, blockedOut, s2, unknownOut)
	}

	// Removal behaves the same.
	gone := r.friend("Sup.Gone")
	r.befriend(sender, gone)
	gid := r.friendshipWith(sender, gone.name)
	if s, o, _ := r.nudge(sender, gid, newKey()); s != 200 {
		t.Fatal(s, o)
	}
	if s, o := r.act(gone, "DELETE", "/v1/social/friends/"+gid); s != 200 {
		t.Fatal(s, o)
	}
	if got := r.inbox(gone); len(got.Items) != 0 {
		t.Fatal(got)
	}
	if s, o, _ := r.nudge(sender, gid, newKey()); s != 404 || o["code"] != "nudge_unavailable" {
		t.Fatal(s, o)
	}
	// A stranger cannot nudge through someone else's friendship either.
	stranger := r.friend("Sup.Stranger")
	if s, o, _ := r.nudge(stranger, r.friendshipWith(sender, late.name), newKey()); s != 404 || o["code"] != "nudge_unavailable" {
		t.Fatal(s, o)
	}

	// A friend who already completed today needs no nudge (their own local date).
	done := r.friend("Sup.Done")
	r.befriend(sender, done)
	r.side(done, zoneB, "2026-11-13", []string{"2026-11-14"}, nil) // Los Angeles 04:00 on 11-14
	if s, o, _ := r.nudge(sender, r.friendshipWith(sender, done.name), newKey()); s != 404 || o["code"] != "nudge_unavailable" {
		t.Fatal("finished friend", s, o)
	}
	if got := r.friends(sender); len(got) == 0 {
		t.Fatal("friends listed")
	}
}

func TestNudgeSenderDailyCapAndConfigColumns(t *testing.T) {
	r := newRig(t)
	r.frozenClock("2026-11-14T12:00:00Z")
	var retention, perDay int
	if err := r.db.Pool.QueryRow(r.ctx, `SELECT nudge_retention_days,nudge_sends_per_day FROM username_config WHERE id`).Scan(&retention, &perDay); err != nil || retention != 14 || perDay != 20 {
		t.Fatal("config defaults", retention, perDay, err)
	}
	r.exec(`UPDATE username_config SET nudge_sends_per_day=2`)
	sender := r.friend("Cap.Sender")
	var ids []string
	for _, name := range []string{"Cap.One", "Cap.Two", "Cap.Three"} {
		other := r.friend(name)
		r.befriend(sender, other)
		ids = append(ids, r.friendshipWith(sender, other.name))
	}
	got := make([]int, 0, 3)
	for _, id := range ids { // sequential: the cap is per sender, not per pair
		s, _, _ := r.nudge(sender, id, newKey())
		got = append(got, s)
	}
	if !slices.Equal(got, []int{200, 200, 429}) {
		t.Fatal("sender cap", got)
	}
}

func TestNudgeRetentionIsBoundedByConfig(t *testing.T) {
	r := newRig(t)
	r.frozenClock("2026-11-14T12:00:00Z")
	a, b := r.friend("Keep.Sender"), r.friend("Keep.Receiver")
	r.befriend(a, b)
	r.exec(`INSERT INTO friend_nudges(sender_id,recipient_id,operation_key,sender_local_date,created_at,delivered)
 VALUES($1::uuid,$2::uuid,gen_random_uuid(),'2026-10-01',now()-interval '20 days',true),
        ($1::uuid,$2::uuid,gen_random_uuid(),'2026-11-09',now()-interval '5 days',true),
        ($1::uuid,$2::uuid,gen_random_uuid(),'2026-11-13',now()-interval '1 day',true)`, a.id, b.id)
	if got := r.inbox(b); len(got.Items) != 2 {
		t.Fatal("20-day-old nudge must be gone, 5- and 1-day-old kept", got)
	}
	if r.nudgeRows() != 2 {
		t.Fatal("expired rows are deleted, not hidden", r.nudgeRows())
	}
	r.exec(`UPDATE username_config SET nudge_retention_days=2`)
	if got := r.inbox(b); len(got.Items) != 1 || r.nudgeRows() != 1 {
		t.Fatal("retention is read from config", got, r.nudgeRows())
	}
	if _, err := r.db.Pool.Exec(r.ctx, `UPDATE username_config SET nudge_retention_days=15`); err == nil {
		t.Fatal("retention beyond the dump window must be refused by the database")
	}
}

func TestNudgeAnalyticsAreUUIDsAndEnumsOnly(t *testing.T) {
	r := newRig(t)
	r.frozenClock("2026-11-14T12:00:00Z")
	a, b := r.friend("Stat.Sender"), r.friend("Stat.Receiver")
	r.befriend(a, b)
	id := r.friendshipWith(a, b.name)
	_, out, _ := r.nudge(a, id, newKey())
	r.nudge(a, id, newKey()) // limited: no event
	events := r.eventProps(a.id, "friend_nudge_sent")
	if len(events) != 1 {
		t.Fatal("one event per created nudge", events)
	}
	props := events[0]
	if props["event_id"] != out["id"] || props["local_date"] != "2026-11-14" {
		t.Fatal(props)
	}
	raw, _ := json.Marshal(props)
	for _, banned := range []string{a.name, b.name, strings.ToLower(b.name), b.id, "username", "display_name"} {
		if strings.Contains(string(raw), banned) {
			t.Fatalf("event leaks %q: %s", banned, raw)
		}
	}
	if len(r.eventProps(b.id, "friend_nudge_sent")) != 0 {
		t.Fatal("recipient emits nothing")
	}
	// Opt-out is honoured by the shared consent gate.
	r.exec(`UPDATE learner_profiles SET analytics_opt_out=true WHERE learner_id=$1::uuid`, b.id)
	r.frozenClock("2026-11-15T12:00:00Z")
	r.nudge(b, r.friendshipWith(b, a.name), newKey())
	if len(r.eventProps(b.id, "friend_nudge_sent")) != 0 {
		t.Fatal("opted-out learner must emit nothing")
	}
}

// connected builds two friends with a shared streak, nudges both ways, a pending
// request to a third learner and a friend link.
type connected struct{ gone, survivor, third pair }

func (r *rig) connected(gone, survivor string) connected {
	c := connected{gone: r.friend(gone), survivor: r.friend(survivor), third: r.friend(gone + ".Third")}
	r.oldFriends(c.gone, c.survivor)
	r.befriend(c.gone, c.third)
	r.frozenClock("2026-11-14T12:00:00Z")
	r.side(c.gone, zoneA, "2026-11-14", []string{"2026-11-13", "2026-11-14"}, nil)
	r.side(c.survivor, zoneB, "2026-11-13", []string{"2026-11-12", "2026-11-13"}, nil)
	r.evaluate(c.gone)
	for _, p := range []struct{ from, to pair }{{c.gone, c.survivor}, {c.survivor, c.gone}, {c.third, c.gone}} {
		r.frozenClock("2026-11-14T12:00:00Z")
		if s, o, _ := r.nudge(p.from, r.friendshipWith(p.from, p.to.name), newKey()); s != 200 {
			r.t.Fatal(s, o)
		}
	}
	r.exec(`INSERT INTO friend_links(owner_id,token_hash,expires_at) VALUES($1::uuid,repeat('c',64),now()+interval '1 day')`, c.gone.id)
	other := r.friend(gone + ".Pending")
	if s, o := r.send(other, c.gone.name); s != 200 {
		r.t.Fatal(s, o)
	}
	return c
}

func (r *rig) assertCleaned(c connected) {
	r.t.Helper()
	for _, table := range []string{"friendships", "friend_requests", "friend_nudges"} {
		if n := r.scalar(`SELECT count(*) FROM `+table+` WHERE `+map[string]string{
			"friendships":     "learner_low=$1::uuid OR learner_high=$1::uuid",
			"friend_requests": "sender_id=$1::uuid OR recipient_id=$1::uuid",
			"friend_nudges":   "sender_id=$1::uuid OR recipient_id=$1::uuid",
		}[table], c.gone.id); n != 0 {
			r.t.Fatalf("%s rows of the erased learner = %d", table, n)
		}
	}
	for _, table := range []string{"friend_links", "learner_usernames", "learner_social_settings"} {
		col := map[string]string{"friend_links": "owner_id", "learner_usernames": "learner_id", "learner_social_settings": "learner_id"}[table]
		if n := r.scalar(`SELECT count(*) FROM `+table+` WHERE `+col+`=$1::uuid`, c.gone.id); n != 0 {
			r.t.Fatalf("%s rows of the erased learner = %d", table, n)
		}
	}
	if n := r.scalar(`SELECT count(*) FROM friend_streaks`); n != 0 {
		r.t.Fatal("shared streak must end", n)
	}
	// The survivor sees nothing of the erased identity anywhere.
	var seen strings.Builder
	for _, path := range []string{"/v1/social/friends", "/v1/social/nudges", "/v1/social/friend-requests", "/v1/social/blocks"} {
		s, out, _ := r.call("GET", path, "", c.survivor.token)
		if s != 200 {
			r.t.Fatal(path, s, out)
		}
		raw, _ := json.Marshal(out)
		seen.Write(raw)
	}
	for _, needle := range []string{c.gone.name, strings.ToLower(c.gone.name), c.gone.id} {
		if strings.Contains(seen.String(), needle) {
			r.t.Fatalf("survivor still sees %q: %s", needle, seen.String())
		}
	}
	if got := r.friends(c.survivor); len(got) != 0 {
		r.t.Fatal("survivor friends", got)
	}
	if got := r.inbox(c.survivor); got.Unread != 0 || len(got.Items) != 0 {
		r.t.Fatal("survivor inbox", got)
	}
	// The name stays held for its previous owner only.
	canonical := Canonical(c.gone.name)
	if n := r.scalar(`SELECT count(*) FROM username_holds WHERE canonical=$1 AND held_until>now()+interval '29 days'
 AND previous_owner_ref=encode(sha256(convert_to($2::text,'UTF8')),'hex')`, canonical, c.gone.id); n != 1 {
		r.t.Fatal("released name must be held 30 days, pseudonymously", n)
	}
	claimer, _ := r.login(strings.ToLower(c.gone.name) + ".claimer@example.test")
	if s, o := r.claim(claimer, c.gone.name); s != 409 || o["code"] != "username_taken" {
		r.t.Fatal("held name must not be claimable", s, o)
	}
	if n := r.scalar(`SELECT count(*) FROM (SELECT canonical FROM learner_usernames GROUP BY canonical HAVING count(*)>1) d`); n != 0 {
		r.t.Fatal("usernames must stay unique")
	}
}

func TestDeletingAConnectedAccountRemovesSocialStateWithoutTrace(t *testing.T) {
	r := newRig(t)
	c := r.connected("Del.Gone", "Del.Survivor")
	if got, ok := r.streak(c.gone, c.survivor); !ok || got.Days == 0 {
		t.Fatal("setup must produce a shared streak", got, ok)
	}
	// Exactly what the account deletion transaction does to the learner row.
	r.exec(`DELETE FROM learners WHERE id=$1::uuid`, c.gone.id)
	r.assertCleaned(c)
}

// A restore brings the erased learner back; the replayed tombstone must give the same
// result and keep unrelated holds and uniqueness intact.
func TestRestoreReplayOfTombstoneKeepsHoldsAndUniqueness(t *testing.T) {
	r := newRig(t)
	c := r.connected("Rst.Gone", "Rst.Survivor")
	r.exec(`INSERT INTO username_holds(canonical,previous_owner_ref,held_until) VALUES('earlier.holder',repeat('d',64),now()+interval '12 days')`)
	var heldBefore time.Time
	if err := r.db.Pool.QueryRow(r.ctx, `SELECT held_until FROM username_holds WHERE canonical='earlier.holder'`).Scan(&heldBefore); err != nil {
		t.Fatal(err)
	}
	r.exec(`INSERT INTO account_deletion_tombstones(learner_reference) VALUES($1) ON CONFLICT DO NOTHING`, accounts.LearnerReference(c.gone.id))
	for range 2 { // replay is idempotent
		if err := accounts.ApplyDeletionTombstones(r.ctx, r.db.Pool); err != nil {
			t.Fatal(err)
		}
	}
	r.assertCleaned(c)
	var heldAfter time.Time
	var owner string
	if err := r.db.Pool.QueryRow(r.ctx, `SELECT held_until,previous_owner_ref FROM username_holds WHERE canonical='earlier.holder'`).Scan(&heldAfter, &owner); err != nil || !heldAfter.Equal(heldBefore) || owner != strings.Repeat("d", 64) {
		t.Fatal("unrelated active hold must be untouched", heldAfter, heldBefore, owner, err)
	}
	if n := r.scalar(`SELECT count(*) FROM learners WHERE id=$1::uuid`, c.gone.id); n != 0 {
		t.Fatal("tombstoned learner must stay erased")
	}
}
