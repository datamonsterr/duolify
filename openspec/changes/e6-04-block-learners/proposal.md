## Why
Learners need to shut out someone who bothers them (#93). Blocking must be effective on the next request, and course invites (E15) need one shared authorization rule so a blocked pair can never reach each other through sharing.

## What Changes
- Postgres `learner_blocks` (`blocker_id`, `blocked_id`, `created_at`; one row per ordered pair, `blocker <> blocked`). Limit `block_max` (500) lives in `username_config`.
- API under `/v1/social`: `GET /blocks` (own blocked list), `POST /blocks` (`{username}`), `DELETE /blocks/{username}`. Actor always comes from the session. Block and unblock are idempotent.
- Blocking is one transaction: `lockLearners`, insert block, `removeFriendshipTx` (friendship, pair requests, and from E6-06 the friend streak), then every registered on-block callback. Any failure rolls the whole block back.
- A blocked pair is hidden from each other in search, profile and friend-link resolution. A blocked send or link accept returns the same neutral refusal as an unknown username and creates and notifies nothing.
- `learner_social_settings.course_invites_from` (`friends|everyone|nobody`, default `friends`) joins the other privacy settings; request policy and `show_streak_to_friends` are enforced wherever identity or streak status is projected, including the profile card.
- Shared E15 contract in the `social` package: `BlockedEitherWay`, `CanInteract`, `CanInvite`, `NotBlockedSQL` and `Service.OnBlock` (see design).
- Client: block confirm (from the friend row and from a profile card, with "Only remove friend"), a Blocked list with unblock, and the `course_invites_from` setting, desktop and mobile.
- Analytics: `learner_blocked` (`was_friend`). `privacy_setting_changed` gains the `course_invites_from` setting.
- Data: `learner_blocks` in nightly dumps, export and deletion policy, backup fixture, inventory.

## Out of scope
Course invites themselves and their tables (E15), friend streak tables (E6-06), reporting abuse, an unblock event (not in the catalogue), muting.
