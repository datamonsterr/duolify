## Decisions
- `learner_usernames(learner_id PK FK cascade, username, canonical UNIQUE, claimed_at, changed_at)`; canonical = `lower(NFKC(username))` computed in Go and re-checked by a CHECK constraint on shape.
- `username_holds(canonical PK, previous_owner_ref sha256(learner_id), held_until)`. A trigger on delete/update of `learner_usernames` inserts the hold, so account deletion (live and tombstone replay) and the future rename share one mechanism. A claim is refused when an unexpired hold belongs to someone else.
- `learner_social_settings(learner_id PK, discoverable, request_policy, show_streak_to_friends)` is created now with safe defaults so E6-02..E6-04 add no learner-linked migration; the projection honours `show_streak_to_friends`.
- Lists live in `username_config` (single row, version, reserved text[], profanity text[]), matched on a confusable-folded canonical form (digits/`_`/`.` removed or mapped) so case-confusable names are refused.
- Rejection codes: `username_taken`, `username_reserved`, `username_invalid`, `username_blocked_word`. Taken, held and lost races all return `username_taken`.
- Streak status is optional: no streak store exists yet (E5-02), so the projection omits it until a source is wired.
