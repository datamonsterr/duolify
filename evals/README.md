# Golden set

Each JSONL row has an `id`, `task` (preview, outline, lesson, chat or note), JSON `input`, textual `rubric`, and `checks`. Check kinds are `json`, `field`, `slide`, `contains`, and `regex`; `field`, `contains`, and `regex` use a `value` string. A case passes when all checks pass. `thresholds.toml` sets the required fraction of passing cases.

Create a dataset on Hugging Face and upload `golden.jsonl` at its root. Set `HF_GOLDEN_DATASET`, `HF_GOLDEN_REVISION` to an explicit 40-character commit hash (missing revision reports explicit `missing_config` status; `main` is not allowed), and `HF_TOKEN` with dataset read access. Run `mise run engine:eval-live` with provider keys. The runner downloads that revision into `evals/.cache`; reports are written to `evals/reports`. Use `mise run engine:eval` for the offline mock fixtures. `--judge` additionally requests a PASS/FAIL judgment against the rubric using word-boundary parsing (`\bPASS\b` / `\bFAIL\b`).

Before using a remote revision, commit its expected SHA-256 in
[`pinned-datasets.toml`](pinned-datasets.toml). Each quoted dataset table maps an
exact 40-character revision to the 64-character hex digest of the exact UTF-8
`golden.jsonl` bytes, including final newlines. Review those source bytes and their
digest independently before committing the pin; never derive a pin from a cache
sidecar or the mirror being verified. The manifest is embedded when the eval
binary builds. Unknown dataset/revision pairs fail before cache access or download.
The current entries are synthetic HF/RustFS test doubles under
`duolify-fixtures/*`; production `datamonsterr/duolify-golden` remains disabled until
E10-01 provisions a reviewed revision and digest. Do not reuse a synthetic pin as
a production revision.

Every HF, mirror and cache payload must match that repository pin. An existing
cache without its `.sha256` sidecar fails explicitly and is never self-signed;
remove both cache files and refetch from a verified source to recover. A sidecar
must also match the pin. Mirrors require a checksum header or nonempty `.sha256`
object, and their bytes must still match the repository pin. HF fallback is
limited to network failures, timeouts (including HTTP 408), and HTTP 5xx;
401/403/404 and other client errors or integrity failures stop evaluation.
JSON reports include `dataset_verified`, and Markdown displays Dataset Verified.
It is false for custom files, offline fixtures, missing configuration/secrets and
legacy reports without the field. Promotion requires it to be true; rejected
remote downloads produce no passing report and never populate the cache.

Live evaluation budget sums provider-reported usage (primary and judge) using the configured price table in `engine/config/prices.toml` (overridable via `EVAL_PRICES_CONFIG`), failing closed with a critical failure if usage is unavailable. Spend is bounded by `--budget <USD>` or `EVAL_BUDGET_USD=USD`; execution halts immediately upon reaching the budget.

Live evaluations accept `--limit <N>` or `EVAL_SAMPLE_LIMIT=N` to run a small sample. The command-line flag takes precedence over the environment variable. When a limit is set, cases are task-stratified using deterministic round-robin selection ordered by case `id` within each task, while still strictly enforcing minimum per-task coverage for every required canonical task. An unset limit or `0` runs all cases; values larger than the dataset also run all cases. In ready pipeline mode (`--mode ready`), missing credentials report an explicit missing-secret status and keep the `ci-ok` CI aggregate passing while keeping feature activation disabled without fabricating a pass.

## Promotion gate

Feature activation consumes the evaluation report via the promotion gate command:
```bash
mise run engine:gate evals/reports/latest.json
# or directly:
duolify-eval gate <report.json>
```
The promotion gate exits 0 only if:
1. Report status is `passed` (not `missing_secrets`, `missing_config`, `budget_exceeded`, or `critical_failure`).
2. Overall eval `passed = true` with no critical failures.
3. Provider was `live` and dataset source was live (`hf`, `mirror`, or `cache`), never mock fixtures.
4. Dataset revision is an immutable 40-character hex commit hash.
5. Every required canonical task (`course_preview`, `course_outline`, `lesson_slides`, `lesson_test`, `tutor_chat`, `note_enrich`) is present with minimum per-task coverage (`total_cases >= 1`) and passing score above threshold.
6. `dataset_verified = true`: dataset bytes were verified against the repository digest pin. Missing verification in older reports fails closed.

E10-06 adds live promotion gating on reproducible live samples. Evaluation reports record case, revision, provider, usage, budget, and thresholds, retained as 90-day artifacts. Critical safety, ledger, and cost check failures immediately reject the evaluation. Promotion of models or narration voices requires passing live cases on a pinned immutable commit hash; offline fixtures or missing credentials leave production feature activation disabled. Recorded mock fixtures gate development under ADR 0011 until E10-01 provisions live secrets and dataset.

E2-06 adds `course_brief_chat` cases. Offline brief cases contain a `recorded` array of classifier JSON responses (or `null` for a failed provider attempt) and run the production stateless brief processor, including schema validation, attempt/USD bounds and deterministic field mapping. `path_equals` uses a JSON-encoded `{ "path": "brief.topic", "expected": "..." }` value to assert exact fields, empty context, bounded follow-ups, confirmation invalidation and terminal form fallback. No provider network call is made in mock mode. Publish these same cases at a pinned `datamonsterr/duolify-golden` revision after E10-01 owner provisioning; E10-06 gates live promotion.

E10-04 recorded fixtures cover pinned source fallback, terminal failure with undelivered
reservation release, and mirror integrity validation. Live runs require pinned 40-character
hex revisions, verify SHA-256 checksums of mirror and cache artifacts, and fall back to the
RustFS/S3 mirror during Hugging Face outages. Mock fixtures remain deterministic and never
substitute silently for a requested live run. Pinned revision cases remain Git-pinned until
E10-01 provisions them on Hugging Face; E10-06 gates live promotion.

E7-03 `credit_envelope` recorded fixtures execute the production bounded provider
runner with API-owned limits. They cover uncached/cache-write/cache-read usage,
malformed output, unknown failed-attempt usage, fallback and cap exhaustion.
Quote-version pinning, local-midnight release and financial backup invariants run
against Postgres in the credits integration/restore suites. Publish these fixture
rows to the pinned `datamonsterr/duolify-golden` revision when E10-01 is provisioned;
no live promotion is enabled by this ticket.

E2-01 `course_preview` recorded fixtures cover beginner, expert, empty and Vietnamese
profiles, malformed/unsafe primary output, terminal failure and a fallback that
cannot fit the remaining USD cap. The offline gate builds the production preview
prompt and runs the shared bounded executor with production schema validation;
it never calls a live provider. Fixture revisions are pinned by the Git commit
until the ADR 0011 live Hugging Face gate is provisioned; E10-06 still gates live
model promotion.

E2-08 `outline_chat_change` fixtures replay recorded mock completions through the
production prompt, typed diff validator and bounded executor. Cases cover all four
operation kinds, exact preservation of manual edits, injection, unknown/reused IDs,
duplicate operations, fallback and terminal outage. API integration owns the
matching financial/lifecycle cases: insufficient Credits (zero engine calls),
released failed reservations, stale/duplicate Apply, Discard and Undo; backup
fixtures preserve full proposal rows and replay deletion. Git pins these fixtures
until E10-01 provisions the same cases in `datamonsterr/duolify-golden`; live
publication and E10-06 promotion remain deferred.

E2-02 `course_outline` fixtures validate that confirmed edited outlines are
preserved exactly without content injection or data loss. Cases cover valid
structure, empty title rejection (input schema and output fallback), rejection of
slides and lesson tests, profile handling (beginner, expert, empty), exact
adherence to manual unit reordering and lesson renaming, and rejection of stale or
invalid revision numbers before provider calls. The offline gate runs the production
`creation_prompt` and validates outputs via `validate_created` within bounded limits.
Terminal failure releases undelivered reservations without partial enrollment in the
API integration suite. Revision rows remain Git-pinned until E10-01 Hugging Face
provisioning; E10-06 gates live promotion.

Review #161 adds eight `lesson_test_variant` recorded cases that call the same
production paraphrase processor as GenerateRetryVariants and GenerateLesson pool
repair. Critical checks reject source-prompt reuse, cosmetic prefixes, incorrect
answers, untaught/missing topics and a third attempt. Initial pool failure releases
undelivered customer Credits; approved pool exhaustion remains free. Checkpoints,
atomic credit/content commits and cleanup retries are exercised against Postgres
in the lesson-test, backup, accounts and credits suites. Git pins these fixtures
until E10-01 provisions the matching live dataset; no live promotion is implied.

E8-01 adds `tutor_chat` cases with a `recorded` reply and a `context` (outline, effective profile, oldest-first history, messages). The runner builds the production tutor prompt (`engine_core::chat::prompt`) and emits boolean fields: `prompt_valid`, `delimiter_intact`, `instructions_clean` (none of `markers` appear in the instruction text), `profile_provided`, `current_lesson_empty`, `history_oldest_first`, `grounded` (reply contains every `expect_terms`) and `forbidden_absent`. Grounding, profile, history, empty-profile and injection cases carry critical `path_equals` checks. Publish them at a pinned `datamonsterr/duolify-golden` revision after E10-01 provisioning; E10-06 gates live promotion.

E8-02 adds `tutor_chat` streaming cases (`input.stream`: scripted `primary`/`fallback` with `deltas` and `then` = `end|fail|hang`, `first_token_ms`, `idle_ms`, `cancel_after`, quote caps). The runner drives the production `stream_bounded_routed` + `reconcile` path and emits `terminal`, `error_code`, `retryable`, `delivered_text`, `fallback_used`, `second_provider_after_delta`, `usage_frames`, `usage_estimated`, `usage_within_bounds` and `provider_dropped`; every case carries critical `path_equals` checks (first-token fallback, first-token failure, idle timeout, cancel, mid-stream failure, usage bounded by the quote). Pin them at a `datamonsterr/duolify-golden` revision after E10-01.

E14-02 adds `lesson_narration` golden rows `lesson-narration-cache-hit`, `lesson-narration-retry-same-hash` and `lesson-narration-alignment-failure` to `fixtures/golden.sample.jsonl`, backed by the recorded mock fixture `fixtures/narration.dedupe.json` (cache, retry, alignment failure). The same rows must be added to the Hugging Face dataset and its pinned revision bumped by the dataset owner; until then development gates use the recorded fixtures and live promotion stays disabled (E10-06).

E14-04 adds `lesson_narration` golden rows `lesson-narration-multilingual`, `-timing`, `-vendor-failure` and `-cost-cap` to `fixtures/golden.sample.jsonl`, backed by the recorded fake-vendor fixture `fixtures/narration.vendor.json` (replayed against a local fake ElevenLabs server by `engine-providers/tests/elevenlabs.rs`; the live API is never called). The same rows belong in the Hugging Face dataset at a bumped pinned revision, which the dataset owner provisions with E10-01; live promotion stays disabled until E10-06.
E15-01 `course_screen` recorded fixtures cover safe, borderline, multilingual (Vietnamese), deep hidden payload, injection, malformed-then-fallback and double-malformed cases. Each has a `sample` (allowlisted outline plus lessons), `limits` (60000 input tokens, 300 output, 120s, two attempts, USD cap) and a `recorded` array; the runner runs the production `engine_core::sharing::screen_course` and emits `verdict`, `reason_codes`, `attempt_count` and `fallback_used`. Invalid output after the allowed attempts yields `pending_screening` (fail closed). Publish these rows to the pinned `datamonsterr/duolify-golden` revision when E10-01 is provisioned (revision pin recorded then in `pinned-datasets.toml`); E10-06 gates live promotion. No live provider call is made.
Issue #199 adds `e15-01-phone-uuid-title` (UUID, year, ISBN, version, math in title/slide: `pass`) and `e15-01-phone-real-number` (`+1 415 555 2671`: `needs_review`/`personal_data`). The recorded verdicts mirror the deterministic phone rule in Go `hasPhone` (`services/api/internal/sharing/logic.go`); the engine itself has no phone rule.

E15-04 `published_copy_lesson` recorded fixtures cover a missing lesson in a published copy: success, schema failure with one bounded fallback, provider failure then fallback, double invalid output (never delivered), a USD cap that halts the fallback, and the gate refusals (previous lesson not passed, no consent, insufficient credits, lesson inside the snapshot) that make zero provider calls. Each row has a `request`, a `gate`, `limits` (8000 input tokens, 4000 output, 90s, two attempts, USD cap) and a `recorded` array; the runner runs the production `engine_core::sharing::generate_missing` and emits `delivered`, `refusal`, `attempt_count`, `fallback_used`, `provider_calls` and `within_usd_cap`. Publish these rows to the pinned `datamonsterr/duolify-golden` revision when E10-01 is provisioned (revision pin recorded then in `pinned-datasets.toml`); E10-06 gates live promotion. No live provider call is made.

E14-06 adds `lesson_narration` golden rows `lesson-narration-approved-voice-alignment`, `-approved-voice-locale`, `-unapproved-voice` and `-voice-switch-replay` to `fixtures/golden.sample.jsonl`, backed by the recorded mock fixture `fixtures/narration.voices.json` (every approved voice keeps full character alignment, including Vietnamese, Japanese and accented text; an unapproved voice key never reaches the provider; switching back to a stored voice is free). The engine narration tests replay it and the API unit test checks the voice keys match. The same rows belong in the Hugging Face dataset at a bumped pinned revision, which the dataset owner provisions with E10-01; live voices stay disabled and E10-06 gates live promotion.

E17-08 adds `character_persona_lesson` golden rows `e17-08-narration-multilingual-multi-speaker`, `-speaking-rate-bounds`, `-alignment-coverage-order`, `-alignment-gap-rejected`, `-cache-same-text-voices`, `-retired-voice-fallback` and `-captions-only-failure` to `fixtures/golden.sample.jsonl`, backed by the recorded mock fixture `fixtures/narration.characters.json` (approved Character voices only; retired or unknown voice keys fall back to the default voice with a fixed reason; invalid alignment or order rejects the output; exhausted synthesis leaves captions only with zero charge). The engine-core character tests replay the fixture voices. The same rows belong in the Hugging Face dataset at a bumped pinned revision, which the dataset owner provisions with E10-01; live voices stay disabled and E10-06 gates live promotion.
