## Why
Marketplace release is gated on moderation (ADR 0012). Moderators need a queue and audited enforcement; authors need the reason and one appeal; held publications need a human decision before they become visible.

## What Changes
- Add a `moderator` role (`moderator_grants`, same pattern as `pricing_admin_grants`; grants are made by the project owner in the database, never by API).
- Queue read model widened to course, review, user and publication targets, ordered by severity then age, with a 48 h first-response target and an overdue flag. `needs_review` and `pending_screening` drafts enter the queue and stay invisible until a moderator resolves them.
- Actions `dismiss`, `warn`, `delist`, `takedown`, `ban_publishing`, each with a closed reason code and a reason text, written to an append-only `moderation_actions` audit log.
- Takedown delists, revokes the share link and blocks enrollment; a severe reason (`unsafe`, `legal`) also blocks existing copies with a notice. Other actions keep pinned copy access.
- The author sees the reason and may appeal each action once; a different moderator decides. An overturned action is restored.
- Client: moderator queue with action dialog, author notice and appeal, blocked-copy notice. Desktop and mobile.
- Catalogue `moderation_action`, `moderation_appeal` and `course_visibility_changed` for moderation before the emitters.
- Data: audit, actions and appeals in nightly dumps, three-year retention, pseudonymization on account deletion, export, restore checks.
- Out of scope: paid courses, grant-management UI, automated moderation, AI changes.
