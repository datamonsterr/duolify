## Context

E17 adds **Characters**, **Character specs**, a **Character rig**, per-Course **Casts** and a **Default cast** (see `CONTEXT.md` and [ADR 0010](../../../docs/adr/0010-custom-characters.md)). The API owns persistence, authorization and Credits; the Engine remains stateless. Slides stay sandboxed Motion HTML, never video. Narration follows ADR 0009 and E14; published reuse follows E15. The deltas add requirements rather than replacing pending E14/E15 or E16 requirements, so their grading, narration, snapshot and steering rules remain intact.

## Goals / Non-Goals

**Goals:** let a Learner describe an original mascot, customize it, select a recurring Cast and see its appearance/persona used consistently in narration, dialogue, answer reactions and Tutor chat. Keep creation cost, safety, animation and recovery predictable.

**Non-Goals:** free-form image generation, optional AI portraits, voice cloning, real-person imitation, third-party characters, standalone sharing/catalogs, retroactive Lesson changes, implementation code or proto edits in this change.

## Decisions

### Structured appearance and original art

The Engine maps a creation prompt into `CharacterSpec` JSON; it never returns SVG, HTML, scripts, image URLs or voice provider IDs as appearance data. A trusted, versioned renderer compiles the validated parameters into Duolify-authored SVG parts. Motion animates named expression states in the Slide sandbox using pinned, self-hosted assets. This preserves identity between Slides and avoids image-generation cost. Free-form generated portraits cannot provide the same reusable rig and are deferred.

The initial rig supports body shapes `round`, `bean`, `star`; eye styles `round`, `oval`, `sleepy`; mouth styles `smile`, `line`, `open`; accessories `glasses`, `scarf`, `hat`, `badge`. These are proposed original design primitives, not references to another game's art. The renderer supplies all five expressions: `idle`, `happy`, `sad`, `thinking`, `celebrate`. Per-Slide changes select expressions and trusted animations; they cannot mutate a Character's base appearance. Reduced motion uses static expressions; meaningful dialogue and speaker names remain readable without animation or audio.

The Default cast contains three original built-in Characters with distinct shapes, palettes, personalities and allowlisted voices. Final names and art need design review. Built-ins pass the same validation and screening, are available without Credits and are bundled for offline fallback. No third-party names, art or recognizable character designs are permitted.

### CharacterSpec schema contract

Implementation will publish one canonical, versioned JSON Schema shared by validators and fixtures; this document specifies its v1 contract. Every object is closed (`additionalProperties: false`), every field below is required, and strings are plain text with control characters rejected. Optional info is represented by an empty string where allowed. Identity, ownership and screening metadata are outside the model-authored spec.

| Field | JSON Schema constraint |
|---|---|
| `schema_version` | Integer constant `1` |
| `rig_version` | String constant `original-v1`; subsequent versions must remain resolvable for old Lessons |
| `info` | Closed object with `name` (1–40 characters), `bio` (0–280), `backstory` (0–800) |
| `personality` | Closed object with `traits` (1–5 unique strings, 1–40 characters each), `speaking_style` (1–200 characters), `persona_prompt` (1–500 characters) |
| `appearance` | Closed object with `body_shape` (enum above), `palette` (closed object with `body`, `accent`, `outline`, each a `^#[0-9A-Fa-f]{6}$` string), `eyes` and `mouth` (enums above), `accessories` (0–2 unique enum values above) |
| `expressions` | Array of exactly the five unique values `idle`, `happy`, `sad`, `thinking`, `celebrate` |
| `voice_key` | String enum from the versioned, approved voice-key allowlist; maps to an ElevenLabs ID only in trusted config |

The API and Engine both validate before persistence/use. Cross-field checks require all parts to exist in `rig_version`, legible palette contrast, no incompatible accessories, a resolvable `voice_key`, maximum serialized spec size 8 KiB and maximum combined persona/info text 2,400 characters. Unknown fields, oversized content, executable markup and unsupported versions fail validation. Runtime consumers resolve against the recorded schema/rig/voice-config version. A removed voice has an explicit safe default mapping; past audio is not regenerated.

### Creation, manual editing and Credits

Proposed API operations: list/get/delete owned Characters, create or regenerate from a prompt, patch structured fields, and select a Course Cast. Exact routes and transport fields are implementation work; no contract changes are made here. Create/regenerate are idempotent background jobs scoped to Learner and request key. Requests cannot edit another Learner's private Character or select it by guessing its ID.

1. Validate prompt length and limits, then screen it. Reject before mapping on policy failure. Raw prompts are transient job data only and purged on terminal state, at most 24 hours after submission.
2. Show the server quote for `character_create` with Pricing config version, get confirmation and reserve Credits before the mapping call. The price key and its permitted daily-credit eligibility are not yet approved; creation/regeneration remains disabled until owner approval and config-schema support in a new version. Existing E12 approved values are unchanged. Manual edits and built-in selection remain available without a mapping charge.
3. Map to a spec, schema-validate, screen all output fields and compiled appearance, and atomically persist an immutable version with successful settlement. Regeneration replaces the selected current version only after success; failure preserves the old version. A response that cannot pass checks never becomes a Character.
4. Charge at the quote's version and no higher than its ceiling; retries share a reservation. Invalid/unsafe output, cancellation before success or exhausted attempts release the reservation and create no spend. Show an option to continue with the Default cast; do not sell a default as a successful custom creation.
5. Manual edits create a new immutable spec version, without prompt-to-spec mapping, only after validation and screening (platform-paid). They never spend `character_create` Credits. Learners can edit info, personality, rig controls and the voice key through bounded fields. Deletion removes the Character from future selections; existing immutable Lesson and published snapshots follow their retention rules.

Default non-pricing limits, configurable and enforced server-side: 10 active custom Characters per Learner; 5 create/regenerate attempts per day and 2 per minute; prompt length 1–1,500 characters; 20 manual edit attempts per hour; one guide plus 0–2 distinct supporting Characters per Cast. Rejected attempts consume rate quota but no Credits. Built-ins do not consume the ownership quota. Count checks and writes are atomic.

### Safety and bounded personas

Screen raw prompts, every generated field/appearance combination and manual edits for real-person likeness or impersonation, third-party IP/trademarked characters, sexual content, age-inappropriate content and prompt injection. Reject with bounded reason codes (`real_person`, `third_party_ip`, `sexual_content`, `age_inappropriate`, `prompt_injection`, `invalid_spec`, `limit_exceeded`, `screening_unavailable`); do not echo unsafe text. Screening failure is closed for creation, edits and publishing. Never silently rewrite a rejected request into an accepted paid result.

Persona context follows Course steering's pattern: serialize only validated fields inside an escaped, size-bounded data section (`<character_personas>`), after system policy and trusted Course context. System instructions explicitly limit the section to characterization and speaking style. It cannot change rules, tools, output format, safety, grounding, prices, grading or disclose hidden prompts; delimiter escaping and JSON serialization are necessary but not sufficient. Deterministic validation, policy checks and adversarial evals remain gates. Course instructions retain their existing scope; neither persona nor backstory is a source of factual Course truth. No provider credentials, raw creation prompts or unrelated Learner data enter this section.

### Cast selection and immutable use

`course_casts` records the owning Learner's Course or Course copy selection: guide and up to two supporting Character IDs. Selection is free and ownership-checked; each new job resolves the then-current approved versions. Empty selection resolves to the configured Default cast. Changes while a job runs apply to the next job only. Updating a Character does not mutate already generated Slides, tests, audio or shared versions.

At job start the API persists `lesson_cast_snapshots`: role, stable speaker key, Character/version and full validated spec, rig version, voice config version and actual runtime fallback marker. Retries and Retry variants use that snapshot. Generated narration/dialogue uses only speaker keys from it, with expression enums and bounded plain text; unresolved speakers fail validation. If a selected member becomes unusable, replace that slot with a compatible built-in before output, keep the other valid slots, and record the effective Cast. Past snapshots remain stable unless a safety takedown requires suppressing a Character at display time, in which case a bundled safe default renders without rewriting stored content.

Tutor chat offers the main guide by default and a supporting Character from the Course Cast as an explicit choice. Resolve and pin a validated spec version at turn start; changes apply next turn, and historical messages are untouched. Outside a Lesson use the Course selection; inside a Lesson default to its pinned Cast for consistency. Never add persona text to tools or a system-role message.

### Slides, reactions and voices

The player renders trusted SVG parts inside its existing sandbox and CSP; specs cannot supply URLs, event handlers or arbitrary markup. Expressions share the Slide playback clock and reset on replay/seek, with asset preloading/offline caching. Missing/unsupported assets use the bundled Default cast without blocking text/captions or controls. No extra runtime LLM call is needed for rendering or reactions.

The Engine writes narrator/dialogue segments with speaker keys; the API resolves each voice from the allowlist, never from learner text. Segment IDs/order and alignment remain stable; the ADR 0009 hash includes text, actual voice, model and settings. New voices affect only newly synthesized audio. Pricing stays E14's narration price per newly synthesized character; cache hits are free. A removed voice maps to the configured default for new synthesis; ElevenLabs failure still means captions only.

Answer reactions come from a trusted mapping of the API's graded result to `happy`/`celebrate` on success and `thinking`/`sad` with supportive copy on error; `idle` is neutral. Reactions cannot grade, expose an answer before submission, change required correct answers, bypass Retry variants, award XP or unlock a Lesson. The all-correct rule in `product-rules-v1` remains unchanged. Failure to render a reaction leaves authoritative correction and retry controls available.

### Published snapshots and privacy

Before publishing, show which attached Characters' name, bio, backstory, speaking style, persona and appearance will travel with the Course and require explicit confirmation. Extend E15's allowlist with only effective Cast/spec/rig/voice versions and necessary immutable assets, including Casts used in already generated Lessons. Never serialize raw prompts, owner IDs, private library references, screening records or unrelated Characters. Re-screen exported fields. Snapshots are readable as Course content, not editable private library records.

Course copies reuse the pinned Cast with no Character creation charge; on-demand Lessons beyond the snapshot also use it. Library edits/deletion cannot alter the publication; republishing creates a new Course version. To keep E15 reuse simple, Cast overrides on an unforked Course copy are refused with an explanation to use its published Cast or Course steering's existing confirmed fork. A forked copy may select its own Cast for future generation without changing old Lessons; no new automatic fork mechanism is added here. Unpublishing and creator deletion follow E15 retention and attribution; safety takedowns can suppress unsafe Characters in existing copies.

### Storage, backup and deletion

Proposed Postgres records: `characters` (owner, current version, status), `character_versions` (spec JSON, schema/rig/voice versions, content hash), `character_screenings` (version or request ID, verdict, reason, policy version; no raw rejected content), `course_casts`, `lesson_cast_snapshots`, per-chat-turn Character version references and published Cast/spec payloads on E15 versions. Operation IDs connect generation jobs to credit ledger reservations; financial rows retain only opaque references. Redis is disposable cache/rate-limit state.

All records are covered by encrypted nightly `pg_dump`, 14-day retention and restore drills. Store optional compiled SVG bundles/rig versions, manifests and audio/alignment in versioned S3 storage. The nightly backup includes an asset-key/version/checksum manifest and protected copies of referenced objects in the separate backup bucket; a database dump alone does not protect assets. Restore checks resolve every spec/rig/voice version and referenced object without LLM/TTS calls or new charges. Built-in assets are versioned release artifacts and mirrored with any required old rig bundles. Add the inventory in implementation to `docs/data-inventory.md` per the canonical `data-backup` spec (older changes refer to another path).

Delete unused private versions/assets within 30 days of Character/account deletion; retained Lesson snapshots last while their Course exists, then follow its 30-day purge. Published payloads remain while enrolled Course copies reference them, pseudonymized on owner deletion, and purge 30 days after the last reference. Shared content-hash objects require zero references across Lessons and publications before deletion. Screening metadata lasts 30 days and is removed with the account within 30 days. Raw prompt job payloads expire within 24 hours; financial references follow the existing seven-year ledger retention. Backups expire through the 14-day roll-off; restores must reapply deletion/takedown records before serving data.

### Analytics and reliability

The `analytics` delta is the event source of truth; each capability lists its own events and properties. Common properties and opt-out apply. Never send creation prompts, names, bios, personas, backstories, dialogue, answers or SVG content to PostHog. Every model attempt reports `$ai_generation` with `feature` `character_create`, `character_screen`, `lesson_generation` or `tutor_chat`, actual token usage and fallback status; platform-paid screening is not a Learner charge.

Golden set tasks: `character_spec`, `character_screen`, `character_persona_lesson`, `character_persona_chat`, `character_injection`, `character_reactions`, `character_snapshot`. Pin Hugging Face revisions. Proposed task gates: schema/rig/voice/speaker validity and grading/snapshot immutability 1.00; persona adherence judge score at least 0.85; injection resistance at least 0.99 with any prompt leak, unsafe output or grading change a critical failure; unsafe-screen recall at least 0.98 and benign false-reject rate at most 0.05. Implementation must encode these in `evals/thresholds.toml`, add mock fixtures and pass `mise run engine:eval`; ready CI runs the live sample under the existing policy. No dataset or eval code changes are part of this docs task.

Screening deadline 10 s per call; mapping deadline 45 s per call and 90 s total job budget including screening. Retry once on retryable provider errors, then at most one OpenRouter fallback, only if time/token/provider-cost ceilings remain. Never retry unsafe input. Limits come from config: bounded input above, maximum 2,000 mapping output tokens and provider-spend ceiling attached to the approved quote. Exhaustion saves no invalid spec, releases creation Credits and offers Default cast. Runtime persona/appearance failures use the Default cast within existing Lesson/chat deadlines and cost ceilings; base generation failure still releases its own reservation. Chat first-token/idle deadlines remain as in `tutor-chat`: safe default selection happens before streaming; a mid-stream error stops once without splicing a second persona reply.

## Risks / Trade-offs

- Limited rig variety → review primitives with design and preserve versioned extensibility; do not quietly add image generation.
- Subtle likeness/IP or prompt attacks → input/output screening, original art review, schema validation, critical eval gates and takedown support; no claim that delimiters alone make LLMs safe.
- Snapshot retention surprises → explicit publication disclosure, allowlist export, immutable copies and clear delete behavior.
- Multiple voices increase synthesis/cache work → bounded Cast/segments and existing E14 spend caps, cache keys and captions fallback.
- Cast/persona conflicts with instruction or factual content → policy and grading take priority; style stays bounded and factual answers stay grounded in Course context.

## Migration Plan

Implementation first ships schema/rig/built-ins and additive stores behind a flag; existing Courses with no Cast resolve to the Default cast without regenerating content. Enable manual selection/editing after safety, backup and eval gates. Enable creation/regeneration only after owner-approved `character_create` pricing and config-schema rollout. Enable Tutor chat and publication integrations with their respective epics. Rollback disables new creation/selections, retains old schema/rig readers and assets, and displays safe defaults; it never drops persisted versions or shared snapshots. This change performs no migration or rollout.

## Open Questions

### E17-03 scoped implementation review

S23 Settings hosts the private library and S04 Create/Course hosts a Course Cast
link. The shared Components buttons, outlined cards, spacing, typography and
light/dark tokens extend these boards; the boards do not already draw Character
controls. Desktop uses library cards and a two-column structured editor; mobile
opens the same editor as a separate step with Cancel and free Save. Native selects,
labelled controls, all five static expression previews, focus return, a trapped
delete-confirmation dialog, loading/empty/connect-and-retry states and preserved
drafts cover keyboard/touch and reduced motion. Preview uses a scriptless sandbox
and `default-src 'none'`; character text never becomes executable markup.

The original three v1 built-ins, names and logical voices remain explicitly
PROVISIONAL. The flag defaults off, including `.env.example`. Free editing has no
Pricing/ledger dependency, while prompt creation remains unavailable. Live semantic
screening is disabled without E10-06 feature evidence; offline development uses
explicit mock-provider injection. A closed canonical schema is retained at
`services/api/internal/characters/schema/v1.json` and consumed by all three stacks.
All four accessory kinds occupy separate rig parts, so any unique pair is
compatible; the existing two-accessory cap remains. No new art or voice rights are
implied. Screens only edit logical voice keys; actual synthesis is E17-08.

The screening envelope is platform operational policy (not a Credit price): ten
seconds at the API (nine seconds in the Engine), two attempts maximum under the existing E10 launch clamp, 200 output
tokens and 0.05 USD worst-case ceiling. All usage attempts are reported without
content. Model/provider activation remains closed in production. Offline fixture
tasks require 1.00 correctness (stricter than aggregate minimums); publish the same
cases at a pinned HF revision when E10-01 provisions it, with no invented revision.

Private writes use a committed attempt before validation and compare-and-swap
version replacement after screening. Database learner-row locks bound concurrent
quota/count races. Immutable independent snapshots and bundled versioned rigs
survive private deletion; no new S3 assets require regeneration on restore.


1. Approve `character_create` price, Daily free credits eligibility and protected provider-cost ceiling in a new Pricing config version; creation remains disabled until then.
2. Final Default cast names/art, precise rig controls, palette/accessory constraints and supported learner age policy.
3. Validate proposed count/rate/text/token limits and adherence/screening thresholds against a representative Golden set.
4. Confirm per-language voice availability and ElevenLabs redistribution rights for E15; approve the voice allowlist/default mappings.
5. Consider a direct Cast-driven fork flow for published Course copies later; v1 uses the published Cast until the existing steering fork.

## E17-04 implementation defaults and scoped design review

Owner direction permits creation behind `character_create` without approving a
price. `usd-dev-character-create-unapproved` is a labelled UNAPPROVED mock-only
proposal stored in Pricing config; no approved E12 commercial term changes.
Production requires an approved active version with a supported key, limits,
rates and eligibility; absent support disables Create/Regenerate while retaining
free structured edits and built-in selection. No environment value promotes a
live Character provider without E10-06 feature evidence.

S23 provides the Settings host, S04 the bounded entry/confirmation progression,
and S17 the credit ceiling/version confirmation. Their canvases do not already
draw the new Character states. The scoped extension uses existing token-based
cards/buttons, one column on mobile, wrapped controls, safe preview iframes,
plain-text long content and reduced-motion static previews. Entry discloses AI,
private prompt handling and original-only art. Quote review shows the server's
version, ceiling, expiry and explicit UNAPPROVED proposal label in development.
Pending jobs expose reconnect/retry and cancellation; rejection, rate/count,
insufficient balance and provider failures retain the old version and offer free
Default cast through Course selection. Keyboard labels, visible focus, dark/light
colors and mobile target sizes inherit the shared design tokens.

Requests have learner-scoped keys. A row lock serializes quota/count reservations
(rolling 24 hours and one minute); rejected requests consume attempt quota without
mapping spend. Pending creates hold one ownership slot. API background work claims
one confirmed reservation; persistence, current-version replacement, settlement
and success outbox commit atomically. A cancelled/failed job cannot become paid
success. The 90-second confirmed execution deadline cannot restart after reconnect,
and every mapping attempt is capped at 45 seconds and 2,000 output tokens; at most
one repair OR fallback follows primary. Runtime live mapping remains disabled.

`character_create_jobs` stores only transient raw prompt payloads, opaque
quote/reservation links and immutable accepted results. Prompts are erased on
terminal outcome or within 24 hours (also before backups); screening verdicts
expire in 30 days. Private job results follow Character/account erasure, while
independent Lesson snapshots retain their lifecycle and financial rows keep opaque
seven-year references. Restore reuses job/result/ledger keys and never remaps a
terminal job. API mapping/screening events exclude all private text; creation
outcomes are deduplicated transactionally in the existing financial outbox.
Offline recorded `character_spec`, `character_screen`, `character_injection`
cases require every deterministic check to pass; live dataset promotion remains
gated by E10-06, as recorded in ADR 0011.

## E17-05 implementation defaults

- Pinning: `lesson_cast_snapshots.lesson_key` is the generation job id; `generated_lessons.manifest.cast_key` links the Lesson. Preview uses defaults (no Course yet).
- Engine: `character_personas_json` is validated, bounded (3 speakers, field caps) and appended below the Course/lesson rules as delimiter-escaped untrusted data labelled style-only; never a system role or tool input. Retry-variant requests carry it for validation only; grading prompts never include it.
- Segments: at most 12 per Slide, 280 plain-text characters, five expressions, pinned speaker keys only, inside the Slide duration; additive proto `CharacterSegment`. Persisted in `lesson_slides.segments` (migration `20261112211701`).
- Playback: the Lesson manifest carries optional `cast` and per-Slide `segments`; the client prepends a host-clocked character stage to the sandboxed Slide. `seek`/`tick` are the only clock, so pause, seek, replay and rate changes reset deterministically; reduced motion shows static expressions. Missing, unsupported or taken-down slots render bundled defaults and emit `character_render_fallback`.
- Analytics: `lesson_cast_applied` (cast_source, cast_version, supporting_count, fallback_used) at job start; no character text, persona, dialogue or SVG.
- Evals: `character_persona_lesson` plus new `character_injection` cases, gated at 1.0 offline.

## E17-06 implementation defaults

- Source of truth: `characters.ReactionFor(correct, passed, hasRetry)` in the API maps the already-graded result to `{trigger, expression, copy}` and is added to every answer response (wrong with Retry variant, correct, pass) before the operation payload is stored, so idempotent replays and restored attempts return the identical reaction. Grading, mastery, correction, Retry variant, XP, unlock and completion code is unchanged; the reaction is never read back.
- Mapping: wrong with a queued Retry variant -> `answer_incorrect`/`thinking`; wrong with none -> `sad`; correct -> `answer_correct`/`happy`; Lesson pass -> `test_passed`/`celebrate`. `passed` without `correct` is never a pass. Idle is the static neutral expression.
- Copy: three approved supportive lines (API, client and engine lists must match). The client always shows the approved line for the trigger; persona text, names and any other string from the response are never rendered.
- Speaker and rendering: the Lesson test page reads the Lesson's pinned Cast from the existing manifest (best effort) and renders the guide through the validated closed-schema rig. Missing Cast, unsupported rig, invalid spec or invalid reaction fall back to the bundled static neutral SVG (approved copy kept when the reaction itself is valid); correction and Continue stay available. No animation is added, so reduced motion needs no variant.
- Analytics: client `character_reaction_shown` (speaker_key, expression, reaction_trigger, fallback_used) once per Lesson/question result; closed-enum properties only. Opt-out and PostHog failure never block the answer flow.
- Evals: `character_reactions` task (and `character_injection` reaction cases) gated at 1.0 offline; no new proto fields, tables or migrations; no runtime model call or spend.
