# Golden set

Each JSONL row has an `id`, `task` (preview, outline, lesson, chat or note), JSON `input`, textual `rubric`, and `checks`. Check kinds are `json`, `field`, `slide`, `contains`, and `regex`; `field`, `contains`, and `regex` use a `value` string. A case passes when all checks pass. `thresholds.toml` sets the required fraction of passing cases.

Create a dataset on Hugging Face and upload `golden.jsonl` at its root. Set `HF_GOLDEN_DATASET`, `HF_GOLDEN_REVISION` to a commit hash, and `HF_TOKEN` with dataset read access. Run `mise run engine:eval-live` with provider keys. The runner downloads that revision into `evals/.cache`; reports are written to `evals/reports`. Use `mise run engine:eval` for the offline mock fixtures. `--judge` additionally requests a PASS/FAIL judgment against the rubric.

Live evaluations accept `--limit <N>` or `EVAL_SAMPLE_LIMIT=N` to run a small sample. The command-line flag takes precedence over the environment variable. Cases are sorted by `id` before selecting the first N, so the sample is deterministic. An unset limit or `0` runs all cases; values larger than the dataset also run all cases. The runner logs the selected and total case counts. For example, `EVAL_SAMPLE_LIMIT=2 mise run engine:eval-live` evaluates two cases.

E2-06 adds `course_brief_chat` cases. Offline brief cases contain a `recorded` array of classifier JSON responses (or `null` for a failed provider attempt) and run the production stateless brief processor, including schema validation, attempt/USD bounds and deterministic field mapping. `path_equals` uses a JSON-encoded `{ "path": "brief.topic", "expected": "..." }` value to assert exact fields, empty context, bounded follow-ups, confirmation invalidation and terminal form fallback. No provider network call is made in mock mode. Publish these same cases at a pinned `datamonsterr/duolify-golden` revision after E10-01 owner provisioning; E10-06 gates live promotion.

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
