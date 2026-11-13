# Accept friend requests and remove connections (E6-03)

## Why
Learners need a consented way to become friends — by username or by an opaque invite link — and to end a friendship (#92). The friend list feeds friend streaks (E6-06) and course invites.

## What Changes
- Postgres `friend_requests` (one row per ordered sender→recipient pair, `pending|accepted|declined`), `friendships` (canonical ordered pair, `learner_low < learner_high`) and `friend_links` (SHA-256 token hash, expiry, revocation).
- API under `/v1/social`: `POST /friend-requests` (`{username}` or `{token}`), `GET /friend-requests`, `POST /friend-requests/{id}/accept|decline`, `DELETE /friend-requests/{id}` (cancel), `GET /friends`, `DELETE /friends/{id}`, `GET|POST|DELETE /friend-link`, `POST /friend-link/resolve`. All mutations are idempotent; the actor always comes from the session.
- Limits live in `username_config`: 20 pending outgoing, 50 sends per day, 500 friends, 7-day re-send cooldown after a decline, 30-day link validity, link-resolve rate.
- The recipient's `request_policy` (`everyone|friends_of_friends|nobody`) is enforced server-side. Unknown learner, `nobody`, no common friend, active decline cooldown and self all return one neutral `request_unavailable`.
- Pair uniqueness in Postgres; concurrent mutual requests resolve to one friendship (pair row locks plus partial unique index).
- `removeFriendshipTx` is the single transactional removal path; E6-06 hooks the friend-streak end there, E6-04 block calls it with `cancelRequestsTx` in its own transaction.
- Client: Friends list, Requests (incoming/sent), Add-a-friend send, invite-link create/copy/revoke and `/friends/invite/{token}` landing that re-checks the session and needs an explicit confirm before a request is created. Desktop list-detail becomes two mobile screens.
- Analytics: `friend_request_sent` (`method`), `friend_request_accepted`, `friend_request_declined`, `friend_removed`.
- Data: three new tables in nightly dumps, export/deletion policy, fixture, inventory.

## Out of scope
Blocking (E6-04), nudges and friend streaks (E6-05/06), course invites, mutual-friend counts.
