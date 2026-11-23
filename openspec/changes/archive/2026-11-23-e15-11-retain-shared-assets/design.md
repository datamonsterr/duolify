## Decisions
Unpublish is one transaction on the owner's course: revoke active link, delist listing, `visibility='private'`, and revoke pending invites; it is idempotent and invalidates the Marketplace cache after commit. Enrollment and invites already refuse `private`.

Attribution: `course_copies.source_owner_id uuid REFERENCES learners ON DELETE SET NULL`, backfilled from the source course. Null means "deleted user". Copies already survive owner deletion because provenance columns are `SET NULL`.

Retention: `shared_asset_retention(prefix PK, ref_count, released_at, purge_after, purged_at)`. A prefix is `lessons/published/<draft>/`. `pinPrefix` upserts and clears release marks inside the enrolling or updating transaction, so the row lock is held until commit. The sweep recounts references per row with `FOR UPDATE`: zero references set `released_at`/`purge_after = now + SHARING_ASSET_PURGE_DAYS`; a nonzero count clears them; a due row is recounted under the lock before objects are deleted and `purged_at` is set. Counts include pins on live copies and lesson manifests of live copies. Objects go through the versioned bucket (delete markers), so storage versioning still covers recovery.

Moderation ledger: severe takedown writes `moderation-blocks/v1/<sha256 action id>.json` (`course_ref` = sha256 of the course id) before commit; an overturn writes the `.lifted.json` marker. The restore drill lists the prefix and re-blocks copies whose `sha256(source_course_id)` matches a block without a lift marker.

Release readiness: pure report over config and the schema; approvals (`SHARING_RELEASE_LEGAL_APPROVED`, `SHARING_RELEASE_CDN_POLICY_APPROVED`) default false.

## Data and backup
New table and column join nightly dumps. Retention rows hold object prefixes only (no personal text); the anonymizer classifies them as not personal. Final invites are purged after 90 days (E15-06, unchanged). Account deletion nulls `source_owner_id`. Restore drill adds pin resolution and moderation block checks. No AI behaviour changes; evals and fallbacks are unchanged.
