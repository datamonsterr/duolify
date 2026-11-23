# Design

## API contract
- `POST /v1/courses/{id}/tutor/quote` `{class: short|long, message}` → versioned quote (`tutor_short`/`tutor_long`); context tokens are measured server-side from the assembled context. Foreign or deleted course → 404.
- `POST /v1/courses/{id}/tutor/messages` `{quote_id, idempotency_key, class, message, thread_id?}` → `text/event-stream`:
  `thread {thread_id}`, `delta {text}`*, then `done {message_id, thread_id, replayed}` or `error {code, interrupted, message_id?}`.
  Pre-stream refusals are JSON: 401, 400 invalid, 404 foreign course/thread, 409 quote not accepted/expired/used or turn in progress, 402 `insufficient_credits` (no Engine call, nothing persisted, client keeps the draft), 502 engine failure with zero delivered content (reservation released).
- `GET /v1/courses/{id}/tutor/thread` → latest own thread with messages (`null` thread when none). `GET /v1/tutor/threads/{id}/messages`, `DELETE /v1/tutor/threads/{id}`; any foreign id → 404.

## Persistence (migration `…08xx`)
`tutor_threads` (learner, course), `tutor_messages` (role, content, status `complete|interrupted`), `tutor_turns` (idempotency key unique per learner, quote unique, state). Messages are written only inside the settlement transaction, so an undelivered turn leaves no user or assistant message. Deleting a thread removes its messages; account erasure cascades.

## Money and idempotency
Reserve (`ReserveAction`) → `Limits` → `Claim`. Turn row is inserted first (`ON CONFLICT`) so a duplicate key or reused quote cannot start a second charge; wallet exhaustion is enforced by the E7 financial lock. Before Claim failure uses `Release`; after Claim a no-content failure uses `Settle(owner, job, nil)` which returns every credit. Completed or interrupted turns replay from storage for the same key without an Engine call or charge. A deliberate resend needs a fresh key and quote.

Delivered content (complete or interrupted prefix) settles via `SettleWith` with normalised usage (`component=content`, `attempt=1`, `delivered=true`); interrupted usage is estimated from the delivered prefix and clamped to quote bounds. The action price is flat per E7 pricing config; no price is hard-coded.

## Context isolation and prompt safety
Context is queried only with the authenticated learner id and verified course ownership. Prompt layout: fixed instruction preamble, then one JSON object (`untrusted_data`) holding outline, profile, history and messages, with `<`,`>`,`&` escaped; data never joins the instruction text. Empty profile and empty history are explicit.

## Defaults chosen
Single course-level thread per learner and course; message ≤ 2000 runes (short) / 4000 (long); history truncated oldest-first to fit the Engine input envelope while keeping the order oldest-first.
