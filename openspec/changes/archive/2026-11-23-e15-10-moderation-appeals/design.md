## Decisions
Role: `moderator_grants(account_id)` mirrors `pricing_admin_grants`; every moderation call checks it server-side and answers a non-moderator with a neutral 404. The actor is always the session. Moderation is not behind `MARKETPLACE_ENABLED` (link-shared courses need takedown too); the marketplace itself stays behind the flag (default off).

Queue: `moderation_queue_items` target_type widens to course, review, user, publication (publication target = draft id). Severity and the 48 h target are configuration (`SHARING_MODERATION_TARGET_HOURS`). A queue item is resolved by exactly one action.

Action matrix: course = all five; publication = dismiss (approve: verdict becomes pass), delist and takedown (reject: verdict block), ban_publishing; review = dismiss, warn, delist, takedown (hide the review); user = dismiss, warn, ban_publishing. `delist` hides from the marketplace and sets the course moderation state `delisted`; `takedown` also revokes the share link, sets `taken_down`, and, only with a severe reason code, sets `course_copies.blocked_at` on every copy. Owners cannot relist or republish a moderated course until an appeal is overturned. `ban_publishing` also delists the owner's listings and refuses new publishing.

Audit: `moderation_actions` rows are append-only (trigger). Only account deletion may null the moderator and author references. Reasons are free text kept three years and never emitted.

Appeals: `moderation_appeals.action_id` is unique, so an action can be appealed once. Only the targeted author may appeal. The deciding moderator must differ from the acting moderator (service check under row lock, repeated by trigger). Overturn reverses the action's effects, including the share link: when overturning a takedown, the API re-activates the specific link(s) that the takedown revoked (same token hash, so previously shared URLs work again). `publication_links` gains `revoked_by_action_id` (nullable FK to `moderation_actions`) set only when a takedown revokes the link; owner-revoked or expired links keep NULL and are never re-activated. Re-activation, the appeal decision and a `restore_link` moderation audit row commit in a single transaction.

Cache: every action that changes catalogue visibility invalidates the Redis catalogue after commit.

## Data and backup
New tables join nightly dumps, export (own notices and appeals) and the restore anonymizer: action reason text and appeal text are redacted on restore. Retention: actions, appeals and queue items three years. No AI behaviour is added; evals and fallbacks are unchanged.
