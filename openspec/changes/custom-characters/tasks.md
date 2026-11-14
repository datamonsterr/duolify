This is the future implementation checklist; this change delivers documentation only. For each slice, write failing e2e tests, then integration tests, then unit tests, then implement until they pass. Check current official library/provider docs before implementation. Any future proto additions require matching API and Engine changes in the same implementation PR; none are made here.

## 1. Prompt creation, regeneration and Credits

- [ ] 1.1 E2E first: allowed prompt produces a private previewable Character; regeneration changes only future uses; retries produce one Character and one charge.
- [ ] 1.2 E2E: unapproved pricing key, insufficient Credits, unsafe/IP/real-person/sexual prompts, quota exhaustion and provider failure leave no new Character/spend and offer the Default cast appropriately.
- [ ] 1.3 Integration: ownership, atomic count/rate limits, idempotent jobs, immutable versions, reservation/settlement/release, pricing flip during a job and failure preserving the prior version.
- [ ] 1.4 Integration: input/output screening fails closed; raw job prompts expire within 24 hours; model retries/fallback stay within time, token and provider-cost ceilings.
- [ ] 1.5 Unit: closed CharacterSpec schema, required fields, bounds, enums, cross-field checks, version compatibility and deterministic rejection reason codes.
- [ ] 1.6 Unit: persona serialization/delimiter escaping, instruction hierarchy and no provider IDs, markup or URLs in specs; success/failure analytics emitters contain no content.
- [ ] 1.7 Obtain owner approval of `character_create` price, daily-credit eligibility and provider-cost cap in a new Pricing config version; add supported-key validation without altering approved E12 values; keep paid operations disabled until approved.
- [ ] 1.8 Implement the canonical JSON Schema, Engine mapping/screening and bounded provider routing, with mock fixtures; add any required paired API/Engine contract changes only in that implementation PR.
- [ ] 1.9 Implement Postgres stores, private API operations and idempotent worker/credit flow; document raw prompt/verdict retention and account deletion.
- [ ] 1.10 Implement creator/regeneration UI with structured preview, authoritative quote/confirmation, pending/rejected/failure/limit states and Default cast option; pass the slice's tests.

## 2. Original rig, free manual editing and Course Casts

- [ ] 2.1 E2E first: manual edit changes info/appearance/personality/voice for free; choose guide and supporting Characters; a Course without selection uses original built-ins.
- [ ] 2.2 E2E: other Learners' private Characters cannot be read/edited/selected; unsafe manual edits fail closed; deletion removes future selections without rewriting past content.
- [ ] 2.3 Integration: Cast ownership/count checks, atomic version increments, job-start snapshots, edits during running jobs and stable retries; default resolution for deleted/unavailable selections.
- [ ] 2.4 Integration: manual screening is platform-paid with no `character_create` spend; library deletion purges unused versions and retains only required immutable snapshots.
- [ ] 2.5 Unit: trusted rig compilation, five expressions, part/contrast compatibility, no arbitrary SVG/script/network data, voice-key allowlist resolution and Cast limit tables.
- [ ] 2.6 Unit: selection/edit/delete event emitters, field-name-only analytics and free-operation ledger invariants.
- [ ] 2.7 Finalize three original built-in Characters, names/art, rig primitives and age/accessibility review with design; reject any resemblance/reference to third-party game characters.
- [ ] 2.8 Implement versioned SVG rig/Default cast bundles and approved voice mappings; preserve old readers/assets and supply offline defaults.
- [ ] 2.9 Implement free structured editor, private library and Course Cast picker with limits, validation/screening feedback, deletion behavior and analytics; pass the slice's tests.

## 3. Lesson generation, player, narration and test reactions

- [ ] 3.1 E2E first: chosen Cast narrates and speaks dialogue consistently across Slides; all expressions, pause/seek/replay/speed, reduced motion and offline replay work.
- [x] 3.2 E2E: voice-aware cached audio costs zero; removed voices use safe mappings; TTS failure gives captions; missing rigs use defaults and never block a test.
- [ ] 3.3 E2E: answer reactions follow authoritative grading, wrong answers still receive corrections/Retry variants, and only all-correct completion unlocks the next Lesson once.
- [ ] 3.4 Integration: Course preview/lesson Cast input, instruction coexistence, job/Retry variant snapshot reuse, valid speaker keys, immutable past Lessons and unchanged Gated generation.
- [x] 3.5 Integration: multi-speaker voice resolution, segment order/alignment, synthesis deduplication and reservations/caps, captions fallback and no Character creation charge during use.
- [ ] 3.6 Unit: expression timeline/reset behavior, allowed speaker references, safe sandbox rendering, reaction-result mapping and voice-aware cache key composition.
- [ ] 3.7 Unit: grading/answer-privacy invariants under adversarial persona content; render, generation, reaction and voice-fallback analytics emitters.
- [ ] 3.8 Implement Engine Cast context/speaker validation and manifest/snapshot persistence with paired API/Engine transport changes if needed, preserving Course steering and generation contracts.
- [ ] 3.9 Implement player rig/dialogue rendering, preloading/offline bundles and accessible static fallback without relaxing CSP/sandbox.
- [x] 3.10 Implement allowlisted per-speaker ElevenLabs synthesis under ADR 0009 with existing pricing, cache and alignment rules; confirm current provider docs and voice rights.
- [ ] 3.11 Implement deterministic presentation-only test reactions, approved supportive copy and key PostHog events; pass the slice's tests.

## 4. Tutor chat personas

- [x] 4.1 E2E first: main guide is default, supporting tutor selection works, Lesson versus Course context resolves the right version, and turn changes never rewrite history.
- [x] 4.2 E2E: injected persona cannot reveal prompts, change facts/credit rules or bypass safety; unavailable persona uses the Default cast before streaming; mid-stream error ends once.
- [x] 4.3 Integration: turn-start version snapshots, ownership checks, bounded context assembly, first-token/idle deadlines, provider fallback, token billing and thread deletion.
- [x] 4.4 Unit: escaped data-only persona builder, precedence/grounding rules, turn selection/default resolution and private analytics properties.
- [x] 4.5 Implement per-turn persona/reference persistence and API/Engine context input with existing streaming safety, fallback and spend ceilings.
- [x] 4.6 Implement tutor identity/selector, default/failure UI and selected/fallback PostHog events; pass the slice's tests.

## 5. Published Character snapshots and privacy

- [ ] 5.1 E2E first: explicit export disclosure/confirmation precedes publish; enrolled copies retain frozen Characters after library edits/deletion and reuse assets without Character creation Credits.
- [ ] 5.2 E2E: on-demand generation uses the published Cast; overrides are refused before the existing confirmed steering fork; forked copies can select future Casts without altering old Lessons.
- [ ] 5.3 E2E: republish/update pinning, owner account deletion, unpublish and safety takedown preserve E15 semantics and never expose prompts/private library records.
- [ ] 5.4 Integration: content-only allowlist scanner, current screening/fail-closed publication, hash coverage, independent snapshot payloads, reference counts, last-reference purge and suppression state.
- [ ] 5.5 Unit: snapshot projection/diff/hash, removal of private fields and library identifiers, published-Cast selection guards and publication/fallback analytics.
- [ ] 5.6 Implement E15 export allowlist extensions, immutable Cast/spec/assets and safe-copy generation, with current voice redistribution permission confirmed before enabling.
- [ ] 5.7 Implement Character export disclosure/confirmation, immutable reuse/update/deletion and safe takedown fallback; emit `character_snapshot_published`; pass the slice's tests.

## 6. Backup, analytics privacy and model reliability gates

- [ ] 6.1 E2E first: create/select/publish/chat funnels emit the cataloged PostHog events once, opt-out stops them, and no prompt/persona/dialogue/answer content is captured.
- [ ] 6.2 E2E: restore a database plus asset backup and play a private/published Lesson with the original Cast, voices and reactions without paid regeneration or exposing deleted/taken-down content.
- [ ] 6.3 Integration: nightly dumps include every Character/Cast/chat/publication record; protected S3 copies and version/checksum manifests resolve; deletion/takedown replay and shared-object retention are enforced.
- [ ] 6.4 Integration: model/screening outages, schema-invalid output, timeout/cost exhaustion and PostHog outage follow fail-closed/default/release rules without blocking valid base learning.
- [ ] 6.5 Unit: event schema/no-content guards, idempotent emitter keys, restore reference checks and critical Golden set gate/threshold handling.
- [ ] 6.6 Add Hugging Face Golden set rows for `character_spec`, `character_screen`, `character_persona_lesson`, `character_persona_chat`, `character_injection`, `character_reactions`, `character_snapshot`; pin the revision and mirror it per existing policy.
- [ ] 6.7 Add mock fixtures and explicit per-task thresholds in `evals/thresholds.toml` for validity, adherence, screening, injection, grading and snapshot/privacy immutability; critical leaks/unsafe outputs/grading changes must fail the gate.
- [ ] 6.8 Implement/verify privacy-safe PostHog emitters and actual per-attempt `$ai_generation` usage; keep all names/properties in this change's analytics catalog, with existing delivery and person-deletion rules.
- [ ] 6.9 Update `docs/data-inventory.md` with tables, S3 objects, prompt/verdict/financial retention and deletion rules; extend nightly backup/restore checks for protected asset copies and old rig versions.
- [ ] 6.10 Run only focused area mise tests for each implementation slice, `mise run engine:eval`, and the existing restore drill; verify mock and ready-CI live-sample modes under their budgets.
- [ ] 6.11 Gate rollout on approved pricing, original-art/age/voice review, passing evals and restore checks; verify rollback disables creation while preserving versions/assets and safe defaults.
- [ ] 6.12 Validate the completed change with `mise exec -- openspec validate --all --strict` before implementation handoff.
