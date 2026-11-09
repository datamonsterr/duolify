# Bound and verify model routing failures (E10-03)

## Why
Unconditional scaffold retries and missing failure boundaries risk cost overruns and ambiguous failure handling. Launch requires typed bounded task policies with at most one repair/fallback, strict deadline enforcement, cancellation, and full undelivered reservation release.

## What Changes
- Replace unconditional retries with typed bounded task policies enforcing at most one repair/fallback after primary within attempt and USD ceilings, stopping with typed retryable errors.
- Enforce task, first-token, idle, and context deadlines with complete usage accounting on producer and consumer.
- Ensure cancellation on timeout, prevent post-delta fallback on streaming calls, and prevent unsafe output delivery.
- Release full undelivered reservations on terminal provider failures while retaining provider cost audit.
- Add sanitized `error_code` to `$ai_generation` analytics allowlist and catalogue.
- Verify canonical decimal OpenRouter aliases in models configuration.

## Impact
Touches engine, api, proto contract, analytics, and data-inventory documentation.
