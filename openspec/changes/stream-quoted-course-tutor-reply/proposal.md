# E8-01: Stream a quoted course tutor reply

## Why
The Tutor chat spec has no implementation. A Learner on the Course page needs a paid, quoted, streaming tutor turn grounded in the Course outline, the effective Learner profile and completed-Lesson history.

## What Changes
- API: course-level tutor quote, SSE send endpoint, thread/message read and per-thread delete. Reserve through E7 credits before the Engine call; persist messages and commit the charge in one transaction (`credits.SettleWith`).
- Engine: structured, injection-safe tutor prompt (instructions fixed; learner and course text delimited, JSON-escaped data); bounded first-token/idle/deadline routing already in `stream_bounded`.
- Mid-stream failure: persist exactly one interrupted delivered prefix as one assistant message, atomically with partial settlement. Zero delivered content: no message, full release.
- Client: tutor panel on the Course page (S19 desktop and mobile states), class and balance display, draft kept on refusal.
- Analytics `tutor_message_sent`, `tutor_reply_completed`, `tutor_reply_failed`; golden-set `tutor_chat` cases; data inventory, backup and privacy policy for new tables.

## Impact
API, engine, client and migrations change together. No `proto/` change: `ChatRequest` and `UsageLimits` already carry the context and deadlines.
