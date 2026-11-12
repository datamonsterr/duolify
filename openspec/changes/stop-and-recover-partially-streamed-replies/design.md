# Design

## Stop and disconnect
The send handler registers an in-flight record keyed `(learner, idempotency key)` with a cancel-with-cause. `POST .../tutor/turns/{key}/stop` looks up only the caller's own key, so another Learner's key is a miss (404, nothing cancelled). A hit cancels with cause `stopped` and waits (bounded) for the handler to finish settlement, then answers `{state, message_id, status}` from storage. A miss returns the stored turn state (completed/interrupted/stopped: no-op) or 202 `pending` when the stream lives on another API instance (the client also aborts its connection, which cancels there). The handler classifies the end once: normal EOF is `completed` even when a stop arrives late; cause `stopped` is `stopped`; request-context cancellation is `disconnect`; anything else is an Engine failure with its typed code. Client order is: POST stop, then abort the fetch, so a stop is never misread as a disconnect.

## Settlement
Content delivered: one assistant message (`complete`, `stopped` or `interrupted`), the user message, the turn state and the settlement commit in one `SettleWith` transaction guarded by `tutor_turns.state='pending'`; a second finisher loses with `errTurnLost`. Zero content: no message, `Settle(nil)` returns the whole reservation and the turn keeps a text-free `released` tombstone, so a repeated or late owner Stop answers 200 `released` (never charging), the same key resent answers 409 `released` (a deliberate resend needs a new key and quote), and unknown or foreign keys stay 404. Usage for a cut reply is the Engine's terminal estimate when it arrived, otherwise the API estimate clamped to the quote; both are flagged `estimated`.

## Engine
`stream_bounded_routed` returns the stream plus the route actually used. Errors carry a typed `StreamFailure`. `chat::reconcile` wraps the stream: it counts delivered bytes, forwards deltas, converts a failure after or before output into a `stream_error` event, and ends every stream with exactly one usage frame (provider-reported, or estimated and bounded by the quote limits and flagged). Fallback only happens before the first delta, never a second reply.

## Recovery and privacy
The partial is readable only by its owner (`learner_id` filter, 404 otherwise). Analytics carry ids, reason, counts and latency only, never text. `end_reason`, `job_id` and `usage_estimated` are text-free and ride the existing nightly dump; settlement rows keep their financial retention.

## Defaults chosen
Stop with zero delivered content stores nothing and refunds in full. Retry after Stop or interruption is a deliberate new send: fresh quote, fresh key. A stopped or interrupted reply stays in the thread and is part of later context.
