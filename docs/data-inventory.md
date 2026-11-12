# Data inventory

E9-04: `quick_notes` adds optimistic revision concurrency column `version` (integer default 1) and `reviewed_at` (timestamptz). Included in nightly encrypted database dumps; restore fixture seeds a versioned note and verifies complete row preservation. Account export includes owned quick notes with version and reviewed timestamp; account deletion cascade-erases them within 30 days. Restored deletion tombstones remove learner notes before serving. Analytics catalogue defines `quick_note_reviewed` and `quick_note_deleted` (carrying `note_id` only) and `ui_control_activated` (`control_id=export_notes`); note text, search queries, and filter terms are strictly excluded from PostHog.

E9-03: `quick_notes` adds `raw_version` (integer default 1) and partial index `quick_notes_pending_enrichment` on `(learner_id, created_at ASC) WHERE status = 'enrichment_pending'`. Tracks note edits to reject stale background enrichment jobs. Enrichment recovery also stores a fencing UUID (`enrichment_claim_id`) and lease deadline (`enrichment_lease_until`), with a learner-scoped partial index for active leases. Preserved in nightly encrypted database dumps; the restore fixture includes a claimed note and compares complete rows. These operational fields are erased with account deletion within 30 days; they contain no learner text and do not appear in analytics.

E9-02: `quick_note_enrichments` stores AI-generated summaries, tags, and course lesson links for quick notes with version, model, and provider provenance metadata. Foreign or hallucinated lesson links are filtered out before persistence. Included in nightly encrypted database dumps; restore fixture seeds an enrichment and verifies complete row preservation. Account export includes owned quick note enrichments; account deletion cascade-erases them within 30 days. Restored deletion tombstones remove learner note enrichments before serving. Analytics catalogue defines `quick_note_enriched` and `quick_note_enrichment_failed` (carrying `note_id`, `reason`, `error_code`); raw note text and AI summaries are strictly excluded from PostHog.

E9-05: `bookmarks` stores unique learner-target pointers (lesson or exact stable slide index) with stable action keys, course and lesson titles. Included in nightly encrypted database dumps; restore fixture seeds a bookmark and verifies complete row preservation. Account export includes owned bookmarks; account deletion cascade-erases them within 30 days. Restored deletion tombstones remove bookmarks before serving. Removed or deleted targets are flagged unavailable and remain removable. Device offline queue is ephemeral local state and disposable after server acknowledgement. Analytics catalogue defines `bookmark_added`, `bookmark_removed`, and `bookmark_opened` (carrying `target_type`, `course_id`, `lesson_id`, `slide_id`); private text, notes, and slide HTML are strictly excluded from PostHog. No AI behavior or secrets in this ticket.

E9-01: `quick_notes` stores raw learner quick notes with stable action key, authorized course/lesson/slide references, raw text (up to 2,000 characters), status `enrichment_pending` and review state. Included in nightly encrypted database dumps; restore fixture seeds a note and verifies complete row preservation. Account export includes owned quick notes; account deletion cascade-erases them within 30 days. Restored deletion tombstones remove learner notes before serving. Device-saved drafts and offline queues are ephemeral local state and are not backed up. Analytics catalogue defines `quick_note_created` (source=slide|lesson|chat and character length only) and `ui_sheet_opened` (sheet_id=quick_note); note text is strictly excluded from PostHog. No AI behavior or secrets in this ticket.

E3-02 adds `lesson_playback_positions`: one learner/lesson cursor containing the
immutable manifest version, opaque slide ID, bounded time in milliseconds and
UTC `updated_at`. This is navigation state and cannot award XP or completion.
It joins encrypted nightly Postgres dumps retained for 14 days. Backup fixtures
compare the entire restored row and verify deletion tombstones erase it. Learner
exports include the row; account/lesson deletion cascades immediate erasure.
Device `sessionStorage` keys `player:position:<learner UUID>:<lesson UUID>` are
disposable recovery cursors, cleared on logout. Save failures visibly offer Retry;
durable offline reconciliation and lesson caching remain E3-04/E3-03. Analytics
records only registered IDs, numeric watch time/index, offline and fixed codes.

E3-04 adds `lesson_playback_sync_events`: non-authoritative playback progress events
with stable action key, learner UUID, lesson UUID, manifest version, slide ID, bounded
time in milliseconds, UTC `occurred_at`, and `created_at`. It joins encrypted nightly
Postgres dumps retained for 14 days. Backup fixtures compare the entire restored row
and verify deletion tombstones erase it. Learner exports include the row; account and
lesson deletion cascade immediate erasure. Device `localStorage` keys
`player:sync-queue:<learner UUID>:<lesson UUID>` retain pending offline progress events
until server acknowledgment and clear on logout. Reconciled events update canonical
cursors monotonically without granting completion or XP.

E3-05 adds `explanation` per slide in object lesson manifests stored in application object
storage. Explanations provide accessible static recovery and captions under reduced
motion or asset failure without learner data, private questions, or assessment leaks.
It contains bounded course content (<= 20,000 characters per slide), backed up with
object versioning alongside Motion HTML slides and manifests. No new Postgres tables
or personal learner data are collected; PostHog analytics records only registered
error codes and recovery actions (`lesson_player_error`, `ui_recovery_selected`)
without slide or learner text.

E3-06 adds device-disposable course library download index:
On the client, `localStorage` key `duolify:download-index:<learner ID>` retains the
device-disposable index of downloaded course IDs, completion timestamps, and lesson IDs
to enable offline course library playback and storage accounting. This index is disposable,
cleared on explicit logout or session expiry via `clearDownloadIndex()`, and never backed up to
servers. Server-side entitlement is derived dynamically from existing subscription terms
(`credit_subscription_terms`, `credit_subscriptions`, and `credit_subscription_revocations`),
requiring no new Postgres tables or personal learner data. Unentitled learners or expired
subscriptions cannot trigger new bulk downloads, but existing downloaded and opened lesson
artifacts remain playable offline. Analytics allowlist registers `ui_control_activated` with
`control_id: "download_library"` and `ui_recovery_selected` with error codes
`"subscription_required"` and `"download_failed"`, strictly excluding learner or slide text.

E2-01: personalized preview output (sample outline and Motion HTML) stays in component
memory and is discarded on Back/leave; no Course, enrollment, object or new learner
table is created. The existing immutable `pricing_quotes.snapshot.input.brief_revision`
contains only an opaque confirmed revision ID, never brief/profile text. Existing
quote/job/usage/ledger export and pseudonymization policies in `accounts/privacy.go`
apply; financial records retain seven years and encrypted snapshots rotate after
35 days. Confirmed briefs retain E2-06 expiry/erasure. Restore fixtures preserve
revision-bound preview quotes alongside reservations and replay account erasures.


E7-04: `credit_checkout_orders` stores immutable illustrative pending order IDs,
request dedupe keys, pseudonymous owner references, optional quote IDs and original
catalogue SKU, amount, currency, Credits, billing period and fixed internal return
destination. It contains no draft text, raw notes, card data or provider secrets.
Included in encrypted nightly financial dumps (35-day snapshot retention, PITR where
available), with at least seven-year live retention. Backup fixtures seed non-empty
orders and compare complete restored rows. Account export includes owned orders;
deletion and restore tombstones null learner IDs while retaining pseudonymous
financial provenance. These illustrative orders never fulfill or grant Credits.
Client recovery stores only account-scoped order/quote IDs in session storage;
logout/deletion clears them. Mounted work remains local to its owning feature,
and no local state or callback can verify payment. No new AI data or eval behavior.

E10-04: pinned Golden set dataset revisions on Hugging Face are mirrored to versioned
S3 object storage (RustFS locally) with SHA-256 checksum verification. Live eval runs
download only immutable 40-character hex commit hashes, reject corrupt or unrelated
fallbacks, and fall back to the verified mirror during Hugging Face outages. Eval
reports are retained for 90 days as CI artifacts. No learner data is created, mirrored,
or held; account deletion/export policies are unaffected.

E10-06: Gate AI promotion on reproducible live samples. Evaluation reports record case,
revision, provider, usage, budget, and thresholds with 90-day retention as CI artifacts.
Live sample promotion requires explicit credentials and passing relevant cases; absence
of secrets reports explicit missing-secret status and keeps feature activation disabled
without fabricating a pass. Provider budget accounting halts execution when the configured
eval budget is reached. Critical safety, ledger, and cost violations reject the run immediately.
No learner data is created, mirrored, or sent; account deletion and export policies are unaffected.

Owner specs are authoritative for collection and deletion. Every feature adding
data must update this table and its restore checks. Bootstrap currently creates
`learners`, email-code auth/profile/session tables, `credit_entries`, immutable `pricing_versions`, Goose/River metadata and River's queue tables;
the other product records below are planned. E11-01 implements the local nightly
worker dump/upload and synthetic recovery proof; cloud keys, lifecycle policies
and production drills still require provisioning. See [backup tooling](../infra/backup/README.md).

| Store | Data held | Backup method | Retention / deletion | Owner spec |
| --- | --- | --- | --- | --- |
| Device download library index (`localStorage`, `duolify:download-index:<learner ID>`) | Course IDs, lesson IDs, and completion timestamps for offline course playback | Disposable; never backed up; server subscription terms and object storage are authoritative | Cleared on logout, account deletion, or explicit storage clear; bounded by device storage quota (max 50 lessons, 100 MiB per account) | [download-entitled-offline-course-library](../openspec/changes/download-entitled-offline-course-library/design.md) |
| Postgres `lesson_playback_sync_events` | Non-authoritative playback progress events with action keys unique per (learner_id, action_key), manifest version, slide ID, bounded time and UTC occurred_at clamped to 2000-01-01 through server time + 2 minutes | Nightly encrypted general dump, 14-day retention; populated backup fixture compares full rows | Read/export for authenticated owner only; immediate account/lesson deletion cascades; restore deletion tombstones erase | [synchronize-offline-playback-progress](../openspec/changes/synchronize-offline-playback-progress/design.md) |
| Device pending sync queue (`localStorage`, learner UUID + lesson ID) | Non-authoritative playback progress events pending server acknowledgment | Disposable; never backed up; server reconciliation is authoritative | Retained until server acknowledgment or permanent event rejection; transient failures retry in bounded batches; cleared on logout or account change | [synchronize-offline-playback-progress](../openspec/changes/synchronize-offline-playback-progress/design.md) |
| Postgres `course_brief_revisions` | Immutable learner-owned confirmed Course briefs, content hash and expiry; no transcript | Nightly encrypted general dump, 14-day retention; populated backup fixture compares full rows | Read/export for authenticated owner only; expires after 30 days, nightly pre-backup purge (within 24 hours); immediate account cascade and restored deletion tombstones | [E2-06 change](../openspec/changes/clarify-course-brief/design.md) |
| Postgres `outline_proposals` | Learner-owned immutable draft/base revision, request, typed diff, result, usage references and Apply/Discard/Undo decision snapshot; unique quote links reconnect without generation | Nightly encrypted general dump, 14 days; populated full-row restore comparison and restored account-erasure checks | Owner-only read/export; draft/account/brief deletion cascades; brief 30-day expiry with nightly pre-backup purge (within 24 hours); restore tombstones replayed before serving. Quotes/reservations/usage retain separate financial policy without learner text | [E2-08 contract](../openspec/changes/outline-chat-change/design.md) |
| Postgres `outline_drafts` | Learner-owned edited sample outline with stable IDs, revision, last 50 pre-edit snapshots/focus IDs and exact confirmed snapshot/hash; includes recovery copies | Nightly encrypted general dump, 14 days; populated full-row backup comparison and restored deletion checks | Authenticated owner read/export; explicit draft deletion, immediate account/brief cascades; brief 30-day expiry and pre-backup purge bound lifetime; reapply restore tombstones before serving | [E2-07 contract](../openspec/changes/edit-sample-outline/design.md) |
| Device outline working copy (`sessionStorage`, learner UUID + brief revision) | Unsaved snapshot, undo stack and original expected revision for failed-save/navigation recovery | Disposable; never backed up; synchronized Postgres draft is authoritative | Same signed-in learner and tab only; cleared after successful save or tab close; stale recovery must pass server CAS | E2-07 |
| Device session `course-brief:<learner UUID>` | Unconfirmed brief, composer and guided state | Disposable; no server backup or text in URLs | Session lifetime, survives navigation/auth/shop return for same UUID; removed by account erasure; isolated between learners | [E2-06 change](../openspec/changes/clarify-course-brief/design.md) |
| Postgres | `learners`, `auth_identities` (normalized email), `auth_provider_identities` (Google and Apple provider subjects plus verified email, including Apple private relay; unique subject binding), `auth_oauth_attempts` (HMAC state/nonce hashes, PKCE challenge, exact redirect, 10-minute expiry), `learner_profiles` (device locale/time zone; optional current goal, topic, background, level, daily minutes/XP, reminder enabled/local time; setup completion; appearance, motion, narration, narration speed, captions, text size, UI sounds, haptics and analytics consent; age confirmation timestamp), `auth_codes` (purpose-keyed HKDF HMAC hashes, AES-GCM sealed OTP ciphertext, resend timestamp, expiry and attempts), `auth_sessions`, `auth_refresh_tokens` (SHA256 hashes; AES-GCM sealed successor ciphertext usable only in the 5-second race window), `auth_rate_limits` (HKDF-keyed HMAC email/IP keys; nullable email_hash for per-email delivery cross-reference) | Full encrypted nightly dump to separate backup bucket; Railway volume snapshots; custom-format dump/restore fixture compares complete provider-subject bindings, OAuth attempts, learner context and auth state, including a synthetic Apple subject | OAuth attempts expire after 10 minutes and are single use; provider bindings cascade with Learner deletion. Attempts contain no provider access or refresh tokens. Apple name is stored as the Learner display name only when Apple supplies it at first authorization; later sign-ins resolve by stable subject when name/email are omitted. Codes expire after 10 minutes/five attempts; rate windows expire after one hour; sessions and refresh secrets expire after 30 days, access JWTs after 15 minutes. Expired codes/rates/sessions are pruned on code requests; OAuth attempts are pruned when a new attempt starts; expiry is enforced on every use. Identity/profile/session/token rows cascade with Learner deletion; identity deletion removes pending codes by email and clears related rate-limit keys. Rate keys contain no reversible identity and expire within one hour. Successor ciphertext is a sealed copy of the issued successor token pair, usable only during the 5-second grace window to tolerate concurrent refresh. Code-request maintenance clears it after grace; until maintenance it remains encrypted in the session rows and encrypted backup snapshots. It expires with the parent token and is never the sole copy of a credential. Personal data is removed within 30 days; encrypted backup copies roll off after 14 days. No profile history or draft answers are stored. Authenticated profile export returns current context and settings only. Restore checks compare every context field, profile setting/consent, provider binding and OAuth attempt, and verify cascade deletion, hashes, defaults, refresh replay, revocation and authentication expiry. Profile API/unit checks verify explicit confirmation, partial clear and preference preservation. Session families use the existing auth_sessions table; GET /v1/auth/sessions returns only owned active IDs, timestamps and current-device flags. Individual/all-device DELETE updates revoked_at; protected requests read Postgres directly with no authorization cache. No email, IP or user-agent device labels are collected. | [apple-oauth-sign-in](../openspec/changes/apple-oauth-sign-in/specs/accounts/spec.md), [google-oauth-sign-in](../openspec/changes/google-oauth-sign-in/specs/accounts/spec.md), [confirm-learner-context](../openspec/changes/confirm-learner-context/specs/learner-profile/spec.md), [email-code-sign-in](../openspec/changes/email-code-sign-in/specs/accounts/spec.md), [persist-settings-analytics-consent](../openspec/changes/persist-settings-analytics-consent/specs/learner-profile/spec.md), [learner-profile](../openspec/specs/learner-profile/spec.md), [data-backup](../openspec/specs/data-backup/spec.md) |
| Postgres | Append-only `credit_entries` (with nullable `learner_id`, SHA-256 `owner_reference`, `pricing_version`, and auditable `provenance` metadata; kinds `daily_grant`, `purchase`, `subscription_grant`, `spend`, `reserve`, `release`, `refund`, `expire`, historical `reward`); `credit_timezones` (with nullable `learner_id`, SHA-256 `owner_reference`, `time_zone`, and 24-hour delayed `effective_at`); derived available and bucket totals | Same dump; PITR where available; reconcile balances with sum of entries after restore; restore fixture asserts exact counts, balances, `pricing_version`, `provenance`, and timezone effective dates preservation; profile writes atomically initialize timezone history and delay changed zones by 24 elapsed hours | Retained at least 7 years per credits spec and financial accounting requirements; preserve financial records while removing/anonymizing personal fields (learner_id nulled, owner_reference assigned); provenance, pricing version references, and timezone history preserved with entries; no UPDATE/DELETE/TRUNCATE permitted on ledger or timezone rows | [credits](../openspec/specs/credits/spec.md), [account-privacy](../openspec/changes/export-delete-account-data/specs/account-privacy/spec.md) |
| Postgres `credit_usage` | Content-free attempt and routing audit records: provider, model, latency, attempt count, fallback flag, sanitized `error_code`; no prompt, completion or private error text | Nightly encrypted financial dumps, 35-day snapshot rotation; restore comparison of immutable attempt records | Retained 7 years per financial audit policy; joins `credit_jobs`; account deletion severs learner identity to SHA-256 pseudonym | [bound-model-routing-failures](../openspec/changes/bound-model-routing-failures/proposal.md) |
| Postgres | `account_deletion_tombstones` (SHA-256 pseudonym, requested_at, applied_at), `posthog_person_deletion_requests` (distinct_id UUID queue) | SQL replay cache included in database dumps; authoritative immutable object intents live outside restored data and are merged via `ReplayDeletionLedger` before HTTP listeners open | Retained indefinitely to prevent restored backups from resurrecting deleted accounts; PostHog requests deleted upon API acceptance | [account-privacy](../openspec/changes/export-delete-account-data/specs/account-privacy/spec.md), [data-backup](../openspec/specs/data-backup/spec.md) |
| Backup object store `account-deletions/v1/<SHA-256>.json` | Immutable JSON containing only `learner_reference` (SHA-256 learner UUID); no UUID, email, text or timestamp | Authoritative ledger outside database dumps; never restored/replaced from a dump; indefinite retention, excluded from 14/35-day backup expiry | Atomic conditional Put before erasure; deterministic key makes retries/concurrent writes idempotent. No delete API. Startup migrates existing SQL tombstones, checks restored learner hashes, and reapplies erasure before listening; errors prevent serving. A durable intent remains authoritative if the subsequent SQL transaction fails. Real pre-deletion dump/restore fixture proves accounts stay deleted. | [review privacy contract](../openspec/changes/harden-account-privacy-review/specs/account-privacy-review/spec.md) |
| Postgres | River jobs, queue/leader metadata, Goose/River migration versions; `database_backup` args contain only a UTC date | Included in full database dump; validate schema versions and retry safety on restore; backup date survives retries across midnight | River cleanup policy; no learner content or credentials in backup/dummy jobs. Backup jobs use three attempts and a separate bounded queue | [data-backup](../openspec/specs/data-backup/spec.md), [ADR 0003](adr/0003-data-stores.md) |
| Postgres | Immutable `pricing_versions`: approved version, effective time, JSON action prices, packs, plans, benefits, limits and development-only synthetic product mappings; append-only `pricing_version_cancellations` and `pricing_activation_events` record cancellation and once-only activation | Included in full nightly encrypted database dumps; financial snapshots rotate after 35 days; resolver equality, cancellation and activation history checked by a custom-format dump/restore integration test | Financial catalogue, schedule, cancellation and activation history retained at least 7 years in the live DB and subsequent dumps; UPDATE/DELETE/TRUNCATE refused; no learner/admin identity or content, so account deletion retains the catalogue unchanged | [pricing-config](../openspec/changes/pricing-config/specs/pricing-config/spec.md), [ADR 0007](adr/0007-credits-and-payments.md) |
| Postgres | `pricing_admin_grants`, mutable `pricing_drafts` (including rollback ancestry), append-only `pricing_audit` with one-way SHA-256 actor IDs and private operator `override_reason`, plus `auth_sessions.reauthenticated_at` for recent-auth checks | Included in full nightly encrypted financial dumps; restore fixture compares grants, scheduled and rollback draft ancestry, cancellation, activation and audit rows alongside approved pricing | Drafts, approved versions, schedules, cancellations, activation markers and audit are retained at least 7 years. Audit actor UUIDs are pseudonymized before insertion; admin deletion removes the grant and pseudonymizes draft actor IDs while preserving pricing history. Audit UPDATE/DELETE/TRUNCATE is refused. Anonymous/non-admin denied requests share a per-process sample of at most one immutable row per minute; authenticated granted-admin actions retain full audit. The bounded sampler retains no IP or request payload. Authenticated account export includes pricing_admin_grants for granted administrators. | [pricing-admin-workflow](../openspec/changes/pricing-admin-workflow/specs/pricing-admin-workflow/spec.md), [account-privacy](../openspec/changes/export-delete-account-data/specs/account-privacy/spec.md), [ADR 0011](adr/0011-owner-decisions-2026-11-09.md) |
| Postgres `pricing_verification_evidence` | Immutable snapshots of actual per-environment Stripe mapping checks, App Store/Google Play identifier syntax checks, trusted economics minima, measured action costs, and SKU margin calculations (explicit synthetic-mode marker when the development fake is selected); strictly no learner text, prompt text, or provider API credentials | Included in full nightly encrypted database dumps; financial snapshots rotate after 35 days; verified by backup restore suite | Retained at least 7 years in the live DB and subsequent dumps per financial accounting requirements; immutable trigger prevents UPDATE and DELETE | [E12-07 contract](../openspec/changes/verify-external-mappings-and-catalogue-economics/proposal.md) |
| Browser session storage | Last known public pricing DTO with version/effective time and its ETag; exposure dedupe keys scoped to UUID/session/surface/version | Disposable cache; pricing recovered and revalidated from API; no backup | Session lifetime; offline prices explicitly labelled; no learner content, provider mappings or payment details. Clear cache and dedupe keys with the browser session; restore checks verify an empty session starts from the API. | [pricing-config](../openspec/changes/pricing-config/specs/pricing-config/spec.md), [analytics](../openspec/specs/analytics/spec.md) |
| Postgres `lesson_generations`, `generated_lessons`, `lesson_slides`, `lesson_questions`, `lesson_retry_pools`, `lesson_completion_outbox`, `lesson_completion_summaries`, `lesson_test_variant_snapshots` | Lesson generation job state and private resumable validated-result/usage checkpoint (cleared atomically on completion or failure), validated Motion HTML slides with taught topics, plain-text narration, original questions, private pre-generated retry pools, authoritative pass completion outbox, completed lesson summaries with weak topics, and served retry variant provenance and instruction snapshots | Nightly encrypted general dump, 14 days; populated full-row backup comparison and restored deletion checks | Read/export for authenticated owner only; account and course deletion cascade; account deletion within 30 days | [E4-07 change](../openspec/changes/handle-exhausted-retry-pools/design.md) |
| Postgres `lesson_test_sessions`, `lesson_test_answers`, `lesson_test_answer_operations` | Lesson test sessions, question queue with opaque single-use IDs and private source/original references, per-question resolutions, submitted answers, operation keys bound to session/lesson/question/choice and correctness; one in-progress session per learner and lesson | Nightly encrypted general dump, 14 days; populated backup comparison | Life of account; delete within 30 days of account deletion via learner cascade | [E4-04 change](../openspec/changes/resume-and-replay-lesson-test-sessions/design.md) |
| Postgres (planned) | XP events, streaks, freezes, daily goals, league membership | Nightly dump; source for rebuilding Redis | XP events for account life; personal rows deleted within 30 days of account deletion | [gamification](../openspec/specs/gamification/spec.md) |
| Postgres (planned) | Friends, requests, blocks, friend streaks and nudges | Nightly dump | Remove relationships and end shared streak on account deletion; personal rows within 30 days | [friend-streaks](../openspec/specs/friend-streaks/spec.md) |
| Postgres `quick_notes` | Learner-owned raw notes, stable action key, course/lesson/slide references, status, review flag, reviewed timestamp, optimistic concurrency version and raw_version | Full nightly encrypted database dumps, 14-day retention; populated backup fixture compares full rows | Authenticated owner read/export; deleted with the account within 30 days; restore tombstones erase before serving | [E9-01 change](../openspec/changes/e9-01-save-raw-quick-notes/design.md), [E9-03 change](../openspec/changes/e9-03-resume-pending-enrichment/design.md), [E9-04 change](../openspec/changes/e9-04-find-review-export-notes/design.md), [quick-notes](../openspec/specs/quick-notes/spec.md) |
| Postgres `bookmarks` | Learner-owned unique lesson/slide bookmarks, stable action key, course/lesson/slide titles | Full nightly encrypted database dumps, 14-day retention; populated backup fixture compares full rows | Authenticated owner read/export; deleted with the account within 30 days; restore tombstones erase before serving | [E9-05 change](../openspec/changes/e9-05-bookmark-lessons-and-slides/design.md), [bookmarks](../openspec/specs/bookmarks/spec.md) |
| Postgres (planned) | Tutor chat threads/messages; enrichment tags/links | Nightly dump | Per-item deletion where supported; account deletion within 30 days | [tutor-chat](../openspec/specs/tutor-chat/spec.md) |
| Redis | Disposable session/cache entries, rate limits, cached balances, leaderboards and streak counters | No backup; rebuild from Postgres | TTL or eviction; never sole copy of product data | [data-backup](../openspec/specs/data-backup/spec.md), [gamification](../openspec/specs/gamification/spec.md) |
| S3 application bucket | Motion HTML slides, manifests (including static slide explanations), lesson assets; optional golden-set mirror | Object versioning; tracked regeneration jobs; verify cloud capability before use | Match owning course/account deletion; object-version expiry policy must be specified before real data | [lesson-player](../openspec/specs/lesson-player/spec.md), [course-creation](../openspec/specs/course-creation/spec.md) |
| S3 separate backup bucket | age-encrypted custom Postgres dumps at `YYYY-MM-DD/schema-<Goose version>.dump.age`; includes learner, append-only ledger and migration/queue state | Worker at 02:00 UTC plus startup catch-up; public X25519 recipient only, conditional whole-object upload, no multipart remnants. Railway snapshots remain a separately provisioned second layer | General backups 14 days; financial backup policy 35 days per ADR 0011. API has no backup access; writer can read/put but cannot delete or administer buckets. Separate retention administrator owns expiry; cloud lifecycle automation remains a follow-up | [data-backup](../openspec/specs/data-backup/spec.md), [ADR 0011](adr/0011-owner-decisions-2026-11-09.md) |
| Worker temporary filesystem | Ciphertext-only private staging file (0600); no plaintext database archive | Disposable, never a recovery source; removed on success, dump failure and upload failure | One bounded job (30 minutes, at most 4 GiB encrypted output); container filesystem is ephemeral after abnormal process termination | [worker backup change](../openspec/changes/worker-database-backup/design.md) |
| Local RustFS IAM metadata | Separate application, backup writer and root/retention identities and policies; local fixture secrets are environment settings | Reprovisioned by the development-only initializer; production IAM/keys managed separately | Never pass root or backup credentials to API containers; private age recovery key is never in worker images or environment | [worker backup change](../openspec/changes/worker-database-backup/design.md) |
| Device OS credential store / browser session fallback | Access/refresh token pair in desktop OS keyring through fixed-service Tauri commands; web build only uses sessionStorage with an in-memory fallback. Analytics preference is a non-secret local boolean. Native mobile storage fails closed pending a supported implementation. | Credentials excluded from app backups; reissued by email-code sign-in | Logout revokes server session and deletes device credentials. Browser fallback ends with the browser session. No localStorage tokens; native keyring failures never fall back to browser storage. | [email-code-sign-in](../openspec/changes/email-code-sign-in/specs/accounts/spec.md), [ADR 0004](adr/0004-auth.md) |
| Device cache | Cached slide copies | Disposable cache; never the sole copy of lesson assets | Cache eviction/logout | [lesson-player](../openspec/specs/lesson-player/spec.md) |
| Hugging Face + S3 mirror + CI artifacts | Golden-set revisions; eval reports | Versioned dataset; verified pinned SHA-256 mirror in S3 (RustFS locally); reports as CI artifacts | Keep dataset revisions; eval artifacts 90 days | [ai-reliability](../openspec/specs/ai-reliability/spec.md) |
| PostHog | Product events, backup/drill results, AI token/latency metadata; `quick_note_created` (source, length only), `quick_note_enriched`, `quick_note_enrichment_failed`, `quick_note_reviewed`, `quick_note_deleted` (note_id only), `ui_sheet_opened` (sheet_id), `ui_control_activated` (control_id, screen, surface, course_id), `bookmark_added`, `bookmark_removed`, `bookmark_opened` (target_type); no note/chat/profile text or search queries | Derived store; never sole product-state copy | Project-plan event retention; request person deletion on account deletion; no note/chat/profile text | [analytics](../openspec/specs/analytics/spec.md) |
| API, worker and engine JSON logs/traces | Opaque server request/trace/span IDs, fixed operation/outcome, elapsed time; worker attempt and safe alert records; no tokens, bodies, URLs, prompts, private text or provider errors | Derived, excluded from recovery; source/config recovered from Git | Docker size rotation: 10 MiB × 3 files per service; deletion/recreation discards telemetry. No learner identity linkage. Production collectors must enforce equivalent bounds | [E11-04 observability](../infra/observability/README.md) |
| Postgres River job metadata | Validated request_id, trace_id and parent span_id under correlation; backup args remain UTC date only | Existing encrypted nightly database dumps; focused synthetic dump/restore compares the metadata of a completed retried job | Existing River terminal-job cleanup; no learner ID or content. Legacy jobs get fresh correlation; account deletion needs no new personal-data store | [E11-04 change](../openspec/changes/trace-requests-workers-alerts/design.md) |
| Private operational metrics and incident memory | Fixed-label call/error/duration/alert counters; bounded dedupe keys for terminal jobs and dependency outage state, independent of PostHog | Disposable memory and derived Prometheus volume; scrape/alert config and dashboard queries in infra are Git-backed | In-process counters reset on restart; dedupe capped at 4096 keys. Collector metrics expire after seven days or 256 MB; deletion/recreation resets the derived store | [E11-04 observability](../infra/observability/README.md) |

Pricing administrator grants are included in account exports and the learner-table
deletion classification; the focused privacy fixture seeds a grant, checks its
export and verifies its existing ON DELETE CASCADE policy removes it.

Postgres recovery objectives are RPO 24 hours and RTO 4 hours. Restoring a backup
into non-production requires prior anonymization. E1-07 implements the explicit
financial retention and anonymization design: on account deletion, `credit_entries.learner_id`
and `pricing_quotes.learner_id` are set to null while preserving a SHA-256 `owner_reference`
for lawful seven-year financial retention; `pricing_admin_grants` are revoked and erased, while
`pricing_drafts` actor references are pseudonymized and immutable `pricing_audit` history is retained.
Personal rows cascade-delete immediately. Immutable external deletion intents are merged into the SQL tombstone replay cache after restore before traffic is served. Export/delete require a live session authenticated in the preceding 15 minutes and independent per-learner limits of three attempts per action per hour. Refresh does not extend authentication freshness; encrypted export remains passphrase protected.

E11-01 recovery checks decrypt an actual worker-image backup into a different
synthetic database, compare complete non-empty identity/profile/code/session/
refresh-token/rate-limit rows (including hashes, expiry and analytics preference),
reject an expired restored code, and check Goose/River metadata, exact learner/ledger counts and
balance (120 - 35 = 85), compare the complete approved pricing catalogue, and
reject ledger/catalogue UPDATE/DELETE/TRUNCATE after restore. Archive names and
restored schema versions must match the fixture's applied migration version.
The suite also retries interrupted uploads, verifies one complete object and
checks that writer and API identities receive AccessDenied on backup deletion.
No production data is restored to local development by these tests.

E13-01 client theme foundation: `docs/design/tokens.json` is the authoritative
light/dark token artifact; the client imports `docs/design/theme.css` directly.
Tokens, component sources and licensed bundled Nunito WOFF2 assets are Git-backed
and restored together from the same revision. Course responses, search input and
the optional E1-06 analytics context stay in memory and disappear when the app
closes; this slice adds no device persistence or learner-data deletion work.
Existing API course retention and backup policies still apply. UI analytics use
stable control IDs only and remain silent without an enabled UUID context.

E13-02 application shell: navigation selection, per-route scroll positions and
responsive layout state are disposable memory only. Resizing keeps the same
route and editor tree. The current slide and return course survive Exit/reopen
within the shell session. Recreating the router/host resets local state; no local
storage, database or backup job is added. Feature owners remain responsible for
durable drafts and deletion. Shell source and its OpenSpec/canvas references are
restored together from Git. Focused tests recreate hosts to verify draft disposal
and check that private route IDs/queries never enter screen/navigation analytics.

E13-03 shared guided-chat components and authored fixtures are Git-backed under
`apps/client/src/features/design/chat/` and `sheet/`. Restore component, fixture,
and token sources from the same Git revision, then build via `mise run client:build`.
The standalone `guided-chat.html` harness keeps chat/composer, summary, outline,
Undo history and sheet drafts only in React memory. Unmount or reload deletes
these inputs and restores authored defaults (covered by the focused integration
and browser checks). No database, object-storage or device persistence is added;
no additional persisted-data restore/deletion checks are needed. PostHog receives
only allowlisted UI control/sheet IDs and common identity properties through the
existing opt-out boundary; no learner content or duplicate API outcomes.

E12-02 action quotes: Postgres `pricing_quote_policies` extends each immutable
Pricing config version with token-class nanodollar rates, model/context/voice
allowlists, conversion and TTL. `pricing_quotes` stores the complete immutable
terms, component ceilings, eligible bucket, request counts, expiry and a version
foreign key; `pricing_quote_consents` records approval independently of wallet
reservations. All three tables are financial history retained at least seven
years in the live database and fresh encrypted nightly dumps (35-day snapshot
rotation per ADR 0011). Account deletion nulls the learner FK while preserving a
SHA-256 pseudonymous owner reference. No prompt, topic, note or chat text is held.
Backup fixtures include non-empty accepted quotes and compare full restored rows,
version policy and consent; restored history rejects mutation. Focused deletion
checks verify quotes survive with null identity. Client quote state is disposable
React memory; no device quote persistence. Existing AI eval/fallback gates apply;
quote creation makes no provider call.

E1-04 session recovery retains safe drafts only in the mounted feature tree, hidden
and inert while authorization is unavailable. Reauthentication as a different
Learner remounts the feature tree. Shared refresh work and serialized SecureStore
writes prevent stale responses from restoring credentials after logout. Navigation
and focus recheck authorization. Explicit sign-out removes device credentials only
after server revocation succeeds; offline failures retain them for Retry. No device
secret or draft is added to server backups. The encrypted worker restore fixture
includes consumed refresh hashes and a revoked family and proves they cannot
restore access; learner deletion still cascades families and refresh hashes.

E13-04 localization and appearance: typed English/Arabic messages and expansion
fixtures live in Git with the semantic tokens. Restore them from the same commit
and run `mise run client:test-localization` and `mise run client:build`.
Locale, appearance and motion choices are disposable React host memory by default;
no local storage or learner text is written. The `PreferenceAdapter` boundary lets
E1 supply validated account preference load/save/clear when available. The mounted E1 account supplies the initial locale;
appearance and motion have no durable E1 API yet. Adapter exceptions keep the local
choice and expose retry/reset; reset clears the adapter and restores defaults.
Focused tests cover adapter restore, allowlisted saves, clear/reset, corrupt input
and host recreation. Account deletion/logout must clear any future E1 adapter;
these components never copy chat, summary or outline text into it. Existing feature
retention, backup, evaluation and fallback gates continue to apply.

E1-11 setup chat: setup chat transcripts, quick replies and live summary drafts are
disposable React state in client memory only and are never persisted to Postgres, Redis,
or object storage. Confirmed profiles write to the existing `learner_profiles` table
via canonical `POST /v1/me/profile` under existing 14-day backup and account deletion
cascade policies. No chat or profile text enters PostHog; analytics emits only event
metadata and token counts (`$ai_generation`, `ui_control_activated`). Development pricing
key `setup_chat` is inserted into immutable `pricing_versions` (`usd-dev-unapproved`,
`approved=false`, 0 credits) with existing 7-year financial retention.

E7-03 adds `credit_jobs` (quote, source owner pseudonym, job state, dates, deadline
and finalized amounts), immutable `credit_allocations` (component/source holds),
immutable `credit_usage` (content-free per-attempt provider/model/cache/token/
character/fallback counters), `credit_cost_counters` (held and spent integer USD
nanodollars), immutable `credit_artifacts` (opaque component delivery deduplication)
and `credit_outbox` (deduplicated content-free financial events). These are
financial records in encrypted nightly dumps, with 35-day backup rotation and
seven-year database retention. Nonempty settled and pending jobs, every new table,
exact balances and immutable rows are compared in the restore fixture. Postgres
counters survive Redis/analytics loss and are never reconstructed from PostHog.
Account export includes all six tables; deletion nulls the two learner FKs while
retaining SHA-256 owner references, source links and the audit trail. Tombstones
replay that same erasure after restore. No prompts or generated learner content
are stored by this boundary. Device reservation state is disposable React state.

E10-03 bounded model routing failures:
Immutable `credit_usage` records per-attempt provider/model/fallback/latency metrics and sanitized categorical `error_code` values (`rate_limited`, `provider_outage`, `timeout`, `cancelled`, `invalid_schema`, `expensive_fallback`, `unknown_failure`). No raw provider prompts, completions, tokens, or private error payloads are logged or persisted. The attempt audit trail joins `credit_jobs` and follows existing financial backup policies: included in nightly encrypted database dumps (age-encrypted, 35-day snapshot rotation, minimum 7-year live retention). Account deletion policies apply without modification: learner links are severed while preserving SHA-256 pseudonymous owner references and financial provenance. PostHog `$ai_generation` events capture sanitized attempt metadata with allowlisted `error_code` and UUID distinct IDs under learner opt-out.
# E7-06 payment fulfillment

E7-07 extends the same receipt/outcome pipeline to monthly subscriptions.
`credit_subscriptions` holds immutable provider/environment/customer bindings via
the original order; `credit_subscription_terms` holds paid invoice references,
funded service-month keys, service periods, Pricing/entitlement version, configured
allowance, benefit/concurrency snapshots and the original ledger grant ID. Both
tables reject UPDATE/DELETE/TRUNCATE. They contain no email, learner content or
direct learner UUID. Account export follows the owned order (including invoice
and lifecycle receipts); deletion and restored tombstones sever the order's
learner link, preserving the financial pseudonym and dedupe history.

These tables enter nightly encrypted Postgres financial backups, retained at
least seven years in the live financial history, with 35-day snapshot rotation.
The nonempty backup fixture compares complete restored rows, reconciles allowance
and entitlements and replays duplicate funding before and after identity deletion.
Entitlements are read from funded periods using database time; Redis wallet caches
remain disposable and cannot extend benefits. Issued plan credits never expire.
Fixture-only management URLs contain no provider secrets or personal data.
No new environment variables or AI behavior are introduced.

`credit_payment_sessions` pins an immutable test-environment Session and configured
price to `credit_checkout_orders`. `credit_payment_inbox` stores only event ID,
event type, environment, Session ID and receipt time. `credit_payment_results`
stores immutable processing outcomes, order references, environment-scoped business
payment keys and the one purchase ledger ID. These financial and dedupe references
are retained at least seven years, covered by encrypted nightly financial snapshots
with 35-day rotation and existing PITR/restore procedures.

`credit_payment_payloads` holds only an allowlisted redacted event envelope, never
customer details, arbitrary metadata, signatures, keys or learner content. It expires
after 30 days; the credit worker prunes it every minute and backup preparation prunes
it before dumps. Expiry never removes immutable inbox or dedupe history. Financial
snapshot rotation may retain a pre-expiry redacted envelope for the documented
35-day recovery window; restore preparation reapplies expiry before serving.

All four tables are exported through the authenticated original order/Session link.
Account deletion and restored tombstones null the order's learner FK and retain its
SHA-256 owner reference. No new payment table stores a learner UUID or email, and
replay after erasure never grants credits or emits analytics. Account erasure and restore tombstone replay release outstanding credit jobs before anonymization, remove their held cost counters, and retain the worst-case provider cost for running jobs with unavailable usage. Restore fixtures include
nonempty rows, full row comparison, immutable-table checks and payment replay checks.
Client recovery uses existing disposable account-scoped order IDs only. No new AI
behavior, prompts or model data are introduced; existing eval/fallback gates apply.

E7-08 adds immutable `credit_subscription_schedule` (funded term, month index,
UTC due date and unique grant key), `credit_subscription_cancellations` (canonical
renewal-off history), and `credit_subscription_revocations` (canonical full-term
refund/dispute history). All join identity only through the existing funded term,
subscription and original order. Account export includes these rows; deletion and
restore tombstones sever the order identity and prevent further grants/analytics.
They retain at least seven years in financial history and nightly encrypted dumps
with 35-day snapshot rotation. Nonempty backup fixtures compare every row, catch up
the remaining funded months exactly once after restore, and assert revoked terms
and deleted learners cannot receive further grants. Issued ledger credits remain
non-expiring. River's minute/start sweep reconstructs due work from these tables;
queue timers and PostHog are never the recovery source.

E7-09 adds append-only `credit_subscription_lifecycle` (bound canonical provider
status, item period, cancellation flag, optional inbox reference and observation
sequence) and `credit_subscription_transitions` (idempotency request, provider
subscription, kind, effective date, pinned Pricing version/offer/plan and provider
price reference). Both link to the learner only through the original immutable
subscription/order. Account export includes them; deletion and restored deletion
tombstones sever identity at that order. Retain at least seven years, in nightly
encrypted financial dumps with 35-day snapshot rotation and PITR where available.
Backup fixtures seed cancellation/resumption transitions and compare complete
rows after restore, including dedupe/effective dates; replay never reissues funded
Credits. Redis wallet state and priority are disposable derived data. No card data,
raw notification bodies or learner content enter these tables or analytics.

| Postgres `course_creations` | Owned immutable confirmed outline/revision, quote/reservation IDs, durable stages and validated response; no lesson content | Nightly encrypted general dumps, 14 days; populated fixture restored and compared in full | Owner export; account cascade erasure and restore tombstones; financial usage stays separately anonymized for seven years / 35-day financial snapshots | [E2-02](../openspec/changes/create-confirmed-outline/design.md) |
| Postgres `courses`, `course_units`, `course_lesson_stubs`, `course_enrollments` | Owned Course and exact ordered Units/stubs/goals; first ready_to_generate, others locked; single enrollment | Nightly encrypted general dumps, 14 days; full-row fixture comparison and deletion replay | Owner export; account cascade erasure; Course soft-delete metadata, 30-day grace before hard deletion (removal UI and automated purge) | [E2-02](../openspec/changes/create-confirmed-outline/design.md), [E2-04](../openspec/changes/recover-generation-and-remove-courses/design.md) |
| Postgres `course_enrollments.level_override`, `course_enrollments.last_active_at`, `learner_profiles.active_course_id` | Per-course level overrides, enrollment access timestamps and persisted active course selection; feeds effective AI context without mutating global profile | Nightly encrypted general dumps, 14 days; full-row fixture comparison and deletion replay | Owner export; account cascade erasure; level override reset to global | [E2-03](../openspec/changes/switch-courses-override-level/design.md) |

E3-01 stores `lesson_manifests` (owned Lesson UUID, content version, immutable
manifest object key, created timestamp) in encrypted nightly general Postgres
backups with 14-day rotation. Account export includes these references; direct
Learner and Lesson cascades erase them on account/course hard deletion. Existing
account deletion tombstones reapply erasure after restore. Course soft deletion
and revoked enrollment immediately deny manifest and document reads.

Manifest JSON and Motion HTML use immutable versioned `lessons/…` application
object keys; assets may load only from configured `LESSON_ASSET_ORIGIN`. They follow
the data-backup protected nightly asset-copy policy with key/version/SHA-256
manifests. The disposable RustFS backup fixture includes all four nonempty lesson
objects, a separately versioned protected copy and checksum manifest, and proves
recovery after current objects are lost, without generation. Postgres restore
compares complete metadata and replays deletion. Shared synthetic fixture bytes
contain no personal data and stay Git-backed; learner-linked content must be
purged within 30 days when no independent references remain under existing object
retention policy. This ticket adds no learner-specific asset generation.

The player holds document replicas in account-scoped query memory only; E3-03
owns durable offline caching. Loading/failed/offline content never grants
completion; Take lesson test is only navigation. PostHog receives opaque IDs and
fixed error codes, never HTML, captions, titles, notes, URLs or answer keys. No new
AI behavior or provider calls are introduced.

E3-03 adds disposable per-account Cache API lesson bundles (manifest version,
SHA-256 document/asset checksums, HTML, assets), capped at five lessons and 40 MiB
per account, 8 MiB per lesson. One complete response is published after validation;
failed replacements preserve the prior version. Logout, account changes and account
deletion clear bundles and invalidate in-flight writes. Minimal offline identity
(UUID and consent, bound by hash to the existing refresh credential) contains no
learner text or raw credentials and is removed with device state. Only public
application shell files enter the service worker cache. All device copies can be
evicted and are excluded from backups/export. Authoritative immutable server
objects and existing lesson metadata retain E3-01 backup/export/deletion policies;
restore fixtures assert byte-identical checksums. No new learner tables or AI calls.

E10-02: Score each AI task with critical checks. Versioned golden-set revisions
are maintained on Hugging Face (or Git-pinned in `evals/fixtures/golden.sample.jsonl`
for offline runs). Evaluation reports (JSON and Markdown) are written to
`evals/reports/` and retained for 90 days as CI artifacts. Reports record per-task
deterministic and judge scores, per-task thresholds, critical check outcomes, dataset
name, dataset revision, prompt revision, and model route mappings. Reports and analytics
strictly exclude any learner personal text or prompt/completion bodies. The operational
analytics event `eval_run_completed` emits only task name, score, threshold, passed flag, dataset revision, dataset source, provider, status, and budget
under the reserved system UUID `00000000-0000-4000-8000-000000000011` with opt-out
enforcement. No new database tables or personal learner data stores are created.

| Postgres `eval_alert_configs`, `eval_alert_events`, `eval_operational_aggregates` | Threshold configurations, deduplicated operational alert event history, and daily aggregate counters per AI task (total, fallback, error, tokens, latency, cost, revenue); no prompts, completions or learner text | Included in nightly encrypted database dumps, 14 days retention; backup fixtures seed and compare operational records | Retained for operational history; not learner-linked; account deletion leaves aggregate counters unaffected | [E10-05 change](../openspec/changes/expose-reliability-and-ai-cost-dashboards/specs/ai-reliability/spec.md) |
| Postgres `eval_reports` | Evaluation run reports (task, dataset revision, prompt revision, model, score, threshold, passed flag, structured metrics details); strictly no personal or prompt text | Included in nightly encrypted database dumps, 14 days general retention; live rows expire after 90 days (`expires_at` column) | Live rows retained for 90 days per eval artifact retention policy; not learner-linked; account deletion leaves eval reports unaffected | [E10-05 change](../openspec/changes/expose-reliability-and-ai-cost-dashboards/specs/ai-reliability/spec.md) |

E10-05: Expose reliability and AI cost dashboards. Introduces `eval_alert_configs` (alert thresholds and windows), `eval_alert_events` (deduplicated operational alert history), `eval_reports` (golden-set evaluation runs with 90-day retention), and `eval_operational_aggregates` (daily rollup counters for total calls, fallback calls, error calls, latency, tokens, cost nanos, and credit revenue cents). Dashboards and alerts are derived purely from operational aggregations, pricing catalogue prices, and evaluation reports. Neither prompts, completions, nor learner-submitted text are stored in these tables or exposed via endpoints. Operational recording and server-side counter increments continue independently even if PostHog analytics is down or if the learner opted out of analytics.

All `/v1/eval/` reads require a live account session, a `pricing_admin_grants` grant, and authentication within the last 15 minutes, using the same policy as pricing administration. `POST /v1/eval/reports` instead requires `Authorization: Bearer <EVAL_REPORT_SERVICE_TOKEN>` from a trusted eval service. Configure that secret independently of account credentials; ingest is disabled when it is unset. The API compares tokens in constant time, caps report bodies at 1 MiB (HTTP 413 on overflow), and logs internal errors while returning only `internal server error` for HTTP 500. The service token grants no dashboard access and is never stored in reports or analytics. Backup, report retention, PostHog schemas, and model eval/fallback behavior remain unchanged.

# E17-03 private Characters and Course Casts

`characters` stores private ownership/current version and a soft deletion date;
`character_versions` stores immutable, closed v1 JSON specs; `character_screenings`
stores only verdict/reason/policy/attempt time (never rejected text or SVG);
`course_casts` stores an owned Course's free selection; `lesson_cast_snapshots`
stores independent immutable approved payloads, so library deletion cannot rewrite
history. All are included in encrypted nightly Postgres dumps with 14-day roll-off,
account export and cascading account erasure. Restore replays existing durable
account deletion tombstones before serving. The backup fixture compares all five
tables and verifies deletion cascades.

Soft-deleted private Characters/versions and screening metadata purge within 30
days, hourly and before nightly dumps. Independent historical Cast payloads last
with their Course; deletion of a Course/account cascades them. Future E15 publishes
independent allowlisted snapshots under its retention policy. No per-learner SVG or
S3 asset is created by free edits: trusted `original-v1` rigs are bundled versioned
release artifacts and retained with the application. Existing optional asset
backup/checksum checks remain unchanged. Redis and PostHog/device state are derived.
No financial records, raw prompts, price or `character_create` spend are added.
E12-07: Verify external mappings and catalogue economics. Introduces `pricing_verification_evidence` table recording immutable verification snapshots of Stripe price mappings, App Store/Google Play store identifiers, official AI provider model rates, and catalogue margin calculations for verified pricing versions. Contains strictly no learner text, prompt text, or provider API credentials. Retained for at least seven (7) years in the primary Postgres store under financial record policy; covered by nightly encrypted dumps; guarded by an immutable SQL trigger against UPDATE or DELETE.


E17-04: `character_create_jobs` contains learner-scoped idempotency keys,
operation/state, pinned quote and opaque reservation references, transient prompt
payloads and accepted immutable Character results. Private data is exportable;
account deletion cascades jobs and linked screenings, erasure tombstones replay
before serving restore. Character deletion purges its job payloads within 30 days;
independent Lesson snapshots survive under their own policy. Prompts expire at
terminal state or within 24 hours, including abandoned jobs, with a pre-backup
purge; screening verdicts expire after 30 days. Jobs/versions/verdicts enter nightly
encrypted 14-day Postgres dumps and existing original rig assets remain Git-backed
or protected versioned objects. Ledger/quote links retain opaque financial history
for seven years; no raw prompt enters financial, Course or publication records.
Focused restore tests replay terminal jobs without mapping/spend and compare
current immutable versions, dedupe keys and released reservations.

# E2-05 authored shared example

`services/api/internal/courses/demo/sky-v1.json` is an immutable Git-authored public
outline and Motion HTML bundle, keyed as `lessons/shared-demo/sky-v1/bundle.json` in
versioned object storage. It contains no learner data. The API embeds the same
bytes and has no database, wallet, enrollment or engine dependency. The protected
RustFS asset-copy fixture includes the bundle, version ID and SHA-256, loses the
current object and restores byte-identical content solely from the protected copy.
Rebuild from the same Git revision to restore embedded serving. New content uses
a new key/version; account deletion does not remove public authored content.
No learner-linked table is introduced, so `accounts/privacy.go` policies and
database backup fixtures are unchanged. Brief/session and outline drafts keep
their existing export/erasure/retention boundaries; the demo does not copy them.
Quote consent stays disposable and is invalidated on demo entry. Live pricing
and catalogue activation remain gated by E12-08; no effort Credit rewards exist.


E10-04 / issue #160 defect 5: `evals/pinned-datasets.toml` stores reviewed
(dataset, immutable revision, SHA-256) integrity metadata in Git, embedded in the
eval binary. Fixture-only identities remain separate from future E10-01 production
pins. Hugging Face revisions and protected versioned RustFS/S3 mirror objects
retain their existing backup policy and immutable keys. Mirror headers/sidecars
are required transport integrity metadata; repository pins are the trust source.
Disposable `evals/.cache` bytes and sidecars must both match the pin; missing
sidecars are rejected and never generated from existing cache bytes. Recovery
removes incomplete cache files and refetches verified source bytes; restored
mirror/cache data receives the same verification before evaluation. No learner
content, credentials or new database state is added. Eval JSON/Markdown reports
include `dataset_verified` under the existing 90-day artifact retention policy;
legacy/custom/offline inputs are unverified and excluded from live promotion.
Remote integrity failures produce no passing report. Existing content-free
`eval_run_completed` analytics and account export/deletion policies are unchanged.

### E3 review: playback ownership and containment

The composite playback-event key replaces the previous global primary key by
constraint migration without deleting rows. Export, cascade deletion, deletion
tombstones, and encrypted backup retention remain attached to the learner.
Sync responses contain only keys from the authenticated learner's request, with
separate `acked_keys` and `rejected_keys`; invalid events never change progress.
Device download indexes and prepared dynamic HTML are account-scoped and cleared
on logout. Home uses saved indexes only after network failures, never HTTP
errors. Prepared dynamic HTML has no permitted network origin. Bulk-download
cache bounds reset after every attempt; device caches remain disposable.

### E5-01 completion XP and daily goals

Postgres `gamification_config` stores versioned award amounts and allowed/default goals, with no learner data. `xp_events` is immutable for the account lifetime and unique per learner/lesson; it retains original completion UUID/time, lesson/course UUIDs, awarded amount, perfect flag, config version, IANA timezone and local date. Lesson/course deletion preserves this event source for reconstructing total/daily/weekly XP. `daily_goal_achievements` retains one immutable goal crossing per learner/local date; the current preference remains `learner_profiles.daily_xp`.

Postgres `streak_days` (one row per learner and local date, first XP event id), `streak_state` (current/longest days, last active date, `decided_through` horizon) and `streak_breaks` (one marker per first missed local date and its previous length) project the streak from `xp_events` and carry no learner text. They are included in nightly dumps and the account export and cascade-delete with the learner. The projection is rebuildable from `xp_events`; the rollover worker rebuilds a lost projection silently (no analytics) and never moves `decided_through` backwards after a time zone change. Streaks never grant Credits. Redis is not used for streaks.

All three tables enter nightly encrypted Postgres dumps (14-day general retention). Synthetic fixtures seed XP/goal markers and compare complete restored rows. XP and achievement rows export in the authenticated account export, cascade on account deletion, and follow external deletion-ledger replay before serving a restored snapshot. Redis and PostHog remain disposable derived data. Analytics sends only UUIDs, numeric awards/goals, dates, config version and a perfect boolean through the central fail-closed consent gate; no summary, question, answer or profile text. XP and daily goals never grant Credits. No AI/model change.
