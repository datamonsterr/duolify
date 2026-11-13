# E8-02: Stop and recover partially streamed replies

## Why
E8-01 persists an interrupted prefix when the stream breaks, but a Learner cannot explicitly stop a reply, the Engine reports transport failures as untyped gRPC errors, a stream that ends before its final usage frame has no bounded usage, and there is no way to recover a stored partial after a dropped connection.

## What Changes
- API: `POST /v1/courses/{id}/tutor/turns/{key}/stop` (owner-scoped, idempotent) cancels the in-flight Engine stream; `GET /v1/courses/{id}/tutor/turns/{key}` returns the owner's turn state and stored partial. Stop, client disconnect and Engine failure each settle the delivered prefix exactly once through `credits.SettleWith`.
- Stored reply status gains `stopped` (explicit Stop); disconnect and failures stay `interrupted`. Turn rows record a text-free `end_reason`, the reservation `job_id` and `usage_estimated` (migration `20261112080802`).
- Proto (additive): `Usage.estimated`, `ChatEvent.stream_error {code, retryable, delivered}`.
- Engine: typed stream errors (`first_token_timeout`, `idle_timeout`, `deadline_exceeded`, `provider_error`), one evaluated fallback only before the first delta, and a terminal bounded usage estimate when a stream ends or fails before its usage frame. Dropping the gRPC stream cancels the provider stream.
- Client: Stop reachable from send to end, server stop, partial recovery by reading the turn, idle-timeout retry, `Stopped` and `Interrupted` banners per S19/S09 with keyboard focus and reduced motion.
- Analytics `tutor_reply_stopped`, extended `tutor_reply_failed`, and `$ai_generation` with actual fallback, usage and `usage_estimated`; golden-set `tutor_chat` first-token/idle/cancel/failure cases; data inventory and privacy.

## Impact
API, engine, client, proto and one migration change together. Prices stay in the Pricing config.
