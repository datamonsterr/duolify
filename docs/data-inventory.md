# Data inventory

Owner specs are authoritative for collection and deletion. Every feature adding
data must update this table and its restore checks. Bootstrap currently creates
`learners`, email-code auth/profile/session tables, `credit_entries`, immutable `pricing_versions`, Goose/River metadata and River's queue tables;
the other product records below are planned. E11-01 implements the local nightly
worker dump/upload and synthetic recovery proof; cloud keys, lifecycle policies
and production drills still require provisioning. See [backup tooling](../infra/backup/README.md).

| Store | Data held | Backup method | Retention / deletion | Owner spec |
| --- | --- | --- | --- | --- |
| Postgres `course_brief_revisions` | Immutable learner-owned confirmed Course briefs, content hash and expiry; no transcript | Nightly encrypted general dump, 14-day retention; populated backup fixture compares full rows | Read/export for authenticated owner only; expires after 30 days, nightly pre-backup purge (within 24 hours); immediate account cascade and restored deletion tombstones | [E2-06 change](../openspec/changes/clarify-course-brief/design.md) |
| Device session `course-brief:<learner UUID>` | Unconfirmed brief, composer and guided state | Disposable; no server backup or text in URLs | Session lifetime, survives navigation/auth/shop return for same UUID; removed by account erasure; isolated between learners | [E2-06 change](../openspec/changes/clarify-course-brief/design.md) |
| Postgres | `learners`, `auth_identities` (normalized email), `auth_provider_identities` (Google and Apple provider subjects plus verified email, including Apple private relay; unique subject binding), `auth_oauth_attempts` (HMAC state/nonce hashes, PKCE challenge, exact redirect, 10-minute expiry), `learner_profiles` (device locale/time zone; optional current goal, topic, background, level, daily minutes/XP, reminder enabled/local time; setup completion; appearance, motion, narration, narration speed, captions, text size, UI sounds, haptics and analytics consent; age confirmation timestamp), `auth_codes` (keyed hashes, expiry and attempts), `auth_sessions`, `auth_refresh_tokens` (SHA256 hashes only), `auth_rate_limits` (HMAC email/IP keys plus SHA-256 brief learner/platform throttle identifiers; no learner text) | Full encrypted nightly dump to separate backup bucket; Railway volume snapshots; custom-format dump/restore fixture compares complete provider-subject bindings, OAuth attempts, learner context and auth state, including a synthetic Apple subject | OAuth attempts expire after 10 minutes and are single use; provider bindings cascade with Learner deletion. Attempts contain no provider access or refresh tokens. Apple name is stored as the Learner display name only when Apple supplies it at first authorization; later sign-ins resolve by stable subject when name/email are omitted. Codes expire after 10 minutes/five attempts; rate windows expire after one hour; sessions and refresh secrets expire after 30 days, access JWTs after 15 minutes. Expired codes/rates/sessions are pruned on code requests; OAuth attempts are pruned when a new attempt starts; expiry is enforced on every use. Identity/profile/session/token rows cascade with Learner deletion; identity deletion removes pending codes by email. Rate keys contain no reversible identity and expire within one hour. Personal data is removed within 30 days; encrypted backup copies roll off after 14 days. No profile history or draft answers are stored. Authenticated profile export returns current context and settings only. Restore checks compare every context field, profile setting/consent, provider binding and OAuth attempt, and verify cascade deletion, hashes, defaults, refresh replay, revocation and authentication expiry. Profile API/unit checks verify explicit confirmation, partial clear and preference preservation. Session families use the existing auth_sessions table; GET /v1/auth/sessions returns only owned active IDs, timestamps and current-device flags. Individual/all-device DELETE updates revoked_at; protected requests read Postgres directly with no authorization cache. No email, IP or user-agent device labels are collected. | [apple-oauth-sign-in](../openspec/changes/apple-oauth-sign-in/specs/accounts/spec.md), [google-oauth-sign-in](../openspec/changes/google-oauth-sign-in/specs/accounts/spec.md), [confirm-learner-context](../openspec/changes/confirm-learner-context/specs/learner-profile/spec.md), [email-code-sign-in](../openspec/changes/email-code-sign-in/specs/accounts/spec.md), [persist-settings-analytics-consent](../openspec/changes/persist-settings-analytics-consent/specs/learner-profile/spec.md), [learner-profile](../openspec/specs/learner-profile/spec.md), [data-backup](../openspec/specs/data-backup/spec.md) |
| Postgres | Append-only `credit_entries` (with nullable `learner_id` and SHA-256 `owner_reference`); derived purchased/reward balances | Same dump; PITR where available; reconcile balances with sum of entries after restore | At least 7 years per credits spec; preserve financial records while removing/anonymizing personal fields (learner_id nulled, owner_reference assigned) | [credits](../openspec/specs/credits/spec.md), [account-privacy](../openspec/changes/export-delete-account-data/specs/account-privacy/spec.md) |
| Postgres | `account_deletion_tombstones` (SHA-256 pseudonym, requested_at, applied_at), `posthog_person_deletion_requests` (distinct_id UUID queue) | Included in full database dump; restored tombstones replayed on startup via `ApplyDeletionTombstones` before HTTP listener serves traffic | Retained indefinitely to prevent restored backups from resurrecting deleted accounts; PostHog requests deleted upon API acceptance | [account-privacy](../openspec/changes/export-delete-account-data/specs/account-privacy/spec.md), [data-backup](../openspec/specs/data-backup/spec.md) |
| Postgres | River jobs, queue/leader metadata, Goose/River migration versions; `database_backup` args contain only a UTC date | Included in full database dump; validate schema versions and retry safety on restore; backup date survives retries across midnight | River cleanup policy; no learner content or credentials in backup/dummy jobs. Backup jobs use three attempts and a separate bounded queue | [data-backup](../openspec/specs/data-backup/spec.md), [ADR 0003](adr/0003-data-stores.md) |
| Postgres | Immutable `pricing_versions`: approved version, effective time, JSON action prices, packs, plans, benefits, limits and development-only synthetic product mappings; append-only `pricing_version_cancellations` and `pricing_activation_events` record cancellation and once-only activation | Included in full nightly encrypted database dumps; financial snapshots rotate after 35 days; resolver equality, cancellation and activation history checked by a custom-format dump/restore integration test | Financial catalogue, schedule, cancellation and activation history retained at least 7 years in the live DB and subsequent dumps; UPDATE/DELETE/TRUNCATE refused; no learner/admin identity or content, so account deletion retains the catalogue unchanged | [pricing-config](../openspec/changes/pricing-config/specs/pricing-config/spec.md), [ADR 0007](adr/0007-credits-and-payments.md) |
| Postgres | `pricing_admin_grants`, mutable `pricing_drafts` (including rollback ancestry), append-only `pricing_audit` with one-way SHA-256 actor IDs, plus `auth_sessions.reauthenticated_at` for recent-auth checks | Included in full nightly encrypted financial dumps; restore fixture compares grants, scheduled and rollback draft ancestry, cancellation, activation and audit rows alongside approved pricing | Drafts, approved versions, schedules, cancellations, activation markers and audit are retained at least 7 years. Audit actor UUIDs are pseudonymized before insertion; admin deletion removes the grant and pseudonymizes draft actor IDs while preserving pricing history. Audit UPDATE/DELETE/TRUNCATE is refused | [pricing-admin-workflow](../openspec/changes/pricing-admin-workflow/specs/pricing-admin-workflow/spec.md), [ADR 0011](adr/0011-owner-decisions-2026-11-09.md) |
| Browser session storage | Last known public pricing DTO with version/effective time and its ETag; exposure dedupe keys scoped to UUID/session/surface/version | Disposable cache; pricing recovered and revalidated from API; no backup | Session lifetime; offline prices explicitly labelled; no learner content, provider mappings or payment details. Clear cache and dedupe keys with the browser session; restore checks verify an empty session starts from the API. | [pricing-config](../openspec/changes/pricing-config/specs/pricing-config/spec.md), [analytics](../openspec/specs/analytics/spec.md) |
| Postgres (planned) | Courses, units, lesson stubs, enrollment and generated lesson metadata | Nightly dump plus object-key consistency checks | Course soft-delete, then hard-delete after 30 days | [course-creation](../openspec/specs/course-creation/spec.md) |
| Postgres (planned) | Lesson test attempts, answers, scores | Nightly dump | Life of account; delete within 30 days of account deletion | [lesson-test](../openspec/specs/lesson-test/spec.md) |
| Postgres (planned) | XP events, streaks, freezes, daily goals, league membership | Nightly dump; source for rebuilding Redis | XP events for account life; personal rows deleted within 30 days of account deletion | [gamification](../openspec/specs/gamification/spec.md) |
| Postgres (planned) | Friends, requests, blocks, friend streaks and nudges | Nightly dump | Remove relationships and end shared streak on account deletion; personal rows within 30 days | [friend-streaks](../openspec/specs/friend-streaks/spec.md) |
| Postgres (planned) | Tutor chat threads/messages; quick notes, enrichment/review state; bookmarks | Nightly dump | Per-item deletion where supported; account deletion within 30 days | [tutor-chat](../openspec/specs/tutor-chat/spec.md), [quick-notes](../openspec/specs/quick-notes/spec.md), [bookmarks](../openspec/specs/bookmarks/spec.md) |
| Redis | Disposable session/cache entries, rate limits, cached balances, leaderboards and streak counters | No backup; rebuild from Postgres | TTL or eviction; never sole copy of product data | [data-backup](../openspec/specs/data-backup/spec.md), [gamification](../openspec/specs/gamification/spec.md) |
| S3 application bucket | Motion HTML slides, manifests, lesson assets; optional golden-set mirror | Object versioning; tracked regeneration jobs; verify cloud capability before use | Match owning course/account deletion; object-version expiry policy must be specified before real data | [lesson-player](../openspec/specs/lesson-player/spec.md), [course-creation](../openspec/specs/course-creation/spec.md) |
| S3 separate backup bucket | age-encrypted custom Postgres dumps at `YYYY-MM-DD/schema-<Goose version>.dump.age`; includes learner, append-only ledger and migration/queue state | Worker at 02:00 UTC plus startup catch-up; public X25519 recipient only, conditional whole-object upload, no multipart remnants. Railway snapshots remain a separately provisioned second layer | General backups 14 days; financial backup policy 35 days per ADR 0011. API has no backup access; writer can read/put but cannot delete or administer buckets. Separate retention administrator owns expiry; cloud lifecycle automation remains a follow-up | [data-backup](../openspec/specs/data-backup/spec.md), [ADR 0011](adr/0011-owner-decisions-2026-11-09.md) |
| Worker temporary filesystem | Ciphertext-only private staging file (0600); no plaintext database archive | Disposable, never a recovery source; removed on success, dump failure and upload failure | One bounded job (30 minutes, at most 4 GiB encrypted output); container filesystem is ephemeral after abnormal process termination | [worker backup change](../openspec/changes/worker-database-backup/design.md) |
| Local RustFS IAM metadata | Separate application, backup writer and root/retention identities and policies; local fixture secrets are environment settings | Reprovisioned by the development-only initializer; production IAM/keys managed separately | Never pass root or backup credentials to API containers; private age recovery key is never in worker images or environment | [worker backup change](../openspec/changes/worker-database-backup/design.md) |
| Device OS credential store / browser session fallback | Access/refresh token pair in desktop OS keyring through fixed-service Tauri commands; web build only uses sessionStorage with an in-memory fallback. Analytics preference is a non-secret local boolean. Native mobile storage fails closed pending a supported implementation. | Credentials excluded from app backups; reissued by email-code sign-in | Logout revokes server session and deletes device credentials. Browser fallback ends with the browser session. No localStorage tokens; native keyring failures never fall back to browser storage. | [email-code-sign-in](../openspec/changes/email-code-sign-in/specs/accounts/spec.md), [ADR 0004](adr/0004-auth.md) |
| Device cache | Cached slide copies | Disposable cache; never the sole copy of lesson assets | Cache eviction/logout | [lesson-player](../openspec/specs/lesson-player/spec.md) |
| Hugging Face + CI artifacts | Golden-set revisions; eval reports | Versioned dataset; optional pinned mirror in S3; reports as CI artifacts | Keep dataset revisions; eval artifacts 90 days | [ai-reliability](../openspec/specs/ai-reliability/spec.md) |
| PostHog | Product events, backup/drill results, AI token/latency metadata | Derived store; never sole product-state copy | Project-plan event retention; request person deletion on account deletion; no note/chat/profile text | [analytics](../openspec/specs/analytics/spec.md) |

Postgres recovery objectives are RPO 24 hours and RTO 4 hours. Restoring a backup
into non-production requires prior anonymization. E1-07 implements the explicit
financial retention and anonymization design: on account deletion, `credit_entries.learner_id`
and `pricing_quotes.learner_id` are set to null while preserving a SHA-256 `owner_reference`
for lawful seven-year financial retention. Personal rows cascade-delete immediately, and
backed-up `account_deletion_tombstones` are replayed on restore before traffic is served.

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
