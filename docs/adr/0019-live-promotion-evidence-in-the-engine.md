# ADR 0019: Live promotion evidence embedded in the engine

- Status: Proposed (engine agent, 2026-11-22; implements E10-06 #109)
- Date: 2026-11-22
- Change: `openspec/changes/gate-ai-promotion-on-reproducible-live-samples` (tasks 5.x)

## Context

The promotion rule (`ai-reliability` spec, ADR 0011 E10-06) says a model or voice is active in
production only after passing live cases on a pinned Golden set revision. Before this change a
live engine applied it unevenly: the four safety screens and character mapping refused every
call (`fixture_only`), so steering, character edits, publication and reviews could never work on
the live route, while every other RPC called the live model with no evidence check, and live
narration turned on with `NARRATION_LIVE_ENABLED` alone.

## Decision

- The committed live report `evals/promotion/live-report.json` is the evidence. The engine embeds
  it at build time; no environment variable enables a feature.
- The engine accepts it only when it passes the eval promotion gate (live provider, `passed`, no
  critical failure, inside budget, all canonical tasks), its revision and `dataset_sha256` match
  `evals/pinned-datasets.toml`, and its `fingerprint` equals the build's: SHA-256 over
  `models.toml`, `thresholds.toml` and the engine-core and engine-providers sources that shape a
  model call. A change to any of them makes the evidence stale until a new live run is recorded.
- Every model-backed RPC requires its own task to have passed (`GATED` in engine-server); live
  narration requires `lesson_narration`. Fixture routers and the mock voice are not gated.
- A refusal is gRPC UNAVAILABLE with a `google.rpc.ErrorInfo` (domain `engine.duolify`, reason
  `promotion_evidence_*` or `promotion_task_not_passed`). The engine and the API log it as
  `live_feature_not_promoted`; learners keep the existing retryable "unavailable" outcome. No
  proto change.
- `duolify-eval promote` writes the evidence only from a report that passes the gate for the
  current build. Ready CI uploads such a candidate when secrets exist; committing it is a
  reviewed change.

## Consequences

- An engine change that touches a fingerprinted source turns live AI off in deployments built
  from it until `mise run engine:eval-golden` and `mise run engine:promote` are rerun (about
  $0.002). `mise run engine:evidence` and the CI job show the state.
- Tasks without live golden cases (`outline_chat_change`, `outline_replan`, `character_create`,
  `lesson_narration`) stay closed on a live engine until cases are published in a new pinned
  revision. Before this change outline chat changes and replanning ran live unchecked.
- Evidence is one run; a flaky task can fail a later run. The report records served models and
  fallbacks per case for review.
