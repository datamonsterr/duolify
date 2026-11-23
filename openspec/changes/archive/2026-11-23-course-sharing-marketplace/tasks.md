## 1. E2E tests (write first, failing)

- [x] 1.1 Username set (valid, taken case-insensitively, reserved, too short), rename cooldown, old-name hold, search.
- [x] 1.2 Friend request by username: send, accept, decline, remove, block; privacy settings change who can find or request.
- [x] 1.3 Owner publishes `link`; visitor opens preview without account; deep link opens the course; revoke kills the link; rotate.
- [x] 1.4 Invite by username and by friend: notification, accept enrolls a Course copy, decline, expiry, blocked sender.
- [x] 1.5 Enrolled learner reuses published lessons at zero credits; reaching the owner's last lesson triggers on-demand generation charged to the learner at the Pricing config version in the quote.
- [x] 1.6 Republish creates version 2; enrolled learner keeps version 1, sees update, updates, progress maps by lesson key.
- [x] 1.7 Public publish with safe course goes live; unsafe course is blocked; borderline goes to moderation queue.
- [x] 1.8 Marketplace: browse, search, category, language, three sorts; course page with preview slides.
- [x] 1.9 Review blocked before N completed lessons, allowed after; report flow; auto-hide at 3 reporters; takedown (normal and severe).
- [x] 1.10 Owner deletes course: delisted, enrolled copies keep access; account deletion keeps copies, removes owner data.
- [x] 1.11 PostHog test double receives funnel events.

## 2. Integration tests

- [x] 2.1 Username uniqueness under concurrency (canonical unique index), NFKC and confusable cases.
- [x] 2.2 Snapshot builder allowlist; scanner rejects profile, notes, chat, bookmarks, progress, instructions, emails and keys.
- [x] 2.3 Snapshot immutability; asset reference counting and purge after grace.
- [x] 2.4 Share token stored hashed; revoked token returns 404/410; preview payload has no learner data.
- [x] 2.5 Credits reserve/settle for on-demand generation; insufficient credits blocks; Pricing config version recorded.
- [x] 2.6 Rate limits and abuse caps for each action in the design.
- [x] 2.7 Moderation roles and audit log; appeal once.
- [x] 2.8 Screening unavailable leaves publish `pending_screening`.
- [x] 2.9 Account deletion cascades per the backup/retention requirements.

## 3. Unit tests

- [x] 3.1 Username validator and canonicalizer.
- [x] 3.2 Visibility state machine.
- [x] 3.3 Ranking functions (popular, rating Bayesian).
- [x] 3.4 Review eligibility rule.
- [x] 3.5 Lesson-key mapping on update.
- [x] 3.6 Event emitters.

## 4. Implementation

- [x] 4.1 Migrations: usernames, history, friendships, requests, blocks, privacy, visibility, versions, links, invites, copies, reviews, reports, moderation, audit.
- [x] 4.2 API endpoints and workers (screening job, notifications).
- [x] 4.3 Proto additions in `proto/` with matching `services/api` and `engine` changes in one PR; Engine `ScreenCourse`, `ScreenReview`.
- [x] 4.4 Client screens (see `docs/design/screens.md`; new screens go through the app-design process).

## 5. Evals, analytics, backup

- [x] 5.1 Golden-set rows for `screen_course` and `screen_review` (safe, borderline educational, unsafe, hidden-payload, spam, multilingual) in the Hugging Face dataset; thresholds in `evals/thresholds.toml`; pass `mise run engine:eval`.
- [x] 5.2 Add events to the `analytics` catalog through its change-control rule.
- [x] 5.3 Add tables and object prefixes to `docs/business/data-inventory.md`; confirm dump and bucket versioning cover them; deletion tests.
- [x] 5.4 `.env.example`: app domain for links, push credentials names with comments.

## 9. Publisher birth date (#228)

- [x] 9.1 E2E first: desktop/mobile missing-date prompt, adult resumes and minor remains private.
- [x] 9.2 Integration second: authenticated setter, real-date/future/lower-bound validation, server age boundary, publish/republish/rotation gates, export/deletion and backup anonymization.
- [x] 9.3 Unit third: calendar age including leap birthdays and UTC, strict date parsing, typed client errors and accessible save/retry form.
- [x] 9.4 Add migration 20261116121503 with only nullable account DATE, wire the real age source and client continuation, update privacy policy/inventory/restore PII registry and synthetic fixtures without PII analytics.
- [x] 9.5 Run focused API/client tasks and ci:openspec; native Tauri checks remain CI-only on this host.
- [x] 9.6 Write-once follow-up: failing e2e minor overwrite/refusal, integration HTTP idempotence and concurrent database writes, then unit locked-form state; implement atomic NULL-only save and support-correction message, update spec/design/inventory and rerun focused checks.

## 6. Narration audio reuse (ADR 0013, #229)

- [x] 6.1 e2e: shared copy plays the owner's audio over HTTP, no Credit rows, foreign lesson and anonymous callers refused, blocked copy refused (`TestSharedCopyPlaysOwnerNarrationAudioOverHTTP`).
- [x] 6.2 Integration: snapshot refs and pins, free reuse and dedupe, missing-asset fallback, severe takedown and appeal lift, pin retention and purge.
- [x] 6.3 Unit: allowlist, `ReusableNarration`, config default and kill switch.
- [x] 6.4 Migration `20261116121502`, retention, takedown, privacy policy, `docs/data-inventory.md`.
