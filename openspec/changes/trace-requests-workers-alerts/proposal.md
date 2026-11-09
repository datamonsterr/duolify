# Trace requests, workers and operational alerts (E11-04)

## Why
Scaffold HTTP request IDs stop before gRPC and durable jobs. Backup retry logs
cannot correlate the originating action, and analytics must not be an alert sink.

## What Changes
- Carry server-generated request IDs and W3C span context through gRPC and River.
- Emit bounded, allowlisted structured span records without bodies or errors.
- Expose dependency readiness and operational counters independently of PostHog.
- Deduplicate backup exhaustion, queue and dependency alerts per incident.
- Harden the nonblocking analytics boundary, preserving learner opt-out.
- Keep retention and alert collection configuration in infra and update inventory.
- Repair the existing pricing administrator grant export/deletion classification
  discovered by the mandatory account privacy coverage check; verify cascade deletion.

## Impact
API, Rust engine and local infrastructure. No new learner event, product table,
proto field, prompt, model or provider behavior. Existing eval/fallback gates apply.
