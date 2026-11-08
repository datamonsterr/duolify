## Why

Courses are expensive to generate and valuable to others. Learners want to send a course to a friend, post a link, or find a good course instead of generating one. Reusing a **Published course** removes the generation cost for the next learner. It also needs a social base: **Usernames** and a **Friend list** (E6), and a **Marketplace** that is safe, moderated and private by design.

## What Changes

- `social` (new): unique case-insensitive **Usernames** with rules, rename cooldown, reserved names and search; **Friend list** requests by username (send, accept, decline, remove), block, privacy settings.
- `course-sharing` (new): **Course visibility** `private | link | public`, versioned frozen snapshots on publish, republish, **Share link** (revocable, preview without account, deep link), **Course invite** to a username or friend, enrollment as a **Course copy** that reuses published lessons, on-demand generation for lessons the owner never generated (default payer: the enrolling learner), abuse limits, privacy, deletion.
- `marketplace` (new): browse, search, categories, language, sort (popular, new, rating), course page with preview slides, ratings and reviews after N completed lessons, report flow, moderation queue and takedown, AI safety screening before a course becomes public, creator attribution, enrollment counts. v1 courses are free.
- `friend-streaks` (delta): "Friend connections" now defers to `social` for requests and block.
- Paid courses and creator revenue share are recorded as future open questions; no payments are specified.

## Capabilities

### New Capabilities

- `social`: usernames, friend list, privacy settings.
- `course-sharing`: visibility, snapshots, share links, invites, Course copy, on-demand generation, abuse limits, deletion.
- `marketplace`: catalog, search, ratings and reviews, reports, moderation, safety screening.

### Modified Capabilities

- `friend-streaks`: friend connections refer to `social`.

## Impact

- Client: username onboarding and rename, friend list and requests, share sheet, invite flow, deep-link handler, public preview page, marketplace screens (browse, search, course page), reviews, report dialog.
- API: usernames, friendships, blocks, privacy, visibility, snapshot builder and versions, share link tokens, invites, Course copy and pinned version, on-demand generation charging, marketplace search and ranking, reviews, reports, moderation queue and roles, rate limits, notifications, analytics.
- Engine: `ScreenCourse` (safety screening), `ScreenReview` (optional, cheap), existing `GenerateLesson` for on-demand lessons. Proto additions are proposed in `design.md` only.
- Proto: additive RPCs; need matching `services/api` and `engine` changes.
- Infra: object storage layout for immutable snapshot assets with reference counting, public preview endpoint caching (CDN is an open question), search index in Postgres full-text (Redis cache).
- Evals: golden-set tasks `screen_course`, `screen_review`.
- ADRs relied on: [0002](../../../docs/adr/0002-service-architecture.md), [0003](../../../docs/adr/0003-data-stores.md), [0004](../../../docs/adr/0004-auth.md), [0006](../../../docs/adr/0006-llm-providers-and-evals.md), [0007](../../../docs/adr/0007-credits-and-payments.md), [0009](../../../docs/adr/0009-narration-elevenlabs.md).

## Dependencies and scope

- Needs E2 (courses), E6 (friends; this change specifies usernames and friend list in `social`), E12 (prices via `pricing-config`). Steering of a Course copy is specified in `course-steering`.
- Out of scope: paid courses, revenue share, payouts, tutor-chat sharing, group study, comments on lessons, creator analytics beyond aggregates, SEO of marketplace pages.
