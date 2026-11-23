## Why
A Learner should be able to nudge a friend who has not finished today, once per day, and the social state a friendship leaves behind (nudges, inbox, pair rows) must go away safely with blocks, removal and account deletion (#95).

## What Changes
- Postgres `friend_nudges`: one row per nudge, which is also the recipient's in-app inbox entry. Unique operation key `(sender_id, operation_key)` makes a retry a replay; unique `(sender_id, recipient_id, sender_local_date)` limits a pair to one nudge per sender local day.
- `POST /v1/social/friends/{friendshipId}/nudge`, `GET /v1/social/nudges`, `POST /v1/social/nudges/read`. Friend list entries gain `nudge` (`sent`, `next_at`).
- Delivery is in-app only (ADR 0012); no push provider is called. Block, removal and the recipient's `show_streak_to_friends` setting suppress delivery (no new setting).
- Config columns on `username_config`: `nudge_retention_days` (14) and `nudge_sends_per_day` (20).
- `removeFriendshipTx` deletes the pair's nudges. Learner deletion cascades them; tombstone replay after a restore does the same and keeps username holds.
- Analytics `friend_nudge_sent` (UUIDs/enums only).
- Client: nudge button and state on friend row and detail, inbox tab, desktop and mobile, per S14.
- Data: dumps, privacy policy, export, backup fixture, inventory.

## Out of scope
Push notifications (E15 supplies optional credentials), new privacy settings, new AI behavior.
