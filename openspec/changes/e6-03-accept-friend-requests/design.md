## Decisions
- One `friend_requests` row per ordered pair; re-send after cooldown reuses it. Accepted rows stay (idempotent accept); removal deletes the pair's rows, which also clears any cooldown.
- Every pair mutation locks both `learners` rows in id order (`FOR UPDATE`): serializes mutual requests, friend-cap checks and block (E6-04). Backstops: partial unique index on the canonical pending pair and `friendships` PK + `CHECK (learner_low < learner_high)`.
- A mutual pending request is accepted instead of duplicated.
- Link token: 32 random bytes, base64url; only the SHA-256 hash is stored; one active link per learner (creating rotates). The token is shown once. A link bypasses `everyone`/`friends_of_friends`, never `nobody`. Resolve and send re-validate server-side; expired, revoked, unknown, own and refused links all return `invite_unavailable`.
- Opening a link: route `/friends/invite/{token}`; the route guard returns the user there after sign-in; a request is only created by an explicit confirm POST.
- The sender never learns of a decline: declined requests vanish from "Sent", re-send within the cooldown returns the neutral error.
- Daily send cap uses a 24 h fixed window in `social_rate_limits` inside the send transaction (rolls back with it). Idempotent re-sends do not count.
- Friend/request views expose username, display name, avatar and the privacy-permitted streak only.
