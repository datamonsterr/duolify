# Data inventory

Owner specs are authoritative for collection and deletion. Every feature adding
data must update this table and its restore checks. Bootstrap currently creates
only `learners`, `credit_entries`, Goose/River metadata and River's queue tables;
the other product records below are planned. E11-01 implements the local nightly
worker dump/upload and synthetic recovery proof; cloud keys, lifecycle policies
and production drills still require provisioning. See [backup tooling](../infra/backup/README.md).

| Store | Data held | Backup method | Retention / deletion | Owner spec |
| --- | --- | --- | --- | --- |
| Postgres | `learners`, future auth/session and learner profile rows | Nightly encrypted dump to separate backup bucket; Railway volume snapshots | Personal data removed within 30 days of account deletion; backup copies roll off in 14 days | [learner-profile](../openspec/specs/learner-profile/spec.md), [data-backup](../openspec/specs/data-backup/spec.md) |
| Postgres | Append-only `credit_entries`; derived purchased/reward balances | Same dump; PITR where available; reconcile balances with sum of entries after restore | At least 7 years per credits spec; preserve financial records while removing/anonymizing personal fields | [credits](../openspec/specs/credits/spec.md) |
| Postgres | River jobs, queue/leader metadata, Goose/River migration versions; `database_backup` args contain only a UTC date | Included in full database dump; validate schema versions and retry safety on restore; backup date survives retries across midnight | River cleanup policy; no learner content or credentials in backup/dummy jobs. Backup jobs use three attempts and a separate bounded queue | [data-backup](../openspec/specs/data-backup/spec.md), [ADR 0003](adr/0003-data-stores.md) |
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
| Device cache / OS credential store | Cached slide copies; eventual login tokens | Disposable cache; tokens reissued via auth | Cache eviction/logout; never only copy of lesson assets | [lesson-player](../openspec/specs/lesson-player/spec.md), [ADR 0004](adr/0004-auth.md) |
| Hugging Face + CI artifacts | Golden-set revisions; eval reports | Versioned dataset; optional pinned mirror in S3; reports as CI artifacts | Keep dataset revisions; eval artifacts 90 days | [ai-reliability](../openspec/specs/ai-reliability/spec.md) |
| PostHog | Product events, backup/drill results, AI token/latency metadata | Derived store; never sole product-state copy | Project-plan event retention; request person deletion on account deletion; no note/chat/profile text | [analytics](../openspec/specs/analytics/spec.md) |

Postgres recovery objectives are RPO 24 hours and RTO 4 hours. Restoring a backup
into non-production requires prior anonymization. Financial retention and account
deletion need an explicit anonymization design when auth/credits features ship;
the current non-null ledger-to-learner foreign key does not implement it.

E11-01 recovery checks decrypt an actual worker-image backup into a different
synthetic database, check Goose/River metadata, exact learner/ledger counts and
balance (120 - 35 = 85), and reject ledger UPDATE/DELETE/TRUNCATE after restore.
The suite also retries interrupted uploads, verifies one complete object and
checks that writer and API identities receive AccessDenied on backup deletion.
No production data is restored to local development by these tests.
