## Why
Learners who copied a shared course must keep it when the owner unpublishes, deletes the course or deletes their account, and a severe moderation block must survive a restore. The Marketplace release flag stays off until these checks are visible (ADR 0012).

## What Changes
- Owner `Unpublish`: one call sets visibility `private`, revokes the share link, delists the Marketplace listing and stops new enrollments and invites. Existing copies keep their pinned version.
- Copies record the creator (`course_copies.source_owner_id`, null when the owner account is deleted). The course view shows "Created by deleted user" and a copy that keeps working.
- Reference-counted retention of published objects (`shared_asset_retention`): a prefix with no live copy reference is released, then purged after `SHARING_ASSET_PURGE_DAYS` (default 30) by a retention job. Pinning locks the row so a purge cannot race an enrollment or update.
- Severe takedowns write an immutable block record to the backup ledger. Restore drills replay it, so a restore of an older dump cannot unblock copies.
- Restore drill checks: every pin resolves (retention row present, objects exist) and severe blocks hold.
- Release-readiness report (moderator only) listing caps, erasure, noindex/CDN policy and approvals. It never changes `MARKETPLACE_ENABLED`.
- Client: Unpublish dialog in the share panel, deletion dialog note, copy origin notice. Desktop and mobile.
- Analytics: `course_visibility_changed` (already catalogued) and `restore_drill_completed`; no new events.
- Out of scope: flipping the release flag, E15-12 policy and E15-13 legal sign-off, paid courses, AI changes.
