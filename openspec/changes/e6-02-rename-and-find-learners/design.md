## Decisions
- Rename is `PATCH /v1/social/username`; `PUT` stays the first claim (409 `username_already_set`).
- Cooldown counts from `changed_at` only (the first change after a claim is free). Reclaiming a name the learner themselves released inside its hold bypasses the cooldown (otherwise a hold equal to the cooldown could never be used) but restarts it.
- Rename and claim run in one transaction under a `learners` row lock; the E6-01 triggers write the hold. A case-only change keeps the canonical name, writes no hold and still uses the cooldown.
- Cooldown refusal returns `username_cooldown` (409) with `next_change_at`.
- Rate limits are fixed windows in Postgres (`social_rate_limits(learner_id,bucket)`), upserted atomically; the existing auth limiter is keyed to hashed emails, not learners. Fixed windows can allow a short burst across a boundary; accepted.
- Search normalizes NFKC+lowercase, strips a leading `@`, prefix match uses `canonical LIKE … ESCAPE` on a `text_pattern_ops` index with `_` escaped. Order: exact first, then alphabetical. Self is excluded. A malformed query returns an empty list, never an error revealing why.
- Visibility: prefix results need `discoverable`; exact results need `discoverable OR request_policy='everyone'`. `GET /profiles/{username}` uses the exact rule. Hidden and missing learners are indistinguishable.
- Block filter: `blockedPairClause` is empty until E6-04 adds one `NOT EXISTS` over the blocks table.
- Settings changes emit `privacy_setting_changed` only when a value actually changes; values are enums (`on|off|everyone|friends_of_friends|nobody`).
