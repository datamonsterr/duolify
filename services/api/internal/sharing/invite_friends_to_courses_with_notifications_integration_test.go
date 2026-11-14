//go:build integration

package sharing_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
	"github.com/datamonsterr/duolify/services/api/internal/social"
)

// ownerName is the fixture owner's username (backuptest.Seed); blocks address learners by it.
const ownerName = "Backup.Renamed"

type inviteEnv struct {
	f    *backuptest.Fixture
	s    *sharing.Service
	soc  *social.Service
	ev   *capture
	push *sharing.FakePush
	src  string
}

// inviteWorld is a published Course owned by `owner`, a sharing service with the fake push
// adapter, and a social service whose block transaction cancels invites (the production wiring).
func inviteWorld(t *testing.T, mutate func(*sharing.Config)) *inviteEnv {
	t.Helper()
	f := backuptest.New(t)
	f.Seed(t)
	ev := &capture{}
	s, _ := enrollService(t, f, ev, mutate)
	push := sharing.NewFakePush()
	s.WithPush(push)
	soc := social.New(f.DB.Pool, nil)
	soc.OnBlock(s.CancelInvitesOnBlock)
	consent(t, f, owner, false)
	consent(t, f, stranger, false)
	// The shared fixture gives the owner restrictive social settings; invites are tested from a default learner.
	if _, err := f.DB.Pool.Exec(t.Context(), `DELETE FROM learner_social_settings WHERE learner_id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	src := seedReusable(t, f, "Astronomy")
	publish(t, s, src)
	return &inviteEnv{f: f, s: s, soc: soc, ev: ev, push: push, src: src}
}

// person is a learner with a username; friends are linked straight in the table.
func (e *inviteEnv) person(t *testing.T, name string) string {
	t.Helper()
	id := newLearner(t, e.f, name)
	if _, err := e.soc.Claim(t.Context(), id, strings.ToLower(name)); err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *inviteEnv) befriend(t *testing.T, a, b string) string {
	t.Helper()
	var id string
	if err := e.f.DB.Pool.QueryRow(t.Context(), `INSERT INTO friendships(learner_low,learner_high) VALUES (LEAST($1::uuid,$2::uuid),GREATEST($1::uuid,$2::uuid)) RETURNING id::text`, a, b).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *inviteEnv) setting(t *testing.T, id, from string) {
	t.Helper()
	if _, err := e.f.DB.Pool.Exec(t.Context(), `INSERT INTO learner_social_settings(learner_id,course_invites_from) VALUES($1,$2) ON CONFLICT (learner_id) DO UPDATE SET course_invites_from=EXCLUDED.course_invites_from`, id, from); err != nil {
		t.Fatal(err)
	}
}

func (e *inviteEnv) n(t *testing.T, q string, args ...any) int {
	t.Helper()
	return count(t, e.f, q, args...)
}

func TestInviteStateAndDelivery(t *testing.T) {
	e := inviteWorld(t, nil)
	ctx := t.Context()
	ada := e.person(t, "Ada")
	e.befriend(t, owner, ada)
	var inviteID string

	t.Run("send_by_username_is_pending_learner_id_based_and_in_the_inbox", func(t *testing.T) {
		inv, err := e.s.SendInvite(ctx, owner, e.src, sharing.InviteInput{Username: "@Ada"})
		if err != nil {
			t.Fatal(err)
		}
		inviteID = inv.ID
		if inv.Status != "pending" || inv.ExpiresAt.IsZero() {
			t.Fatalf("invite %+v", inv)
		}
		if n := e.n(t, `SELECT count(*) FROM course_invites WHERE id=$1 AND recipient_id=$2 AND sender_id=$3 AND course_id=$4 AND method='username' AND delivered AND expires_at > now()+interval '29 days'`, inv.ID, ada, owner, e.src); n != 1 {
			t.Fatal("invite row must be learner-id based, delivered and expire in 30 days")
		}
		list, unread, err := e.s.ReceivedInvites(ctx, ada)
		if err != nil || unread != 1 || len(list) != 1 || list[0].ID != inv.ID || list[0].Course.Title != "Astronomy" || list[0].Sender.DisplayName == "" {
			t.Fatalf("inbox %+v unread=%d err=%v", list, unread, err)
		}
		if err = e.s.MarkInvitesRead(ctx, ada); err != nil {
			t.Fatal(err)
		}
		if _, unread, _ = e.s.ReceivedInvites(ctx, ada); unread != 0 {
			t.Fatal("read must clear the unread count")
		}
		if got := e.ev.named("course_invite_sent"); len(got) != 1 || got[0]["method"] != "username" || got[0]["invite_id"] != inv.ID || got[0]["course_id"] != e.src || got[0]["_id"] != owner {
			t.Fatalf("sent event %+v", got)
		}
	})

	t.Run("sender_lists_by_course_and_replay_keeps_one_invite", func(t *testing.T) {
		list, err := e.s.SentInvites(ctx, owner, e.src)
		if err != nil || len(list) != 1 || list[0].ID != inviteID || list[0].Recipient.Username != "ada" {
			t.Fatalf("sent %+v %v", list, err)
		}
		if _, err = e.s.SentInvites(ctx, stranger, e.src); !errors.Is(err, sharing.ErrCourseUnavailable) {
			t.Fatalf("a non-owner must get the neutral course error, got %v", err)
		}
		if _, err = e.s.SendInvite(ctx, owner, e.src, sharing.InviteInput{Username: "ada"}); !errors.Is(err, sharing.ErrInviteDuplicate) {
			t.Fatalf("one pending invite per recipient and course, got %v", err)
		}
	})

	t.Run("operation_key_replays", func(t *testing.T) {
		bob := e.person(t, "Bob")
		e.befriend(t, owner, bob)
		key := "c3333333-3333-4333-8333-333333333333"
		a, err := e.s.SendInvite(ctx, owner, e.src, sharing.InviteInput{Username: "bob", Key: key})
		if err != nil {
			t.Fatal(err)
		}
		b, err := e.s.SendInvite(ctx, owner, e.src, sharing.InviteInput{Username: "bob", Key: key})
		if err != nil || a.ID != b.ID {
			t.Fatalf("replay %v %v %v", a, b, err)
		}
		if n := e.n(t, `SELECT count(*) FROM course_invites WHERE recipient_id=$1`, bob); n != 1 {
			t.Fatalf("replay created %d invites", n)
		}
	})

	t.Run("send_from_the_friend_list", func(t *testing.T) {
		cy := e.person(t, "Cyd")
		fid := e.befriend(t, owner, cy)
		inv, err := e.s.SendInvite(ctx, owner, e.src, sharing.InviteInput{FriendshipID: fid})
		if err != nil {
			t.Fatal(err)
		}
		if n := e.n(t, `SELECT count(*) FROM course_invites WHERE id=$1 AND recipient_id=$2 AND method='friend'`, inv.ID, cy); n != 1 {
			t.Fatal("friend invite must resolve the friend by friendship id")
		}
		if _, err = e.s.SendInvite(ctx, stranger, e.src, sharing.InviteInput{FriendshipID: fid}); err == nil {
			t.Fatal("a friendship id of another pair must not work")
		}
	})

	t.Run("refusals_are_neutral", func(t *testing.T) {
		if _, err := e.s.SendInvite(ctx, owner, e.src, sharing.InviteInput{Username: "nobody-by-that-name"}); !errors.Is(err, sharing.ErrInviteUnavailable) {
			t.Fatalf("unknown username: %v", err)
		}
		if _, err := e.s.SendInvite(ctx, stranger, e.src, sharing.InviteInput{Username: "ada"}); !errors.Is(err, sharing.ErrCourseUnavailable) {
			t.Fatalf("not the owner: %v", err)
		}
		priv := seedReusable(t, e.f, "Private one")
		if _, err := e.s.SendInvite(ctx, owner, priv, sharing.InviteInput{Username: "ada"}); !errors.Is(err, sharing.ErrCourseNotShared) {
			t.Fatalf("private course must ask for link visibility: %v", err)
		}
		nob := e.person(t, "Nia")
		e.befriend(t, owner, nob)
		e.setting(t, nob, "nobody")
		inv, err := e.s.SendInvite(ctx, owner, e.src, sharing.InviteInput{Username: "nia"})
		if err != nil || inv.Status != "pending" {
			t.Fatalf("nobody must look sent to the sender: %+v %v", inv, err)
		}
		if list, unread, _ := e.s.ReceivedInvites(ctx, nob); len(list) != 0 || unread != 0 {
			t.Fatalf("recipient with nobody saw %+v", list)
		}
		if n := e.n(t, `SELECT count(*) FROM course_invite_deliveries WHERE invite_id=$1`, inv.ID); n != 0 {
			t.Fatal("a refused invite must have no delivery rows")
		}
		if sent, _ := e.s.SentInvites(ctx, owner, e.src); !hasInvite(sent, inv.ID, "pending") {
			t.Fatal("the sender must see the refused invite as pending")
		}
		// A non-friend under the default setting is the same neutral outcome.
		far := e.person(t, "Far")
		if inv, err = e.s.SendInvite(ctx, owner, e.src, sharing.InviteInput{Username: "far"}); err != nil || inv.Status != "pending" {
			t.Fatalf("non friend %+v %v", inv, err)
		}
		if list, _, _ := e.s.ReceivedInvites(ctx, far); len(list) != 0 {
			t.Fatal("a non friend must receive nothing under the default setting")
		}
		if _, err = e.s.AcceptInvite(ctx, far, inv.ID); !errors.Is(err, sharing.ErrInviteUnavailable) {
			t.Fatalf("a suppressed invite must not be acceptable: %v", err)
		}
		// Everyone opens the door to a non friend.
		e.setting(t, far, "everyone")
		other := e.person(t, "Dee")
		e.setting(t, other, "everyone")
		if inv, err = e.s.SendInvite(ctx, owner, e.src, sharing.InviteInput{Username: "dee"}); err != nil {
			t.Fatal(err)
		}
		if list, _, _ := e.s.ReceivedInvites(ctx, other); len(list) != 1 {
			t.Fatal("everyone must deliver to a non friend")
		}
	})

	t.Run("daily_cap_counts_every_invite_the_sender_made", func(t *testing.T) {
		w := inviteWorld(t, func(c *sharing.Config) { c.InviteSendsPerDay = 3 })
		for i := 0; i < 3; i++ {
			id := w.person(t, fmt.Sprintf("Cap%d", i))
			w.befriend(t, owner, id)
			if i == 1 {
				w.setting(t, id, "nobody") // refused invites still count
			}
			if _, err := w.s.SendInvite(ctx, owner, w.src, sharing.InviteInput{Username: fmt.Sprintf("cap%d", i)}); err != nil {
				t.Fatalf("send %d: %v", i, err)
			}
		}
		id := w.person(t, "Capx")
		w.befriend(t, owner, id)
		if _, err := w.s.SendInvite(ctx, owner, w.src, sharing.InviteInput{Username: "capx"}); !errors.Is(err, sharing.ErrInviteRateLimited) {
			t.Fatalf("over the cap: %v", err)
		}
		if _, err := w.f.DB.Pool.Exec(ctx, `UPDATE course_invites SET created_at=now()-interval '25 hours'`); err != nil {
			t.Fatal(err)
		}
		if _, err := w.s.SendInvite(ctx, owner, w.src, sharing.InviteInput{Username: "capx"}); err != nil {
			t.Fatalf("after the rolling day: %v", err)
		}
	})
}

func hasInvite(list []sharing.SentInvite, id, status string) bool {
	for _, i := range list {
		if i.ID == id && i.Status == status {
			return true
		}
	}
	return false
}

func TestInviteExpiryAcceptDeclineRevoke(t *testing.T) {
	e := inviteWorld(t, nil)
	ctx := t.Context()
	send := func(name string) (string, sharing.Invite) {
		id := e.person(t, name)
		e.befriend(t, owner, id)
		inv, err := e.s.SendInvite(ctx, owner, e.src, sharing.InviteInput{Username: strings.ToLower(name)})
		if err != nil {
			t.Fatal(err)
		}
		return id, inv
	}

	t.Run("expiry_after_thirty_days_hides_refuses_and_allows_a_new_invite", func(t *testing.T) {
		id, inv := send("Exp")
		if _, err := e.f.DB.Pool.Exec(ctx, `UPDATE course_invites SET expires_at=now()-interval '1 second' WHERE id=$1`, inv.ID); err != nil {
			t.Fatal(err)
		}
		if list, _, _ := e.s.ReceivedInvites(ctx, id); len(list) != 0 {
			t.Fatal("an expired invite must leave the inbox")
		}
		if _, err := e.s.AcceptInvite(ctx, id, inv.ID); !errors.Is(err, sharing.ErrInviteUnavailable) {
			t.Fatalf("accepting an expired invite: %v", err)
		}
		if err := e.s.SweepInvites(ctx); err != nil {
			t.Fatal(err)
		}
		if n := e.n(t, `SELECT count(*) FROM course_invites WHERE id=$1 AND status='expired' AND resolved_at IS NOT NULL`, inv.ID); n != 1 {
			t.Fatal("the sweep must persist expiry")
		}
		if n := e.n(t, `SELECT count(*) FROM course_copies WHERE learner_id=$1`, id); n != 0 {
			t.Fatal("expiry must not enroll")
		}
		if _, err := e.s.SendInvite(ctx, owner, e.src, sharing.InviteInput{Username: "exp"}); err != nil {
			t.Fatalf("a fresh invite after expiry: %v", err)
		}
	})

	t.Run("expired_but_unswept_does_not_block_a_new_invite", func(t *testing.T) {
		_, inv := send("Lazy")
		if _, err := e.f.DB.Pool.Exec(ctx, `UPDATE course_invites SET expires_at=now()-interval '1 hour' WHERE id=$1`, inv.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := e.s.SendInvite(ctx, owner, e.src, sharing.InviteInput{Username: "lazy"}); err != nil {
			t.Fatalf("lazy expiry must free the slot: %v", err)
		}
	})

	t.Run("accept_enrolls_once_into_a_pinned_copy", func(t *testing.T) {
		id, inv := send("Acc")
		var wg sync.WaitGroup
		results := make([]string, 5)
		for i := range results {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if out, err := e.s.AcceptInvite(ctx, id, inv.ID); err == nil {
					results[i] = out.CourseID
				}
			}()
		}
		wg.Wait()
		first := results[0]
		for _, r := range results {
			if r == "" || r != first {
				t.Fatalf("every concurrent accept must return the one copy: %v", results)
			}
		}
		if n := e.n(t, `SELECT count(*) FROM course_copies WHERE learner_id=$1 AND source_course_id=$2 AND copy_course_id=$3`, id, e.src, first); n != 1 {
			t.Fatalf("copies = %d", n)
		}
		if n := e.n(t, `SELECT count(*) FROM course_copies WHERE learner_id=$1`, id); n != 1 {
			t.Fatal("exactly one copy")
		}
		if n := e.n(t, `SELECT count(*) FROM course_invites WHERE id=$1 AND status='accepted' AND copy_course_id=$2`, inv.ID, first); n != 1 {
			t.Fatal("invite must record the copy")
		}
		consent(t, e.f, id, false)
		if got := e.ev.named("course_invite_accepted"); len(got) == 0 || got[0]["invite_id"] != inv.ID {
			t.Fatalf("accepted events %+v", got)
		}
		enrolled := 0
		for _, ev := range e.ev.named("course_enrolled_published") {
			if ev["source"] == "invite" && ev["_id"] == id {
				enrolled++
			}
		}
		if enrolled != 1 {
			t.Fatalf("course_enrolled_published(source=invite) emitted %d times", enrolled)
		}
		if _, err := e.s.DeclineInvite(ctx, id, inv.ID); !errors.Is(err, sharing.ErrInviteUnavailable) {
			t.Fatalf("decline after accept: %v", err)
		}
		if _, err := e.s.RevokeInvite(ctx, owner, inv.ID); !errors.Is(err, sharing.ErrInviteUnavailable) {
			t.Fatalf("revoke after accept: %v", err)
		}
	})

	t.Run("accept_is_idempotent_with_an_existing_enrollment", func(t *testing.T) {
		id, inv := send("Had")
		token, err := e.s.PublishLink(ctx, owner, e.src)
		if err != nil {
			t.Fatal(err)
		}
		consent(t, e.f, id, false)
		enrolled, err := e.s.Enroll(ctx, id, token)
		if err != nil {
			t.Fatal(err)
		}
		out, err := e.s.AcceptInvite(ctx, id, inv.ID)
		if err != nil || out.CourseID != enrolled.CourseID {
			t.Fatalf("accept %+v want %s: %v", out, enrolled.CourseID, err)
		}
		if n := e.n(t, `SELECT count(*) FROM course_copies WHERE learner_id=$1`, id); n != 1 {
			t.Fatalf("copies=%d", n)
		}
	})

	t.Run("only_the_recipient_accepts_and_decline_is_final", func(t *testing.T) {
		id, inv := send("Dec")
		if _, err := e.s.AcceptInvite(ctx, stranger, inv.ID); !errors.Is(err, sharing.ErrInviteUnavailable) {
			t.Fatalf("stranger accept: %v", err)
		}
		if _, err := e.s.DeclineInvite(ctx, owner, inv.ID); !errors.Is(err, sharing.ErrInviteUnavailable) {
			t.Fatalf("sender decline: %v", err)
		}
		out, err := e.s.DeclineInvite(ctx, id, inv.ID)
		if err != nil || out.Status != "declined" {
			t.Fatalf("decline %+v %v", out, err)
		}
		if again, err := e.s.DeclineInvite(ctx, id, inv.ID); err != nil || again.Status != "declined" {
			t.Fatalf("decline replay %+v %v", again, err)
		}
		if _, err = e.s.AcceptInvite(ctx, id, inv.ID); !errors.Is(err, sharing.ErrInviteUnavailable) {
			t.Fatalf("accept after decline: %v", err)
		}
		if got := e.ev.named("course_invite_declined"); len(got) != 1 || got[0]["invite_id"] != inv.ID {
			t.Fatalf("declined events %+v", got)
		}
		if list, _, _ := e.s.ReceivedInvites(ctx, id); len(list) != 0 {
			t.Fatal("a declined invite must leave the inbox")
		}
	})

	t.Run("revoke_by_the_sender_only", func(t *testing.T) {
		id, inv := send("Rev")
		if _, err := e.s.RevokeInvite(ctx, id, inv.ID); !errors.Is(err, sharing.ErrInviteUnavailable) {
			t.Fatalf("recipient revoke: %v", err)
		}
		out, err := e.s.RevokeInvite(ctx, owner, inv.ID)
		if err != nil || out.Status != "revoked" {
			t.Fatalf("revoke %+v %v", out, err)
		}
		if _, err = e.s.AcceptInvite(ctx, id, inv.ID); !errors.Is(err, sharing.ErrInviteUnavailable) {
			t.Fatalf("accept revoked: %v", err)
		}
	})

	t.Run("a_rename_does_not_break_the_invite", func(t *testing.T) {
		id, inv := send("Old")
		if _, err := e.f.DB.Pool.Exec(ctx, `UPDATE learner_usernames SET username='newname' WHERE learner_id=$1`, id); err != nil {
			t.Fatal(err)
		}
		sent, _ := e.s.SentInvites(ctx, owner, e.src)
		found := false
		for _, s := range sent {
			if s.ID == inv.ID && s.Recipient.Username == "newname" {
				found = true
			}
		}
		if !found {
			t.Fatalf("sent list must show the new name: %+v", sent)
		}
		if _, err := e.s.AcceptInvite(ctx, id, inv.ID); err != nil {
			t.Fatalf("accept after rename: %v", err)
		}
	})

	t.Run("a_course_made_private_cannot_be_accepted", func(t *testing.T) {
		id, inv := send("Priv")
		if _, err := e.f.DB.Pool.Exec(ctx, `UPDATE courses SET visibility='private' WHERE id=$1`, e.src); err != nil {
			t.Fatal(err)
		}
		defer func() {
			_, _ = e.f.DB.Pool.Exec(ctx, `UPDATE courses SET visibility='link' WHERE id=$1`, e.src)
		}()
		if list, _, _ := e.s.ReceivedInvites(ctx, id); len(list) != 0 {
			t.Fatal("a private course must disappear from the inbox")
		}
		if _, err := e.s.AcceptInvite(ctx, id, inv.ID); !errors.Is(err, sharing.ErrInviteUnavailable) {
			t.Fatalf("accept on a private course: %v", err)
		}
		if n := e.n(t, `SELECT count(*) FROM course_invites WHERE id=$1 AND status='pending'`, inv.ID); n != 1 {
			t.Fatal("a refused accept must leave the invite pending")
		}
	})
}

func TestInviteBlocksAreAuthorizedInTheWriteTransaction(t *testing.T) {
	e := inviteWorld(t, nil)
	ctx := t.Context()

	t.Run("a_block_cancels_pending_invites_in_both_directions", func(t *testing.T) {
		ada := e.person(t, "Blk")
		e.befriend(t, owner, ada)
		inv, err := e.s.SendInvite(ctx, owner, e.src, sharing.InviteInput{Username: "blk"})
		if err != nil {
			t.Fatal(err)
		}
		if err = e.soc.Block(ctx, ada, ownerName); err != nil {
			t.Fatal(err)
		}
		if n := e.n(t, `SELECT count(*) FROM course_invites WHERE id=$1 AND status='revoked'`, inv.ID); n != 1 {
			t.Fatal("block must cancel the invite in the block transaction")
		}
		if list, _, _ := e.s.ReceivedInvites(ctx, ada); len(list) != 0 {
			t.Fatal("blocked invite must not show")
		}
		// A new invite across a block is the same answer as an unknown username.
		if _, err = e.s.SendInvite(ctx, owner, e.src, sharing.InviteInput{Username: "blk"}); !errors.Is(err, sharing.ErrInviteUnavailable) {
			t.Fatalf("send after block must look like an unknown username: %v", err)
		}
		if sent, _ := e.s.SentInvites(ctx, owner, e.src); hasInvite(sent, inv.ID, "revoked") {
			t.Fatal("the sender must not learn of the block through its list")
		}
	})

	t.Run("a_recipient_invite_to_the_blocker_is_cancelled_too", func(t *testing.T) {
		// Both directions: the blocked learner owns a course and had invited the blocker.
		w := inviteWorld(t, nil)
		peer := w.person(t, "Peer")
		w.befriend(t, owner, peer)
		other := seedReusable(t, w.f, "Peer course")
		if _, err := w.f.DB.Pool.Exec(ctx, `UPDATE courses SET learner_id=$1 WHERE id=$2`, peer, other); err != nil {
			t.Fatal(err)
		}
		if _, err := w.f.DB.Pool.Exec(ctx, `INSERT INTO course_invites(course_id,sender_id,recipient_id,operation_key,method,delivered,expires_at) VALUES($1,$2,$3,gen_random_uuid(),'username',true,now()+interval '1 day')`, other, peer, owner); err != nil {
			t.Fatal(err)
		}
		if err := w.soc.Block(ctx, owner, "peer"); err != nil {
			t.Fatal(err)
		}
		if n := w.n(t, `SELECT count(*) FROM course_invites WHERE sender_id=$1 AND recipient_id=$2 AND status='pending'`, peer, owner); n != 0 {
			t.Fatal("the reverse invite must be cancelled")
		}
	})

	t.Run("a_block_racing_an_invite_never_leaves_a_deliverable_invite", func(t *testing.T) {
		for i := 0; i < 12; i++ {
			r := e.person(t, fmt.Sprintf("Race%d", i))
			e.befriend(t, owner, r)
			start := make(chan struct{})
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				_, _ = e.s.SendInvite(ctx, owner, e.src, sharing.InviteInput{Username: fmt.Sprintf("race%d", i)})
			}()
			go func() {
				defer wg.Done()
				<-start
				_ = e.soc.Block(ctx, r, ownerName)
			}()
			close(start)
			wg.Wait()
			if n := e.n(t, `SELECT count(*) FROM learner_blocks WHERE blocker_id=$1`, r); n != 1 {
				t.Fatalf("iteration %d: block missing", i)
			}
			if n := e.n(t, `SELECT count(*) FROM course_invites WHERE recipient_id=$1 AND status='pending' AND delivered`, r); n != 0 {
				t.Fatalf("iteration %d: a block raced an invite and left a deliverable invite", i)
			}
			if list, _, _ := e.s.ReceivedInvites(ctx, r); len(list) != 0 {
				t.Fatalf("iteration %d: inbox shows %+v", i, list)
			}
		}
	})

	t.Run("a_block_after_send_stops_accept_and_push", func(t *testing.T) {
		w := inviteWorld(t, nil)
		cat := w.person(t, "Cat")
		w.befriend(t, owner, cat)
		inv, err := w.s.SendInvite(ctx, owner, w.src, sharing.InviteInput{Username: "cat"})
		if err != nil {
			t.Fatal(err)
		}
		// Owner blocks the recipient (a plain row, as the E6 service does it).
		if _, err = w.f.DB.Pool.Exec(ctx, `INSERT INTO learner_blocks(blocker_id,blocked_id) VALUES($1,$2)`, owner, cat); err != nil {
			t.Fatal(err)
		}
		if err = w.s.DrainInviteDeliveries(ctx); err != nil {
			t.Fatal(err)
		}
		if len(w.push.Sent()) != 0 {
			t.Fatal("push must re-check blocks at send time")
		}
		if _, err = w.s.AcceptInvite(ctx, cat, inv.ID); !errors.Is(err, sharing.ErrInviteUnavailable) {
			t.Fatalf("accept across a block: %v", err)
		}
	})
}

func TestInviteOutboxAndPushAdapter(t *testing.T) {
	ctx := t.Context()

	t.Run("in_app_is_always_written_and_push_is_sent_once_with_only_ids", func(t *testing.T) {
		e := inviteWorld(t, nil)
		ada := e.person(t, "Pat")
		e.befriend(t, owner, ada)
		inv, err := e.s.SendInvite(ctx, owner, e.src, sharing.InviteInput{Username: "pat"})
		if err != nil {
			t.Fatal(err)
		}
		if n := e.n(t, `SELECT count(*) FROM course_invite_deliveries WHERE invite_id=$1 AND channel='in_app' AND status='sent'`, inv.ID); n != 1 {
			t.Fatal("in-app delivery is written with the invite")
		}
		if n := e.n(t, `SELECT count(*) FROM course_invite_deliveries WHERE invite_id=$1 AND channel='push' AND status='pending'`, inv.ID); n != 1 {
			t.Fatal("push must be queued")
		}
		for i := 0; i < 3; i++ { // a repeated drain never sends twice
			if err = e.s.DrainInviteDeliveries(ctx); err != nil {
				t.Fatal(err)
			}
		}
		sent := e.push.Sent()
		if len(sent) != 1 || sent[0].RecipientID != ada || sent[0].InviteID != inv.ID || sent[0].Kind != "course_invite" {
			t.Fatalf("push %+v", sent)
		}
		for _, banned := range []string{"Astronomy", "pat", "Pat", "Look up", "owner"} {
			if strings.Contains(fmt.Sprintf("%+v", sent), banned) {
				t.Fatalf("push payload leaked %q: %+v", banned, sent)
			}
		}
		if n := e.n(t, `SELECT count(*) FROM course_invite_deliveries WHERE invite_id=$1 AND channel='push' AND status='sent' AND attempts=1`, inv.ID); n != 1 {
			t.Fatal("push must be marked sent after one attempt")
		}
		if list, _, _ := e.s.ReceivedInvites(ctx, ada); len(list) != 1 {
			t.Fatal("in-app inbox must hold the invite")
		}
	})

	t.Run("denied_push_still_leaves_the_in_app_invite", func(t *testing.T) {
		e := inviteWorld(t, nil)
		e.push.Fail(sharing.ErrPushDenied)
		ada := e.person(t, "Den")
		e.befriend(t, owner, ada)
		inv, err := e.s.SendInvite(ctx, owner, e.src, sharing.InviteInput{Username: "den"})
		if err != nil {
			t.Fatal(err)
		}
		if err = e.s.DrainInviteDeliveries(ctx); err != nil {
			t.Fatal(err)
		}
		if n := e.n(t, `SELECT count(*) FROM course_invite_deliveries WHERE invite_id=$1 AND channel='push' AND status='suppressed'`, inv.ID); n != 1 {
			t.Fatal("a denied push is suppressed, not retried")
		}
		if list, _, _ := e.s.ReceivedInvites(ctx, ada); len(list) != 1 {
			t.Fatal("denied push must leave the in-app inbox intact")
		}
	})

	t.Run("a_failing_adapter_retries_a_bounded_number_of_times", func(t *testing.T) {
		e := inviteWorld(t, func(c *sharing.Config) { c.PushMaxAttempts = 3 })
		e.push.Fail(errors.New("upstream 500"))
		ada := e.person(t, "Fai")
		e.befriend(t, owner, ada)
		inv, err := e.s.SendInvite(ctx, owner, e.src, sharing.InviteInput{Username: "fai"})
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 6; i++ {
			if err = e.s.DrainInviteDeliveries(ctx); err != nil {
				t.Fatal(err)
			}
		}
		if n := e.n(t, `SELECT count(*) FROM course_invite_deliveries WHERE invite_id=$1 AND channel='push' AND status='failed' AND attempts=3`, inv.ID); n != 1 {
			t.Fatal("attempts must stop at the configured bound")
		}
		if list, _, _ := e.s.ReceivedInvites(ctx, ada); len(list) != 1 {
			t.Fatal("a failing push must not touch the inbox")
		}
	})

	t.Run("disabled_adapter_is_in_app_only", func(t *testing.T) {
		e := inviteWorld(t, nil)
		e.s.WithPush(sharing.DisabledPush{})
		ada := e.person(t, "Dis")
		e.befriend(t, owner, ada)
		inv, err := e.s.SendInvite(ctx, owner, e.src, sharing.InviteInput{Username: "dis"})
		if err != nil {
			t.Fatal(err)
		}
		if err = e.s.DrainInviteDeliveries(ctx); err != nil {
			t.Fatal(err)
		}
		if n := e.n(t, `SELECT count(*) FROM course_invite_deliveries WHERE invite_id=$1 AND channel='push' AND status='suppressed'`, inv.ID); n != 1 {
			t.Fatal("a disabled adapter suppresses push")
		}
		if list, _, _ := e.s.ReceivedInvites(ctx, ada); len(list) != 1 {
			t.Fatal("in-app must still deliver")
		}
	})

	t.Run("a_revoked_or_declined_invite_is_not_pushed_later", func(t *testing.T) {
		e := inviteWorld(t, nil)
		ada := e.person(t, "Gone")
		e.befriend(t, owner, ada)
		inv, _ := e.s.SendInvite(ctx, owner, e.src, sharing.InviteInput{Username: "gone"})
		if _, err := e.s.RevokeInvite(ctx, owner, inv.ID); err != nil {
			t.Fatal(err)
		}
		if err := e.s.DrainInviteDeliveries(ctx); err != nil {
			t.Fatal(err)
		}
		if len(e.push.Sent()) != 0 {
			t.Fatal("a revoked invite must not be pushed")
		}
	})

	t.Run("recipient_setting_changed_to_nobody_before_push", func(t *testing.T) {
		e := inviteWorld(t, nil)
		ada := e.person(t, "Late")
		e.befriend(t, owner, ada)
		if _, err := e.s.SendInvite(ctx, owner, e.src, sharing.InviteInput{Username: "late"}); err != nil {
			t.Fatal(err)
		}
		e.setting(t, ada, "nobody")
		if err := e.s.DrainInviteDeliveries(ctx); err != nil {
			t.Fatal(err)
		}
		if len(e.push.Sent()) != 0 {
			t.Fatal("push must honor the setting at send time")
		}
	})
}

func TestInviteAnalyticsOptOutAndRetention(t *testing.T) {
	e := inviteWorld(t, nil)
	ctx := t.Context()

	t.Run("opted_out_sender_emits_nothing", func(t *testing.T) {
		consent(t, e.f, owner, true)
		defer consent(t, e.f, owner, false)
		id := e.person(t, "Opt")
		e.befriend(t, owner, id)
		before := len(e.ev.named("course_invite_sent"))
		if _, err := e.s.SendInvite(ctx, owner, e.src, sharing.InviteInput{Username: "opt"}); err != nil {
			t.Fatal(err)
		}
		if len(e.ev.named("course_invite_sent")) != before {
			t.Fatal("opt-out must stop invite events (central consent gate)")
		}
	})

	t.Run("final_invites_purge_after_ninety_days_and_pending_stay", func(t *testing.T) {
		mk := func(name string) string {
			id := e.person(t, name)
			e.befriend(t, owner, id)
			inv, err := e.s.SendInvite(ctx, owner, e.src, sharing.InviteInput{Username: strings.ToLower(name)})
			if err != nil {
				t.Fatal(err)
			}
			return inv.ID
		}
		old, recent, pending := mk("Old1"), mk("New1"), mk("Pend")
		for id, age := range map[string]string{old: "91 days", recent: "89 days"} {
			if _, err := e.f.DB.Pool.Exec(ctx, `UPDATE course_invites SET status='declined', resolved_at=now()-$2::interval WHERE id=$1`, id, age); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := e.f.DB.Pool.Exec(ctx, `UPDATE course_invites SET created_at=now()-interval '120 days' WHERE id=$1`, pending); err != nil {
			t.Fatal(err)
		}
		if err := e.s.SweepInvites(ctx); err != nil {
			t.Fatal(err)
		}
		if n := e.n(t, `SELECT count(*) FROM course_invites WHERE id=$1`, old); n != 0 {
			t.Fatal("an invite final for 91 days must be purged")
		}
		if n := e.n(t, `SELECT count(*) FROM course_invite_deliveries WHERE invite_id=$1`, old); n != 0 {
			t.Fatal("its delivery rows go with it")
		}
		if n := e.n(t, `SELECT count(*) FROM course_invites WHERE id=ANY($1)`, []string{recent, pending}); n != 2 {
			t.Fatal("recent final and pending invites must stay")
		}
	})

	t.Run("erasing_a_learner_erases_their_invites", func(t *testing.T) {
		id := e.person(t, "Erase")
		e.befriend(t, owner, id)
		if _, err := e.s.SendInvite(ctx, owner, e.src, sharing.InviteInput{Username: "erase"}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.f.DB.Pool.Exec(ctx, `DELETE FROM learners WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
		if n := e.n(t, `SELECT count(*) FROM course_invites WHERE recipient_id=$1`, id); n != 0 {
			t.Fatal("invites must cascade with the learner")
		}
	})
}
