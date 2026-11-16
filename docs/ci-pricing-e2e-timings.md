# PR #245: e2e timing investigation

Measured 2026-11-16 UTC / 2026-11-17 local on Linux amd64, Ryzen 5 2600,
Go 1.27.3, Docker 29.9.1, with the race detector and test caching disabled.
Branch baseline: `0393e92`; pinned `origin/main`: `d6c818e`.
The latter has the same API source as `46b293a`; its upstream timeout-only
change is outside this fix. This fix changes no workflow or timeout.

## Findings and changes

- The largest observed regression was `TestE2EPlanEnvironmentChangesAndRejectViolations`:
  44.06 s on the original branch, 12.92 s on the initial main run, and 2.30 s on
  repeated main. Its four actual plan scenarios each took about 0.1–0.2 s. The
  remaining time was a second, non-race host build of the entire API CLI. A
  pricing source change invalidates this separate build cache, even though
  `api:test-e2e` already builds that real CLI inside `duolify-worker:local`.
  Reuse that required image with `docker run --interactive` for the same four
  success, public-domain rejection, missing-backup rejection, and secret-redaction
  scenarios. The test now takes 1.69–1.79 s and does not depend on the host CLI
  build cache. Every existing scenario assertion remains.
- E12-09 was already a subtest sharing E12-10's database, accounts, server,
  catalogue, and analytics sink. There was no duplicate server setup to remove.
  The added `OwnerApprovedProductionLifecycle` cost 2.11 s before and 1.53–1.55 s
  after. No other e2e tests/subtests were added by the original pricing branch.
- The new owner-approved fixture is read explicitly by that lifecycle helper;
  startup and migrations do not scan or automatically load the fixture directory.
- Every `pricing.Parse` previously decoded and compiled the same embedded JSON
  Schema again. An E12-10 CPU profile measured 0.49 s in `Compiler.Compile` out
  of 2.68 s sampled CPU (18.28%). Compile the immutable schema with
  `sync.OnceValues`, retaining `AssertFormat`, schema validation, cross-field
  checks, and fresh parsed configuration data for every request. Catalogue rows
  still reload normally; no stale catalogue/config cache is introduced.
- No HTTP checks were moved to integration tests. No existing assertions,
  activation waits, or test cases were removed or weakened. The exact same 87
  branch e2e test/subtest names pass before and after, with no skips. A new
  concurrent unit test checks valid and invalid owner policy validation and
  independent returned configuration maps under the race detector.

The observed package difference is cache-sensitive; these measurements do not
attribute a fixed 45 seconds to the new pricing lifecycle. They identify and
remove the variable full-CLI rebuild cost that dominated the measured gap.
Container startup also varies between runs; individual timings are observations,
not CI timing guarantees.

## Setup and reproducibility

`.mise/tasks/api.toml` sets `DUOLIFY_ENV=development`, runs
`gotestsum -- -race -count=1 ./test/e2e/...` from `services/api`, and depends on
`infra:build-worker`. That dependency builds the worker and backup initializer
through Compose. Tests create disposable Postgres/Redis/RustFS containers;
no additional long-running Compose services or configured live services are used.

Initial diagnostic Go runs reused the same prebuilt branch worker image.
The subsequent full main task rebuilt its image from main, and both full patched
branch tasks rebuilt from the modified branch. The main and branch e2e runs were
sequential. The focused pricing integration suite overlapped main's repeat and
may contribute to its container startup variance.

```sh
# Diagnostic JSON, after the same prerequisite as the normal task:
mise run infra:build-worker
DUOLIFY_ENV=development mise exec -- go test -C services/api -race -count=1 -json ./test/e2e/... > /tmp/e2e.json

# Full task, including its dependency, with timing events retained:
GOTESTSUM_JSONFILE=/tmp/e2e-task.json mise run api:test-e2e

mise run api:lint
DUOLIFY_ENV=development mise exec -- go test -C services/api -race -count=1 ./internal/pricing/...
API_TEST_PACKAGES=./internal/pricing/... mise run api:test-integration

# Repeatable parse benchmark and concurrent validation check:
DUOLIFY_ENV=development mise exec -- go test -C services/api -race -count=1 -run '^TestParseConcurrentValidation$' -bench '^BenchmarkParseOwnerApprovedCatalogue$' -benchtime=100x ./internal/pricing
```

## Validation and task totals

| Run | Package time | Whole mise task | Test/subtest passes |
| --- | ---: | ---: | ---: |
| Main initial diagnostic | 136.388 s | Diagnostic only | 86 |
| Main full task repeat | 141.720 s | 165.99 s | 86 |
| Branch before (`0393e92`) | 170.190 s | Diagnostic only | 87 |
| Branch after, first full task | 117.162 s | 263.06 s | 87 |
| Branch after, final full task | 114.550 s | 121.10 s | 87 |

The first patched full task pulled a newly resolved floating `golang:1.27` base
and rebuilt its Docker layers; the extra 145.90 s is outside the Go package time.
The final task used the same warmed Docker layers as main's full task repeat and
finished comfortably within the existing three-minute step budget locally.
Its package is 55.640 s faster than the branch baseline and faster than both main
runs, eliminating the added runtime overhead.

`mise run api:lint`: **0 issues**. Focused pricing unit tests: **pass**, 2.538 s.
`API_TEST_PACKAGES=./internal/pricing/... mise run api:test-integration`:
**154 tests passed**, 57.784 s, including activation, forward rollback, historical
quotes, and backup/restore checks. All test runs above used `-race -count=1`.

| Owner-approved fixture parse benchmark | Before | After |
| --- | ---: | ---: |
| Time/op (`-race`, 100 iterations) | 33.026247 ms | 6.850261 ms |
| Bytes/op | 1,713,837 | 202,989 |
| Allocations/op | 25,226 | 3,894 |

## Complete per-test timings

Seconds from each passing Go JSON event. Child timings are included in parent
elapsed times; do not sum both. A dash means the subtest is absent on main.

| Test/subtest | Main initial | Main task | Branch before | Branch after first | Branch after final |
| --- | ---: | ---: | ---: | ---: | ---: |
| **Package total** | 136.388 | 141.720 | 170.190 | 117.162 | 114.550 |
| `TestAuthHardeningConcurrentRefreshAndEmailIsolation` | 2.670 | 5.160 | 3.090 | 2.650 | 2.500 |
| `TestBackupWorkerRecoversSyntheticDatabase` | 11.210 | 15.480 | 11.940 | 9.840 | 9.100 |
| `TestE2EPlanEnvironmentChangesAndRejectViolations` | 12.920 | 2.300 | 44.060 | 1.790 | 1.690 |
| `TestE2EPlanEnvironmentChangesAndRejectViolations/rejects_mutated_plan_with_dropped_dependency_via_built_CLI` | 0.110 | 0.130 | 0.170 | 0.460 | 0.420 |
| `TestE2EPlanEnvironmentChangesAndRejectViolations/rejects_mutated_plan_with_public_engine_domain_via_built_CLI` | 0.120 | 0.110 | 0.180 | 0.430 | 0.430 |
| `TestE2EPlanEnvironmentChangesAndRejectViolations/rejects_mutated_plan_with_secret_literal_and_redacts_in_output` | 0.110 | 0.120 | 0.180 | 0.440 | 0.420 |
| `TestE2EPlanEnvironmentChangesAndRejectViolations/render_real_railway.ts_and_validate_via_built_CLI` | 0.140 | 0.120 | 0.170 | 0.460 | 0.420 |
| `TestE2EReadinessAndRollbackGateHTTP` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestE7ReviewCreationSubmissionSingleConnection` | 2.700 | 4.540 | 2.730 | 2.810 | 2.680 |
| `TestE7ReviewTimezoneWritesAndDeletion` | 5.550 | 6.310 | 5.400 | 5.250 | 5.160 |
| `TestE7ReviewTimezoneWritesAndDeletion//v1/me/profile` | 2.780 | 3.620 | 2.600 | 2.590 | 2.590 |
| `TestE7ReviewTimezoneWritesAndDeletion//v1/me/settings` | 2.770 | 2.690 | 2.800 | 2.660 | 2.560 |
| `TestEvalHTTPAuthorization` | 3.770 | 2.940 | 2.710 | 2.580 | 2.680 |
| `TestEvalHTTPAuthorization/admin_can_read_alerts` | 0.000 | 0.010 | 0.010 | 0.000 | 0.000 |
| `TestEvalHTTPAuthorization/admin_can_read_dashboards` | 0.010 | 0.010 | 0.010 | 0.010 | 0.010 |
| `TestEvalHTTPAuthorization/admin_can_read_dashboards/cost` | 0.000 | 0.010 | 0.010 | 0.010 | 0.000 |
| `TestEvalHTTPAuthorization/admin_can_read_dashboards/reliability` | 0.000 | 0.010 | 0.010 | 0.010 | 0.000 |
| `TestEvalHTTPAuthorization/admin_can_read_reports` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestEvalHTTPAuthorization/anonymous_cannot_read_alerts` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestEvalHTTPAuthorization/anonymous_cannot_read_dashboards` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestEvalHTTPAuthorization/anonymous_cannot_read_dashboards/cost` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestEvalHTTPAuthorization/anonymous_cannot_read_dashboards/reliability` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestEvalHTTPAuthorization/anonymous_cannot_read_reports` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestEvalHTTPAuthorization/forged_report_rejected_for_admin_session` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestEvalHTTPAuthorization/forged_report_rejected_for_anonymous` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestEvalHTTPAuthorization/forged_report_rejected_for_wrong_service_token` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestEvalHTTPAuthorization/internal_errors_are_redacted` | 0.010 | 0.010 | 0.010 | 0.000 | 0.010 |
| `TestEvalHTTPAuthorization/learner_cannot_read_alerts` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestEvalHTTPAuthorization/learner_cannot_read_dashboards` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestEvalHTTPAuthorization/learner_cannot_read_dashboards/cost` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestEvalHTTPAuthorization/learner_cannot_read_dashboards/reliability` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestEvalHTTPAuthorization/learner_cannot_read_reports` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestEvalHTTPAuthorization/oversized_report_rejected` | 0.030 | 0.050 | 0.040 | 0.040 | 0.040 |
| `TestEvalHTTPAuthorization/service_can_ingest` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestEvalHTTPAuthorization/service_token_cannot_read_alerts` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestEvalHTTPAuthorization/service_token_cannot_read_dashboards` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestEvalHTTPAuthorization/service_token_cannot_read_dashboards/cost` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestEvalHTTPAuthorization/service_token_cannot_read_dashboards/reliability` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestEvalHTTPAuthorization/service_token_cannot_read_reports` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestEvalHTTPAuthorization/stale_admin_session_rejected` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestExpireOldFixtureDumpsAndRestoreOverwrittenObject` | 5.170 | 8.510 | 5.130 | 5.210 | 5.000 |
| `TestFailedHTTPActionCorrelatesGRPCAndRiverAlert` | 23.500 | 26.170 | 23.040 | 23.880 | 24.370 |
| `TestGeneratedLessonPublishedBeforePlayback` | 4.330 | 3.030 | 3.020 | 2.740 | 2.870 |
| `TestHTTPAPI` | 4.830 | 5.120 | 4.640 | 4.680 | 5.170 |
| `TestHTTPAPI/anonymous_courses` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestHTTPAPI/courses` | 0.010 | 0.010 | 0.010 | 0.010 | 0.010 |
| `TestHTTPAPI/engine_error` | 0.110 | 0.110 | 0.080 | 0.060 | 0.060 |
| `TestHTTPAPI/engine_timeout` | 0.600 | 0.590 | 0.600 | 0.590 | 0.540 |
| `TestHTTPAPI/health` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestHTTPAPI/invalid` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestHTTPAPI/learner` | 0.000 | 0.000 | 0.000 | 0.000 | 0.010 |
| `TestHTTPAPI/missing_topic` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestHTTPAPI/preview` | 0.100 | 0.140 | 0.130 | 0.110 | 0.080 |
| `TestHTTPAPI/readiness` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestHTTPAPI/trailing_json` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestModelRoutingFailuresE2E` | 3.640 | 4.550 | 3.780 | 3.410 | 3.260 |
| `TestModelRoutingFailuresE2E/429_rate_limit_recovers_via_fallback_within_2-attempt_budget` | 0.140 | 0.130 | 0.120 | 0.080 | 0.070 |
| `TestModelRoutingFailuresE2E/5xx_provider_outage_on_primary_and_fallback_releases_all_reserved_credits` | 0.090 | 0.070 | 0.120 | 0.050 | 0.050 |
| `TestModelRoutingFailuresE2E/eval_package_routing_bounds_verify` | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 |
| `TestModelRoutingFailuresE2E/invalid_schema_returns_typed_error_and_releases_reservation` | 0.130 | 0.120 | 0.120 | 0.080 | 0.060 |
| `TestModelRoutingFailuresE2E/timeout_terminates_within_deadline_and_releases_reservation_in_full` | 0.600 | 0.590 | 0.630 | 0.550 | 0.560 |
| `TestNotesReviewForeignEnrichDoesNotBillOwner` | 3.480 | 3.130 | 2.620 | 2.640 | 2.640 |
| `TestNotesReviewPatchRequiresVersion` | 2.610 | 3.220 | 2.650 | 2.630 | 2.520 |
| `TestPreviewFinancialDelivery` | 2.930 | 3.320 | 3.190 | 3.780 | 2.860 |
| `TestPreviewFinancialDelivery/absent-usage` | 0.190 | 0.160 | 0.180 | 0.100 | 0.110 |
| `TestPreviewFinancialDelivery/cleanup` | 0.150 | 0.160 | 0.190 | 0.090 | 0.100 |
| `TestPreviewFinancialDelivery/deadline` | 0.160 | 0.230 | 0.150 | 0.140 | 0.120 |
| `TestPricingDraftAndApproveThroughHTTP` | 3.190 | 3.450 | 3.030 | 2.790 | 2.810 |
| `TestPricingDraftAndApproveThroughHTTP/anonymous_denial_audit_is_bounded` | 0.020 | 0.020 | 0.020 | 0.030 | 0.030 |
| `TestPricingDraftAndApproveThroughHTTP/cancelled_approved_draft_is_shown_cancelled` | 0.010 | 0.010 | 0.020 | 0.010 | 0.010 |
| `TestPricingE1210ActivateAndRollbackThroughHTTP` | 5.390 | 7.020 | 8.400 | 6.900 | 6.380 |
| `TestPricingE1210ActivateAndRollbackThroughHTTP/Analytics_events_emitted_without_reason_or_chat_text` | 0.100 | 0.100 | 0.100 | 0.100 | 0.100 |
| `TestPricingE1210ActivateAndRollbackThroughHTTP/Dev/test_mode_activates_interim_pricing_and_enables_actions` | 1.290 | 1.310 | 1.330 | 1.100 | 1.090 |
| `TestPricingE1210ActivateAndRollbackThroughHTTP/Forward_rollback_disables_design_chat_actions_while_preserving_catalogue` | 1.240 | 1.350 | 1.290 | 1.050 | 1.060 |
| `TestPricingE1210ActivateAndRollbackThroughHTTP/OwnerApprovedProductionLifecycle` | — | — | 2.110 | 1.550 | 1.530 |
| `TestPricingE1210ActivateAndRollbackThroughHTTP/Production_mode_rejects_unapproved_design_chat_pricing` | 0.230 | 0.280 | 0.230 | 0.170 | 0.120 |
| `TestPricingE12_07_RejectWrongStripePriceAndUneconomicCatalogueDraft` | 4.170 | 3.520 | 3.240 | 2.910 | 2.930 |
| `TestPricingE12_07_RejectWrongStripePriceAndUneconomicCatalogueDraft/Reject_standard_action_cost_above_ceiling_without_override` | 0.050 | 0.080 | 0.070 | 0.030 | 0.010 |
| `TestPricingE12_07_RejectWrongStripePriceAndUneconomicCatalogueDraft/Reject_uneconomic_catalogue_draft_below_70%_margin` | 0.060 | 0.050 | 0.060 | 0.020 | 0.020 |
| `TestPricingE12_07_RejectWrongStripePriceAndUneconomicCatalogueDraft/Reject_wrong_Stripe_price_amount` | 0.150 | 0.140 | 0.180 | 0.090 | 0.110 |
| `TestPricingE12_07_RejectWrongStripePriceAndUneconomicCatalogueDraft/Rejection_emits_analytics_and_writes_audit_evidence_without_private_reason` | 0.100 | 0.100 | 0.100 | 0.100 | 0.100 |
| `TestPricingE12_07_RejectWrongStripePriceAndUneconomicCatalogueDraft/Valid_draft_with_verified_Stripe_price_and_margin_>=_70%_can_be_approved` | 0.160 | 0.170 | 0.160 | 0.100 | 0.100 |
| `TestReliabilityAndCostDashboardsE2E` | 2.790 | 5.210 | 3.160 | 2.840 | 2.680 |
| `TestReliabilityAndCostDashboardsE2E/inject_failing_and_costly_calls_and_observe_deduplicated_alerts` | 0.360 | 0.320 | 0.400 | 0.290 | 0.250 |
| `TestRestoreDrillFinancialSnapshotReplaysMissingAndDuplicateHTTPNotifications` | 12.680 | 12.210 | 14.270 | 11.220 | 11.110 |
| `TestRestoreDrillRestoresSyntheticDumpAndFailsInjectedCorruption` | 12.550 | 12.370 | 13.390 | 12.380 | 12.150 |
| `TestStripePackHTTPAnalyticsAndPrivacy` | 5.070 | 2.920 | 5.460 | 2.990 | 2.760 |
