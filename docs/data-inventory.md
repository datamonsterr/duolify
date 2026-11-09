# Data inventory

Owner specs are authoritative for collection and deletion. Every feature adding
data must update this table and its restore checks. Bootstrap currently creates
only `learners`, `credit_entries`, Goose/River metadata and River's queue tables;
the other product records below are planned. Backup policies are requirements,
not a claim that scheduling has shipped. See [backup tooling](../infra/backup/README.md).

| Store | Data held | Backup method | Retention / deletion | Owner spec |
| --- | --- | --- | --- | --- |
| Postgres | `learners`, future auth/session and learner profile rows | Nightly encrypted dump to separate backup bucket; Railway volume snapshots | Personal data removed within 30 days of account deletion; backup copies roll off in 14 days | [learner-profile](../openspec/specs/learner-profile/spec.md), [data-backup](../openspec/specs/data-backup/spec.md) |
| Postgres | Append-only `credit_entries`; derived purchased/reward balances | Same dump; PITR where available; reconcile balances with sum of entries after restore | At least 7 years per credits spec; preserve financial records while removing/anonymizing personal fields | [credits](../openspec/specs/credits/spec.md) |
| Postgres | River jobs, queue/leader metadata, Goose/River migration versions | Included in full database dump; validate schema versions and retry safety on restore | River's configured cleanup policy; no learner payloads in the bootstrap dummy job; specify payload retention when adding jobs | [data-backup](../openspec/specs/data-backup/spec.md), [ADR 0003](adr/0003-data-stores.md) |
| Postgres (planned) | Courses, units, lesson stubs, enrollment and generated lesson metadata | Nightly dump plus object-key consistency checks | Course soft-delete, then hard-delete after 30 days | [course-creation](../openspec/specs/course-creation/spec.md) |
| Postgres (planned) | Lesson test attempts, answers, scores | Nightly dump | Life of account; delete within 30 days of account deletion | [lesson-test](../openspec/specs/lesson-test/spec.md) |
| Postgres (planned) | XP events, streaks, freezes, daily goals, league membership | Nightly dump; source for rebuilding Redis | XP events for account life; personal rows deleted within 30 days of account deletion | [gamification](../openspec/specs/gamification/spec.md) |
| Postgres (planned) | Friends, requests, blocks, friend streaks and nudges | Nightly dump | Remove relationships and end shared streak on account deletion; personal rows within 30 days | [friend-streaks](../openspec/specs/friend-streaks/spec.md) |
| Postgres (planned) | Tutor chat threads/messages; quick notes, enrichment/review state; bookmarks | Nightly dump | Per-item deletion where supported; account deletion within 30 days | [tutor-chat](../openspec/specs/tutor-chat/spec.md), [quick-notes](../openspec/specs/quick-notes/spec.md), [bookmarks](../openspec/specs/bookmarks/spec.md) |
| Redis | Disposable session/cache entries, rate limits, cached balances, leaderboards and streak counters | No backup; rebuild from Postgres | TTL or eviction; never sole copy of product data | [data-backup](../openspec/specs/data-backup/spec.md), [gamification](../openspec/specs/gamification/spec.md) |
| S3 application bucket | Motion HTML slides, manifests, lesson assets; optional golden-set mirror | Object versioning; tracked regeneration jobs; verify cloud capability before use | Match owning course/account deletion; object-version expiry policy must be specified before real data | [lesson-player](../openspec/specs/lesson-player/spec.md), [course-creation](../openspec/specs/course-creation/spec.md) |
| S3 separate backup bucket | Encrypted Postgres dumps named by date/schema version | Nightly worker upload; Railway volume snapshots are a second layer | 14 days; API has no backup delete rights; retention uses a separate identity | [data-backup](../openspec/specs/data-backup/spec.md) |
| Device cache / OS credential store | Cached slide copies; eventual login tokens | Disposable cache; tokens reissued via auth | Cache eviction/logout; never only copy of lesson assets | [lesson-player](../openspec/specs/lesson-player/spec.md), [ADR 0004](adr/0004-auth.md) |
| Hugging Face + CI artifacts | Golden-set revisions; eval reports | Versioned dataset; optional pinned mirror in S3; reports as CI artifacts | Keep dataset revisions; eval artifacts 90 days | [ai-reliability](../openspec/specs/ai-reliability/spec.md) |
| PostHog | Product events, backup/drill results, AI token/latency metadata | Derived store; never sole product-state copy | Project-plan event retention; request person deletion on account deletion; no note/chat/profile text | [analytics](../openspec/specs/analytics/spec.md) |

Postgres recovery objectives are RPO 24 hours and RTO 4 hours. Restoring a backup
into non-production requires prior anonymization. Financial retention and account
deletion need an explicit anonymization design when auth/credits features ship;
the current non-null ledger-to-learner foreign key does not implement it.

E13-01 client theme foundation: `docs/design/tokens.json` is the authoritative
light/dark token artifact; the client imports `docs/design/theme.css` directly.
Tokens, component sources and licensed bundled Nunito WOFF2 assets are Git-backed
and restored together from the same revision. Course responses, search input and
the optional E1-06 analytics context stay in memory and disappear when the app
closes; this slice adds no device persistence or learner-data deletion work.
Existing API course retention and backup policies still apply. UI analytics use
stable control IDs only and remain silent without an enabled UUID context.
