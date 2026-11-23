# Cloudflare offload implementation tickets

Write and run e2e, integration and then unit tests, in that order. Record each observed failure before implementation. Use the existing area suites and local Workers runtime. Use `apps/client/e2e/servers.ts` environment variables for ports 4190–4199 in any browser or API e2e that starts servers. All Cloudflare interactions in development are local or faked. Production cutover, resource creation and any spend are separate owner gates. Register new analytics through the existing allowlist and analytics catalogue, and record only the properties in the [offload telemetry contract](specs/analytics/spec.md) and [design](design.md#posthog-and-ai-reliability).

## CF-01 (W2) — AI Gateway in front of OpenRouter

**Labels** `area:engine`, `ready-for-agent`

**Blocked by** none

### Spec

- [AI reliability delta](specs/ai-reliability/spec.md) and [voice-input delta](specs/voice-input/spec.md).
- [ ] Add `OPENROUTER_BASE_URL` with the direct URL as default, separate authenticated Gateway header, per-request logging/cache guards and a bounded direct fallback. Cache only verified non-personal, identical public generation requests. Keep STT provider ZDR and data collection denial, and use direct STT until the Gateway transcription route can meet the contract.

### Tests (TDD order)

1. **E2E** `engine/crates/engine-server/tests/ai_gateway_e2e.rs` — boot the Engine with fake Gateway and OpenRouter servers, drive production RPCs for generation, a streamed turn and transcription, assert direct default, direct recovery before first delta, and no second upstream call after a delta.
2. **Integration** `engine/crates/engine-providers/tests/openrouter_routing.rs` and `engine/crates/engine-server/tests/transcribe.rs` — assert outbound path, auth header separation, timeout/error taxonomy, exact public cache opt-in, private cache skip, and identical ZDR payload across routes. Exercise a disabled or unsupported transcription route and the fail-closed privacy controls.
3. **Unit** `engine/crates/engine-providers/src/openrouter_gateway/tests.rs` — check URL joining, allowlist and cache key bounds, request classification, redirect handling, non-retryable errors, cancellation and attempt budget.

### Acceptance

- `mise run engine:lint`, `mise run engine:test`, `mise run engine:build`, `mise run engine:eval`, `mise run ci:openspec`.
- When updating the Go telemetry adapter or allowlist, also run `mise run api:lint`, `mise run api:test`, `mise run api:build`, `mise run api:test-integration` and `mise run api:test-e2e`.

### Analytics

Extend existing `$ai_generation` with `transport_route`, `gateway_fallback`, `gateway_cache_status` and `gateway_reason`. Keep model/provider usage attribution, exclude learner content, honor opt-out and report no false cache hits on direct routing.

### Data and backup

There are no new authoritative records. Disposable Gateway cache may hold eligible public results only. STT and private inputs remain transient and excluded from storage, logs, cache and backups.

### AI reliability

Add routing cases for an eligible public request, a private request, transcription with ZDR, and before-first-output vs mid-stream failure. Use fakes and offline Golden set cases, submit relevant rows for the pinned dataset, and keep the existing model fallback and live promotion gate. Gateway → direct OpenRouter is bounded and uses the same provider policy.

## CF-02 (W1) — Opt-in Workers AI TTS Narration provider

**Labels** `area:engine`, `area:api`, `ready-for-agent`

**Blocked by** none. Owner approval applies to a change of default or new spend.

### Spec

- [Narration delta](specs/narration/spec.md), [Lesson player delta](specs/lesson-player/spec.md), and [Pricing config delta](specs/pricing-config/spec.md).
- [ ] Add a Workers AI implementation behind the existing trait in `engine/crates/engine-core/src/narration`, with the vendor adapter in `engine/crates/engine-providers`. Expose `NARRATION_PROVIDER=workers_ai|elevenlabs` (default `elevenlabs`) and allowlist `WORKERS_AI_TTS_MODEL` for Aura-2 EN/ES and Aura-1. Implement language gating, audio-derived timing and Workers AI → budgeted ElevenLabs → captions. Retain ElevenLabs for curated Plus/Pro voices and pinned dialogue. Reject the retired MeloTTS model and preserve the recorded historical contract.
- [ ] Version the hash with actual provider and model, keep replay and legacy refs, reserve neurons in the shared budget, and cover fallback attempts and quote compatibility through the existing Pricing config cost-policy approval.

### Tests (TDD order)

1. **E2E** `services/api/test/e2e/workers_ai_narration_test.go` and `apps/client/e2e/narration-cf-02.spec.ts` — use synthetic fixtures to play estimated captions with seek/speed and offline cache, assert fallback playback or captions-only, provider-specific reuse, and no learner charge for undelivered audio.
2. **Integration** `engine/crates/engine-providers/tests/workers_ai_tts.rs`, `engine/crates/engine-server/tests/narration.rs`, `services/api/internal/narration/workers_ai_budget_integration_test.go`, and `services/api/internal/pricing/narration_provider_cost_integration_test.go` — fake authenticated binary MP3 and bad response, cover provider fallback, mixed language, concurrent UTC-day admissions, unknown outcomes, restore of both hash versions, and approved cost-only version activation.
3. **Unit** `engine/crates/engine-core/src/narration/tests.rs` and `services/api/internal/narration/workers_ai_budget_test.go` — check Unicode grapheme expansion, bounds/corruption, provider/model hash separation, language mapping, conservative rounding, permanent error classification and budget day rollover.

### Acceptance

- `mise run engine:lint`, `mise run engine:test`, `mise run engine:build`, `mise run engine:eval`.
- `mise run api:lint`, `mise run api:test`, `mise run api:build`, `mise run api:test-integration`, `mise run api:test-e2e`.
- `mise run client:lint-web`, `mise run client:test-web`, `mise run client:build`, `mise run client:e2e`, `mise run ci:openspec`. Run `mise run api:restore-drill-ci` when changing backed-up schema or object metadata.

### Analytics

Extend existing `narration_generated`, `narration_cache_hit`, `narration_failed`, `narration_budget_exhausted` and `$ai_generation` with applicable provider/model, timing source, fallback, neurons and bounded reason code. Preserve existing analytics and consent on playback and synthesis.

### Data and backup

Store provider/model, timing and audio references and per-day cost reservations in the existing authoritative records or covered Postgres additions. Restore both legacy and new keys with their reference counts and versioned audio/alignment objects. Include any new learner-linked records in backup and privacy fixtures. Retain auditable approved cost versions and unchanged learner rate.

### AI reliability

Add recorded `lesson_narration` cases to `evals/fixtures/golden.sample.jsonl` and `evals/fixtures/narration.workers-ai.json` for supported languages, unsupported language or retired model, corrupt MP3, Unicode timing, repeated requests, vendor outage and budget exhaustion. Submit cases for the pinned Hugging Face dataset and require applicable live promotion evidence before routing Learner traffic. Workers AI Free cap and existing ElevenLabs cap bound fallback spend.

## CF-03 (W3) — Edge area and public read Worker

**Labels** `area:infra`, `area:edge`, `ready-for-agent`

**Blocked by** none

### Spec

- [Course sharing delta](specs/course-sharing/spec.md), [Marketplace delta](specs/marketplace/spec.md), and [shared demo delta](specs/course-creation/spec.md).
- [ ] Create `edge/`, `edge/wrangler.jsonc`, `.mise/tasks/edge.toml`, and the path-filtered edge CI job in `.github/workflows/_checks.yml` and its callers. Use the Workers Vitest integration now published as `@cloudflare/vitest-plugin` (formerly `@cloudflare/vitest-pool-workers`). Set workflow concurrency with cancellation and a timeout for every added job. Keep the repository's platform and CI trigger policy.
- [ ] Serve the four anonymous public surfaces with the Hyperdrive/read-only Tunnel plan and Go-read-cache fallback. Gate Marketplace before reading cache, carry the Go contract through cache TTL and moderation, and exclude viewer-specific data. Provide the share authorization boundary CF-04 can use. Until Go verification is wired, keep share preview on the Go path for the release gate. Keep source-of-truth reads for token revocation and rotation.

### Tests (TDD order)

1. **E2E** `edge/test/e2e/public-reads.e2e.test.ts` — call local Worker and Go origin over HTTP using seeded published and private Courses; cover all four surfaces, flag-off with warm cache, blocked signed-in viewer, revoked link and moderation expiry in both data modes.
2. **Integration** `edge/test/integration/public-read-conformance.test.ts` — run Workers Vitest against migrated disposable Postgres and the exact role with both origin handlers and Worker. Compare payload/status, filters, pagination, visibility, authorization, cache age and read-only permissions. Force conformance or connection failure and select the Go read-cache mode.
3. **Unit** `edge/test/unit/public-read-policy.test.ts` — check normalized cache keys, TTL and age, disabled flag bypass, immutable demo version, no-store, and no cache for cookies, authenticated, revoked or non-success responses.

### Acceptance

- `mise run edge:lint`, `mise run edge:test`, `mise run edge:build`, `mise run infra:lint`, `mise run infra:test`, `mise run infra:build`, `mise run ci:lint`, `mise run ci:openspec`.
- If touching Go or adding the read role through API migrations, also run `mise run api:lint`, `mise run api:test`, `mise run api:build`, `mise run api:test-integration`, `mise run api:test-e2e` and the affected migration/restore checks.
- Save local conformance evidence and record the selected data path. Require a separate authorized Tunnel/TLS/Access check before public rollout.

### Analytics

`edge_public_read` with `surface`, `read_path`, `cache_status`, and `outcome` through the existing consent-aware wrapper. Anonymous-only diagnostics use aggregate counts without link or query values.

### Data and backup

Public projections and caches derive from existing backed-up Postgres rows and authored demo fixtures. Restore applied projections and read privileges from versioned migrations if introduced. Cache contents are disposable. Retain origin deletion and moderation authority.

### AI reliability

No AI. Keep existing publication screening eligibility and allowlisted public projections. Test against stored fixtures and the Go contract.

## CF-04 (W3) — Turnstile for email code, sign-up and share preview

**Labels** `area:api`, `area:client`, `ready-for-agent`

**Blocked by** none

### Spec

- [Accounts delta](specs/accounts/spec.md) and [share preview delta](specs/course-sharing/spec.md#requirement-turnstile-on-share-previews).
- [ ] Add the accessible widget to protected client flows, validate fresh proof in Go before side effects or preview disclosure, and retain existing authentication and rate limits. Supply the share-preview proof through the agreed request contract and expose a verification seam for CF-03. Use Cloudflare dummy pass/fail keys in test fixtures. Include desktop and mobile screenshots in the PR body and request owner UI approval before merge.

### Tests (TDD order)

1. **E2E** `apps/client/e2e/turnstile-cf-04.spec.ts` and `services/api/test/e2e/turnstile_test.go` — use the official pass/fail sitekeys with deterministic harness responses to drive email request, first verification and share preview. Assert retry and focus on failure, acceptance with a fresh proof and no auth or preview effects before Go verification.
2. **Integration** `services/api/internal/accounts/turnstile_integration_test.go` and `services/api/internal/sharing/turnstile_preview_integration_test.go` — fake Siteverify with documented dummy secret behavior, validate Go routes and prove unavailable, expired and mismatched verification prevents OTP, account and preview side effects. A warm preview still needs successful Go verification.
3. **Unit** `services/api/internal/accounts/turnstile_test.go` and `apps/client/src/features/accounts/turnstile.test.tsx` — check action/hostname, missing token, cancellation, bounded idempotent retry, reset and accessible retry.

### Acceptance

- `mise run api:lint`, `mise run api:test`, `mise run api:build`, `mise run api:test-integration`, `mise run api:test-e2e`.
- `mise run client:lint-web`, `mise run client:test-web`, `mise run client:build`, `mise run client:e2e`, `mise run ci:openspec`. Submit screenshots and owner approval with the controller's UI PR.

### Analytics

`turnstile_verification` with `surface`, `outcome` and closed `reason_code` where the wrapper has an eligible consented identity. Keep aggregate anonymous counters without IP addresses, tokens, email, codes or link values.

### Data and backup

There are no new authoritative records. Siteverify proof stays ephemeral, outside application storage, browser drafts, logs and backups. Keep the existing account backup and privacy rules.

### AI reliability

No AI. Use an injected Siteverify client and official public dummy fixtures. Transport failure blocks the protected action and permits a fresh retry.

## CF-05 (W4) — Durable Object chat and Go ledger integration

**Labels** `area:edge`, `area:api`, `ready-for-agent`

**Blocked by** CF-03

### Spec

- [Tutor chat delta](specs/tutor-chat/spec.md), [Guided chat delta](specs/guided-chat/spec.md), and [W4 design](design.md#w4-chat-sessions-at-the-edge).
- [ ] Add per-Learner-session SQLite-backed Durable Objects using the `agents` package, hibernating WebSocket, and SSE fallback. Gate upgrades and SSE before deriving object identity, use scoped service-to-service auth to Go, and delegate quote/reserve/settle/release and chat persistence to the existing Go API/ledger. Keep `CHAT_EDGE_ENABLED=false` by default and preserve the existing Engine route. Keep scripts, offline behavior and consent for optional paid Guided chat detours.

### Tests (TDD order)

1. **E2E** `edge/test/e2e/chat-session.e2e.test.ts` — connect to local Worker via WebSocket and SSE, drive mock Engine Tutor chat and network-backed Guided chat through Go, stop/reconnect with the same turn, and prove one quote/settlement and a correct restored thread across both transports.
2. **Integration** `edge/test/integration/chat-agent.test.ts` and `services/api/internal/chat/edge_turn_integration_test.go` — Workers Vitest and real test Postgres verify server flag, authorized claims, revocation, hibernation rehydration, replay without duplicate reserve/settle, partial-stream semantics and restore/erase. Verify a request from the wrong Learner or without service credentials cannot start a turn or read history.
3. **Unit** `edge/test/unit/chat-protocol.test.ts` and `services/api/internal/chat/edge_turn_test.go` — validate credential expiry, turn identity and scope, frame/heartbeat/cancel behavior, privacy exclusions and bounded fallback classification.

### Acceptance

- `mise run edge:lint`, `mise run edge:test`, `mise run edge:build`, `mise run api:lint`, `mise run api:test`, `mise run api:build`, `mise run api:test-integration`, `mise run api:test-e2e`, `mise run ci:openspec`.
- Run `mise run engine:eval` for rerouted Tutor and Guided chat cases and `mise run api:restore-drill-ci` when changing backed-up records. If touching Engine also run `mise run engine:lint`, `mise run engine:test`, `mise run engine:build`.

### Analytics

Add `chat_transport` to applicable existing terminal events. Canonical `$ai_generation` and credit events retain actual attempts and one settlement across transports. Exclude transcripts, credential or frame content.

### Data and backup

Postgres owns chat history, drafts, turn recovery and the financial ledger. Existing encrypted backup, export and erasure cover them. Add fixture and privacy coverage for any new learner-linked record. Keep transient DO connection metadata disposable and verify recovery after object loss or Postgres restore.

### AI reliability

Reuse bounded Engine paths and Golden set thresholds. Add synthetic Tutor/Guided chat transport cases for grounding, consent, partial-output disconnect, rejection, deterministic classifier fallback and idempotent recovery. Submit applicable cases for the pinned dataset. Preserve the Engine path and all existing privacy and promotion gates.

## CF-06 (W4) — Client chat transport and fallback

**Labels** `area:client`, `ready-for-agent`

**Blocked by** CF-05

### Spec

- [Tutor chat streaming](specs/tutor-chat/spec.md#requirement-streaming-replies), [Tutor chat recovery](specs/tutor-chat/spec.md#requirement-edge-chat-authority-and-recovery), and [Guided chat edge transport](specs/guided-chat/spec.md).
- [ ] Add WebSocket transport behind the server flag, Edge SSE when upgrade/connection fails, and the existing Go/Engine fallback. Preserve one turn identity, consent, stop, draft recovery, offline behavior and accessible announcements. Include desktop and mobile screenshots of the connected and fallback UI in the PR body and request owner UI approval before merge.

### Tests (TDD order)

1. **E2E** `apps/client/e2e/chat-cf-06.spec.ts` — exercise working WebSocket and deliberately rejected upgrade with working SSE, then a fresh turn on the existing Engine path when Edge is unavailable. Verify Tutor and Guided chat display, quote consent, stop, persisted partial reply and no duplicate settlement with one turn id.
2. **Integration** `apps/client/src/features/chat/chat-transport.integration.test.tsx` — exercise Tutor and Guided chat surfaces with HTTP and WebSocket fixtures, same-turn reconnect, feature flag false, draft and focus continuity, accessible completion and fallback telemetry.
3. **Unit** `apps/client/src/features/chat/chat-transport.test.ts` — exercise frame sequencing, deduplication, cursor reuse, no restart after a first delta, close/abort handling and privacy-safe errors.

### Acceptance

- `mise run client:lint-web`, `mise run client:test-web`, `mise run client:build`, `mise run client:e2e`, `mise run ci:openspec`. Submit screenshots and owner approval with the controller's UI PR.

### Analytics

`chat_transport_fallback` with `surface`, `from_transport`, `to_transport` and fixed `reason_code`, once per transition. Use the existing consent-aware client wrapper and catalogue, and omit text, session/turn credentials and diagnostics.

### Data and backup

There are no new authoritative client records. Keep existing device draft and sign-out clear behavior. Render canonical chat history from Go/Postgres through the selected transport.

### AI reliability

No new model or prompt. Run browser fixtures with existing Tutor and Guided scenarios and verify existing deterministic and Engine fallback behavior when Edge transport fails.
