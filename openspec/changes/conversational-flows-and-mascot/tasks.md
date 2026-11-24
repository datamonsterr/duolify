Sizes: S = under 1 day, M = 1-3 days, L = 3-5 days. Each group is one PR (or two where noted). Within each group, order is e2e test, integration test, unit test, then code.

## 0. Decisions and docs (S)

- [x] 0.1 Write ADR "Scripted guided chat and original mascot" recording the design.md decision table; update `CONTEXT.md` with **Guided chat**, **Script**, **Mascot**
- [x] 0.2 Amend `docs/design/brand.md` "Avoid mascots" sentence for the original mascot exception; link `docs/design/mascot/`
- [ ] 0.3 Owner trademark check and final mascot name; update assets if renamed

## 1. Script engine and widgets (L, two PRs: engine, then widgets)

- [x] 1.1 Playwright e2e: a fixture script answers via every widget, edits a bubble, resumes after reload (fixture entry beside `guided-chat.html`)
- [x] 1.2 Integration tests: draft save/restore, branch re-evaluation after edit, local-copy sync, offline queue
- [x] 1.3 Unit tests: script schema and CI lint (locale keys EN+AR, branch targets, validators vs API limits, secure steps non-persistent), expression language, validators
- [x] 1.4 Implement `features/guided-chat` engine (state machine, closed expression language, branching, edit semantics), reusing E13-03 thread/composer primitives
- [x] 1.5 Implement widgets: single, multi-select, time, date, stepper/slider, toggle, short text, long text, file/image, rating, confirm card, secure email/code/password
- [x] 1.6 API: `guided_chat_drafts` goose migration, `GET|PUT|DELETE /v1/me/guided-drafts/{script_id}` with ownership, size cap, `If-Match`; sqlc, tests; purge worker (30 days idle)
- [x] 1.7 Axe and keyboard tests per widget; `role="log"` announcement test; reduced-motion test

## 2. Migrate onboarding (M)

- [x] 2.1 e2e: onboarding via chat (keyboard-only and pointer), resume, skip, edit, offline; compare with old flow
- [x] 2.2 Script `onboarding.setup`, `profile.edit`, `goal.daily`, `reminder.edit`; flag `FEATURE_GUIDED_ONBOARDING`; 50/50 A/B by Learner id
- [x] 2.3 Remove `SetupChat` AI call path behind the flag, keep old form for rollback for one release

## 3. Migrate course creation (M)

- [x] 3.1 e2e: brief chat to confirmed revision, no preview/Credits side effects, mobile sheet
- [x] 3.2 Scripts `course.brief`, `course.level`, `course.steer`; outline proposals stay quoted; update brief draft rules

## 4. Free-text routing (L, two PRs: prefilter/FAQ, classifier)

- [ ] 4.1 Golden set: `guided_intent` cases on the Hugging Face dataset (five routes, EN/AR, destructive phrases, injection, PII); thresholds in `evals/thresholds.toml`
- [ ] 4.2 e2e: question mid-step answered from FAQ and the step re-asked; paid question shows quote; destructive card not confirmable by text
- [ ] 4.3 Integration: `POST /v1/me/guided-chat/route`, rate limits (10/min, 40/run), classifier timeout/fallback paths, platform cost cap
- [ ] 4.4 Implement prefilter, bundled FAQ (EN+AR), classifier call via the `prompt-skill-routing-and-guardrails` guardrail and router, quote card, fallback buttons
- [ ] 4.5 `mise run engine:eval` passes; promotion evidence per ADR 0019

## 5. Mascot component and states (M)

- [ ] 5.1 Visual regression: eight states at 32/64/160 px, light and dark, RTL; reduced-motion static poses
- [x] 5.2 Unit tests: state derivation from UI events, off-screen pause, size budget (`bundle-budget.json`, 30 KB)
- [x] 5.3 Implement `features/mascot` (parts, tokens, CSS/WAAPI animations) from `docs/design/mascot/`; chat avatar, empty states, celebrations, errors
- [x] 5.4 Player chrome slot for the corner companion; immersive hide rule; Settings toggle

## 6. Migrate sign-in (M)

- [x] 6.1 e2e: email/code chat with autofill attributes, wrong code, resend, throttle, offline, age gate; pre-auth memory-only
- [x] 6.2 Scripts `auth.signin` (the age gate is its second step, so no separate `auth.age`); keep return-path allowlist and credential storage rules

## 7. Migrate character flows (M)

- [ ] 7.1 Coordinate with the `custom-characters` rewrite; consume its selectors; e2e for `character.create` and `character.cast`
- [ ] 7.2 Scripts `character.create`, `character.cast` (chat front end only; pricing and screening unchanged)

## 8. Migrate remaining flows (L, two PRs)

- [x] 8.1b e2e per flow; scripts `course.share`, `course.invite`, `course.republish`, `course.review`, `report.content` (flag `duolify.guided.rest`; old forms stay until 8.3; `social.*` is 8.1a)
- [x] 8.2 Scripts `credits.confirm`, `account.export`, `account.delete` (destructive card, secure password)
- [ ] 8.3 Delete old forms and flags one release after each flow's rollout; update `docs/analytics/coverage.md`

## 9. Customisation (M)

- [x] 9.1 e2e: recolour, accessory, pick a Character, delete the Character, fallback to default
- [x] 9.2 API: `mascot` field on settings (validation against `RigPartSpec v1`, partial update), export, erasure, backup tests
- [x] 9.3 "Your guide" picker UI; shared `RigPartSpec v1` package consumed by `custom-characters`

## 10. Analytics (S)

- [x] 10.1 Catalogue and tests (`mascot_*` has no test yet, listed as a gap in the coverage table): all `guided_chat_*` and `mascot_*` events, content-free, opt-out, `entry_point` not `source`
- [x] 10.2 Funnel definition (queries documented, no dashboard built here): completion vs old form, time to complete, drop-off step, free-text rate; update `docs/analytics/coverage.md`

## 11. Backup and restore (S)

- [x] 11.1 Add `guided_chat_drafts` and `mascot` to dump coverage, export and erasure tests; restore drill covers expiry purge and tombstones; data inventory docs

## 12. Design review and accessibility (M)

- [ ] 12.1 Screenshots for every script and the mascot states at 390x844, 844x390 and 1280x800, light/dark, EN/AR, reduced motion; commit under `docs/design/reviews/`
- [ ] 12.2 Screen-reader pass (VoiceOver, TalkBack, NVDA) on onboarding, sign-in and delete account; keyboard-only pass; 200% text; no timing traps
