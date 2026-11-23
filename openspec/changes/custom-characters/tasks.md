Sections 1-6 track E17-01..09 (v1 rig), largely delivered and verified against code in the 2026-11-23 audit. Sections 7-15 plan the v2 revision (owner request 5, 2026-11-23: selector/prompt creation of humans, animals, robots and fantasy creatures for use in Slides). This change delivers spec work only; later PRs implement. For each slice, write failing e2e tests, then integration tests, then unit tests, then implement until they pass. Check current official library/provider docs before implementation. Any future proto additions require matching API and Engine changes in the same implementation PR; none are made here.

## 1. Prompt creation, regeneration and Credits

- [x] 1.1 E2E first: allowed prompt produces a private previewable Character; regeneration changes only future uses; retries produce one Character and one charge. _Done: apps/client/e2e/characters-e17-01.spec.ts._
- [x] 1.2 E2E: unapproved pricing key, insufficient Credits, unsafe/IP/real-person/sexual prompts, quota exhaustion and provider failure leave no new Character/spend and offer the Default cast appropriately. _Done: characters-e17-02-pricing.spec.ts, characters-e17-04.spec.ts._
- [x] 1.3 Integration (verified in code 2026-11-23: 20 edits/hour in `library.go` beginAttempt, 2 creates/minute and 5/day in `creation.go` QuoteCreate, both tested in `TestPrivateCharactersVersionsQuotasAndCast` and the quoted-create integration test; the windowed 429s now carry `Retry-After`, the count cap does not): ownership, atomic count/rate limits, idempotent jobs, immutable versions, reservation/settlement/release, pricing flip during a job and failure preserving the prior version.
- [x] 1.4 Integration: input/output screening fails closed; raw job prompts expire within 24 hours; model retries/fallback stay within time, token and provider-cost ceilings. _Done: create_and_regenerate_a_quoted_private_character_integration_test.go._
- [x] 1.5 Unit: closed CharacterSpec schema, required fields, bounds, enums, cross-field checks, version compatibility and deterministic rejection reason codes. _Done: schema/v1.json, create_and_regenerate_a_quoted_private_character_test.go._
- [x] 1.6 Unit: persona serialization/delimiter escaping, instruction hierarchy and no provider IDs, markup or URLs in specs; success/failure analytics emitters contain no content. _Done: create_and_regenerate_a_quoted_private_character_test.go._
- [ ] 1.7 Obtain owner approval of `character_create` price, daily-credit eligibility and provider-cost cap in a new Pricing config version; add supported-key validation without altering approved E12 values; keep paid operations disabled until approved.
- [x] 1.7a E17-02 price decision (ADR 0013): separate `usd-character-create-2026-11-16` fixture sets 25 Credits, Daily-free ineligible; owner-review/activation/runtime gates, config-owned quotes/reservations, rollback and approved count/rate/prompt bounds have browser, integration and unit coverage. Existing provider envelopes remain bounded; no production activation or final default-cast art approval is implied.
- [x] 1.7b Chain the Character Pricing snapshot after `usd-design-chat-owner-approved-2026-11-16`: preserve all predecessor actions, limits, eligibility and settlement policy, then add only Character creation. Regression coverage checks the complete snapshot, effective-time order and the production design-chat owner gate; browser acceptance approves/activates both successors in order.
- [x] 1.8 Implement the canonical JSON Schema, Engine mapping/screening and bounded provider routing, with mock fixtures; add any required paired API/Engine contract changes only in that implementation PR. _Done: creation.go, schema/v1.json, engine character_create/character_screen tasks._
- [x] 1.9 Implement Postgres stores, private API operations and idempotent worker/credit flow; document raw prompt/verdict retention and account deletion. _Done: creation.go, library.go, character migrations._
- [x] 1.10 Implement creator/regeneration UI with structured preview, authoritative quote/confirmation, pending/rejected/failure/limit states and Default cast option; pass the slice's tests. _Done: features/characters/CreateCharacter.tsx + create_and_regenerate_a_quoted_private_character.test.tsx._

## 2. Original rig, free manual editing and Course Casts

- [x] 2.1 E2E first: manual edit changes info/appearance/personality/voice for free; choose guide and supporting Characters; a Course without selection uses original built-ins. _Done: characters-e17-03.spec.ts._
- [x] 2.2 E2E: other Learners' private Characters cannot be read/edited/selected; unsafe manual edits fail closed; deletion removes future selections without rewriting past content. _Done: characters-e17-03.spec.ts._
- [x] 2.3 Integration: Cast ownership/count checks, atomic version increments, job-start snapshots, edits during running jobs and stable retries; default resolution for deleted/unavailable selections. _Done: edit_private_characters_and_choose_a_course_cast_integration_test.go._
- [x] 2.4 Integration: manual screening is platform-paid with no `character_create` spend; library deletion purges unused versions and retains only required immutable snapshots. _Done: edit_private_characters_and_choose_a_course_cast_integration_test.go._
- [x] 2.5 Unit: trusted rig compilation, five expressions, part/contrast compatibility, no arbitrary SVG/script/network data, voice-key allowlist resolution and Cast limit tables. _Done: rig.go, edit_private_characters_and_choose_a_course_cast_test.go._
- [x] 2.6 Unit: selection/edit/delete event emitters, field-name-only analytics and free-operation ledger invariants. _Done: edit_private_characters_and_choose_a_course_cast_test.go, library.go._
- [ ] 2.7 Finalize three original built-in Characters, names/art, rig primitives and age/accessibility review with design; reject any resemblance/reference to third-party game characters.
- [x] 2.8 Implement versioned SVG rig/Default cast bundles and approved voice mappings; preserve old readers/assets and supply offline defaults. _Done: rig.go, features/characters/rig.ts; built-ins are PLACEHOLDERS pending 2.7/#40._
- [x] 2.9 Implement free structured editor, private library and Course Cast picker with limits, validation/screening feedback, deletion behavior and analytics; pass the slice's tests. _Done: CharacterLibrary.tsx, CourseCast.tsx + edit_private_characters_and_choose_a_course_cast.test.tsx._

## 3. Lesson generation, player, narration and test reactions

- [x] 3.1 E2E first: chosen Cast narrates and speaks dialogue consistently across Slides; all expressions, pause/seek/replay/speed, reduced motion and offline replay work. _Done: characters-e17-05.spec.ts, characters-e17-08.spec.ts._
- [x] 3.2 E2E: voice-aware cached audio costs zero; removed voices use safe mappings; TTS failure gives captions; missing rigs use defaults and never block a test.
- [x] 3.3 E2E: answer reactions follow authoritative grading, wrong answers still receive corrections/Retry variants, and only all-correct completion unlocks the next Lesson once. _Done: characters-e17-06.spec.ts._
- [x] 3.4 Integration: Course preview/lesson Cast input, instruction coexistence, job/Retry variant snapshot reuse, valid speaker keys, immutable past Lessons and unchanged Gated generation. _Done: pin_course_casts_through_generated_lesson_playback_integration_test.go._
- [x] 3.5 Integration: multi-speaker voice resolution, segment order/alignment, synthesis deduplication and reservations/caps, captions fallback and no Character creation charge during use.
- [x] 3.6 Unit: expression timeline/reset behavior, allowed speaker references, safe sandbox rendering, reaction-result mapping and voice-aware cache key composition. _Done: play_a_default_cast_motion_html_tracer.test.tsx, narrate_pinned_speakers_*_test.go._
- [x] 3.7 Unit: grading/answer-privacy invariants under adversarial persona content; render, generation, reaction and voice-fallback analytics emitters. _Done: react_to_authoritative_lesson_answers_without_changing_grading_test.go._
- [x] 3.8 Implement Engine Cast context/speaker validation and manifest/snapshot persistence with paired API/Engine transport changes if needed, preserving Course steering and generation contracts. _Done: pinning.go, manifest.go, proto CharacterSegment._
- [x] 3.9 Implement player rig/dialogue rendering, preloading/offline bundles and accessible static fallback without relaxing CSP/sandbox. _Done: features/characters/motion-slide.ts, playback.ts._
- [x] 3.10 Implement allowlisted per-speaker ElevenLabs synthesis under ADR 0009 with existing pricing, cache and alignment rules; confirm current provider docs and voice rights.
- [x] 3.11 Implement deterministic presentation-only test reactions, approved supportive copy and key PostHog events; pass the slice's tests. _Done: reaction.go, CharacterReaction.tsx, e17-06._

## 4. Tutor chat personas

- [x] 4.1 E2E first: main guide is default, supporting tutor selection works, Lesson versus Course context resolves the right version, and turn changes never rewrite history.
- [x] 4.2 E2E: injected persona cannot reveal prompts, change facts/credit rules or bypass safety; unavailable persona uses the Default cast before streaming; mid-stream error ends once.
- [x] 4.3 Integration: turn-start version snapshots, ownership checks, bounded context assembly, first-token/idle deadlines, provider fallback, token billing and thread deletion.
- [x] 4.4 Unit: escaped data-only persona builder, precedence/grounding rules, turn selection/default resolution and private analytics properties.
- [x] 4.5 Implement per-turn persona/reference persistence and API/Engine context input with existing streaming safety, fallback and spend ceilings.
- [x] 4.6 Implement tutor identity/selector, default/failure UI and selected/fallback PostHog events; pass the slice's tests.

## 5. Published Character snapshots and privacy

- [x] 5.1 E2E first: explicit export disclosure/confirmation precedes publish; enrolled copies retain frozen Characters after library edits/deletion and reuse assets without Character creation Credits. _Done: characters-e17-09.spec.ts._
- [x] 5.2 E2E: on-demand generation uses the published Cast; overrides are refused before the existing confirmed steering fork; forked copies can select future Casts without altering old Lessons. _Done: characters-e17-09.spec.ts._
- [x] 5.3 E2E: republish/update pinning, owner account deletion, unpublish and safety takedown preserve E15 semantics and never expose prompts/private library records. _Done: characters-e17-09.spec.ts._
- [x] 5.4 Integration: content-only allowlist scanner, current screening/fail-closed publication, hash coverage, independent snapshot payloads, reference counts, last-reference purge and suppression state. _Done: publish_immutable_character_snapshots_with_safe_lifecycle_integration_test.go._
- [x] 5.5 Unit: snapshot projection/diff/hash, removal of private fields and library identifiers, published-Cast selection guards and publication/fallback analytics. _Done: publish_immutable_character_snapshots_with_safe_lifecycle_test.go._
- [ ] 5.6 Implement E15 export allowlist extensions, immutable Cast/spec/assets and safe-copy generation, with current voice redistribution permission confirmed before enabling.
- [x] 5.7 Implement Character export disclosure/confirmation, immutable reuse/update/deletion and safe takedown fallback; emit `character_snapshot_published`; pass the slice's tests. _Done: publication.go, CharacterExportConfirmation.tsx._

## 6. Backup, analytics privacy and model reliability gates

- [ ] 6.1 (open: no e2e funnel or opt-out assertion found in `characters-e17-*.spec.ts`; only unit-level emitters) E2E first: create/select/publish/chat funnels emit the cataloged PostHog events once, opt-out stops them, and no prompt/persona/dialogue/answer content is captured.
- [x] 6.2 E2E: restore a database plus asset backup and play a private/published Lesson with the original Cast, voices and reactions without paid regeneration or exposing deleted/taken-down content. _Done: characters-e17-09.spec.ts, services/api/internal/backuptest/characters.go._
- [x] 6.3 Integration: nightly dumps include every Character/Cast/chat/publication record; protected S3 copies and version/checksum manifests resolve; deletion/takedown replay and shared-object retention are enforced. _Done: backuptest/characters.go._
- [ ] 6.4 (open: verify outage cases for screening, timeout, cost exhaustion and PostHog outage are asserted end to end) Integration: model/screening outages, schema-invalid output, timeout/cost exhaustion and PostHog outage follow fail-closed/default/release rules without blocking valid base learning.
- [ ] 6.5 Unit: event schema/no-content guards, idempotent emitter keys, restore reference checks and critical Golden set gate/threshold handling.
- [ ] 6.6 Add Hugging Face Golden set rows for `character_spec`, `character_screen`, `character_persona_lesson`, `character_persona_chat`, `character_injection`, `character_reactions`, `character_snapshot`; pin the revision and mirror it per existing policy.
- [x] 6.7 Add mock fixtures and explicit per-task thresholds in `evals/thresholds.toml` for validity, adherence, screening, injection, grading and snapshot/privacy immutability; critical leaks/unsafe outputs/grading changes must fail the gate. _Done: evals/thresholds.toml (character_* tasks) + evals/fixtures/golden.sample.jsonl; HF pin stays under 6.6._
- [x] 6.8 Implement/verify privacy-safe PostHog emitters and actual per-attempt `$ai_generation` usage; keep all names/properties in this change's analytics catalog, with existing delivery and person-deletion rules. _Done: features/characters/analytics.ts, library.go, creation.go._
- [x] 6.9 Update `docs/data-inventory.md` with tables, S3 objects, prompt/verdict/financial retention and deletion rules; extend nightly backup/restore checks for protected asset copies and old rig versions. _Done: docs/data-inventory.md, backuptest/characters.go._
- [ ] 6.10 Run only focused area mise tests for each implementation slice, `mise run engine:eval`, and the existing restore drill; verify mock and ready-CI live-sample modes under their budgets.
- [ ] 6.11 Gate rollout on approved pricing, original-art/age/voice review, passing evals and restore checks; verify rollback disables creation while preserving versions/assets and safe defaults.
- [ ] 6.12 Validate the completed change with `mise exec -- openspec validate --all --strict` before implementation handoff.

## 7. Attribute catalog and schema v2 (not blocked; M)

- [x] 7.1 _Done: schema/v2.json, spec_v2.go, schema-v2.ts, 46 shared cases in schema/v2.fixtures.json run by Go and Vitest; the Rust validator joins in group 9._ Unit first: closed `schema/v2.json` (discriminated by `body.kind`), versioned attribute catalog, cross-field rules (kind-compatible parts, age-band clothing minimums, contrast, size caps), reason codes; table-driven valid/invalid fixtures shared by Go, TypeScript and Rust.
- [x] 7.2 _Done: upgrade.go, upgrade.ts, schema/v1-to-v2.golden.json; all 297 v1 shape x eye x mouth x accessory combinations upgrade to valid v2._ Unit first: pure v1 to v2 upgrader with golden pairs for every v1 body shape, eye, mouth and accessory; unknown future `schema_version` or catalog id degrades to a safe default render and refuses writes.
- [ ] 7.3 (read half done: ReadSpec resolves v1 and degrades unknown versions; the write half needs the v2 compiler from 8.2) Integration: stored v1 versions stay immutable and readable; the first edit of a v1 Character writes a v2 version; compare-and-swap, quotas and screening unchanged.
- [ ] 7.4 (API and client done, nothing wired to a route yet; Engine validator ships with group 9, which already edits the engine) Implement validators in API, Engine and client from the one schema file; no behavior change behind the flag.

## 8. Rig v2: avatar, full body, poses (size L; final art blocked on #40)

- [x] 8.1 _Done: rigv2_test.go + rig-v2.test.ts; 252 shared SHA-256 goldens (rig/golden.v2.json) prove Go and TypeScript are byte-identical; every catalog id renders within budget, XML well-formed, no forbidden elements; alt text parity._ Unit first: deterministic compile (byte-identical SVG per document, rig version, view, pose, expression), render budget (avatar at most 8 KiB, full body at most 24 KiB, no external refs, filters, scripts or SMIL), alt text generated from attributes.
- [x] 8.2 _Done: rig/parts.v2.json (declarative part library interpreted by rigv2.go and rig-v2.ts), 4 kinds, 7 poses, 7 expressions, avatar and body views, bundled into the client build; PLACEHOLDER art pending 8.3._ Implement the compiler and part library with PLACEHOLDER parts (unblocked), including avatar crop, 7 poses and 7 expressions, in one shared TypeScript/Go-testable module; bundle for offline.
- [ ] 8.3 BLOCKED on #40: owner approval of the original v2 part library art (all kinds, age bands, head coverings) and the three default-cast Characters; replace placeholders, run accessibility and no-likeness review.

## 9. Engine prompt to attributes mapping (not blocked; M)

- [ ] 9.1 Golden cases first (see 14); then Engine structured-output mapping that returns only catalog ids and colours, never inferring attributes from names or nationalities; at most one repair or one fallback model.
- [ ] 9.2 Paired additive proto change (API and Engine in one PR): `pose` on `CharacterSegment`, v2 spec transport; `buf breaking` clean.
- [ ] 9.3 Integration: prompt path returns a draft the Learner edits; credit reserve/settle/release unchanged (25 Credits, ADR 0013); on failure open the free selector pre-filled with any validated partial attributes.

## 10. Selector UI and live preview (L; unblocked with placeholder art, ship flag off until 8.3)

- [ ] 10.1 E2E first: build a human, an animal, a robot and a fantasy creature from selectors only with zero Credits; prompt path then tweak; both paths produce one document shape; avatar and full-body previews update live; keyboard and screen reader complete the flow.
- [ ] 10.2 Component tests for each selector widget (kind and base, age band, skin and colour pickers, face, hair and head covering, eyes and eyebrows, facial hair, glasses, body type, clothes, accessories, name, pronouns, voice, personality); reduced motion; mobile and desktop layout; draft preserved on error.
- [x] 10.3 Export `CharacterSelector` and `CharacterPreview` for reuse by the guided chat in `conversational-flows-and-mascot`; document the draft-patch contract (see design, Mascot contract).

## 11. Safety extensions (M; guardrail wiring depends on the parallel change landing)

- [ ] 11.1 Golden refusal cases first (celebrity, brand mascot, copyrighted character, hateful caricature, sexual content, minors with romantic or suggestive content, injection).
- [ ] 11.2 Wire the cheap prompt guardrail from `prompt-skill-routing-and-guardrails` ahead of `character_screen` once merged; until then `character_screen` alone, fail closed. Screen names and pronoun/label text.
- [ ] 11.3 Make published Character snapshots reportable under `report-targets`; takedown suppression overlay reused.

## 12. Cast references in Slides, chat and lists (M)

- [ ] 12.1 E2E first: a generated Lesson shows two Characters with different poses and expressions per segment; missing or taken-down member renders the default; offline replay.
- [ ] 12.2 Implement pose in manifest and player; avatars in Tutor chat header, Cast list and library; v1 segments without pose derive a pose from the expression.

## 13. Mascot contract (S; after `conversational-flows-and-mascot` specs settle)

- [ ] 13.1 Contract tests: profile mascot resolves through the same resolver, falls back to the built-in default mascot when deleted, suppressed or invalid; mascot states map to pose and expression; no persona text reaches the mascot slot.

## 14. Data, limits, analytics, evals (M)

- [ ] 14.1 Integration: limits table from the design; selector creates count against 2 creates/minute, the 20 writes/hour bucket and the 10-Character cap but never spend Credits; account export includes Character documents (no prompts); erasure and restore drill render every restored document to its recorded checksum.
- [ ] 14.2 Unit: new event emitters and `fields_changed` v2 allowlist contain no values; metrics defined in the design are queryable.
- [ ] 14.3 Add `character_attributes` and extended `character_screen` Golden rows and thresholds; pin the Hugging Face revision when #25 provisions it (BLOCKED on #25, not on #40); mock fixtures run now.

## 15. Rollout (blocked on #40)

- [ ] 15.1 Enable v2 selector creation behind `FEATURE_CUSTOM_CHARACTERS` only after 8.3 art approval, passing evals, restore drill and the owner decisions listed in issue #40; rollback disables creation and keeps readers and assets.
