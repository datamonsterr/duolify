## 1. E2E tests (write first, failing)

- [ ] 1.1 Username set (valid, taken case-insensitively, reserved, too short), rename cooldown, old-name hold, search.
- [ ] 1.2 Friend request by username: send, accept, decline, remove, block; privacy settings change who can find or request.
- [ ] 1.3 Owner publishes `link`; visitor opens preview without account; deep link opens the course; revoke kills the link; rotate.
- [ ] 1.4 Invite by username and by friend: notification, accept enrolls a Course copy, decline, expiry, blocked sender.
- [ ] 1.5 Enrolled learner reuses published lessons at zero credits; reaching the owner's last lesson triggers on-demand generation charged to the learner at the Pricing config version in the quote.
- [ ] 1.6 Republish creates version 2; enrolled learner keeps version 1, sees update, updates, progress maps by lesson key.
- [ ] 1.7 Public publish with safe course goes live; unsafe course is blocked; borderline goes to moderation queue.
- [ ] 1.8 Marketplace: browse, search, category, language, three sorts; course page with preview slides.
- [ ] 1.9 Review blocked before N completed lessons, allowed after; report flow; auto-hide at 3 reporters; takedown (normal and severe).
- [ ] 1.10 Owner deletes course: delisted, enrolled copies keep access; account deletion keeps copies, removes owner data.
- [ ] 1.11 PostHog test double receives funnel events.

## 2. Integration tests

- [ ] 2.1 Username uniqueness under concurrency (canonical unique index), NFKC and confusable cases.
- [ ] 2.2 Snapshot builder allowlist; scanner rejects profile, notes, chat, bookmarks, progress, instructions, emails and keys.
- [ ] 2.3 Snapshot immutability; asset reference counting and purge after grace.
- [ ] 2.4 Share token stored hashed; revoked token returns 404/410; preview payload has no learner data.
- [ ] 2.5 Credits reserve/settle for on-demand generation; insufficient credits blocks; Pricing config version recorded.
- [ ] 2.6 Rate limits and abuse caps for each action in the design.
- [ ] 2.7 Moderation roles and audit log; appeal once.
- [ ] 2.8 Screening unavailable leaves publish `pending_screening`.
- [ ] 2.9 Account deletion cascades per the backup/retention requirements.

## 3. Unit tests

- [ ] 3.1 Username validator and canonicalizer.
- [ ] 3.2 Visibility state machine.
- [ ] 3.3 Ranking functions (popular, rating Bayesian).
- [ ] 3.4 Review eligibility rule.
- [ ] 3.5 Lesson-key mapping on update.
- [ ] 3.6 Event emitters.

## 4. Implementation

- [ ] 4.1 Migrations: usernames, history, friendships, requests, blocks, privacy, visibility, versions, links, invites, copies, reviews, reports, moderation, audit.
- [ ] 4.2 API endpoints and workers (screening job, notifications).
- [ ] 4.3 Proto additions in `proto/` with matching `services/api` and `engine` changes in one PR; Engine `ScreenCourse`, `ScreenReview`.
- [ ] 4.4 Client screens (see `docs/design/screens.md`; new screens go through the app-design process).

## 5. Evals, analytics, backup

- [ ] 5.1 Golden-set rows for `screen_course` and `screen_review` (safe, borderline educational, unsafe, hidden-payload, spam, multilingual) in the Hugging Face dataset; thresholds in `evals/thresholds.toml`; pass `mise run engine:eval`.
- [ ] 5.2 Add events to the `analytics` catalog through its change-control rule.
- [ ] 5.3 Add tables and object prefixes to `docs/business/data-inventory.md`; confirm dump and bucket versioning cover them; deletion tests.
- [ ] 5.4 `.env.example`: app domain for links, push credentials names with comments.
