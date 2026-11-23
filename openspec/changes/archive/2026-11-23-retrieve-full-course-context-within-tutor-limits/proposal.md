# E8-03: Retrieve full-course context within tutor limits

## Why
Tutor context today is the outline, the summaries that fit in whatever room is left (the oldest are silently dropped) and recent messages. In a large Course the tutor cannot answer about the first lessons, and an over-budget prompt only fails with an untyped `context_too_large`.

## What Changes
- API: the prompt is assembled statelessly per turn from Postgres: outline, **every** lesson summary (never dropped), effective profile, recent messages and a bounded set of source excerpts retrieved from the Learner's own generated lessons in this Course by Postgres full-text search (rebuildable GIN expression index, migration `20261112090801`; no new table, no new service).
- Budget: the standard envelope is the quote's `input_tokens` (Pricing config `cached_context_tokens` + class fresh tokens); the hard ceiling is Pricing config `prompt_tokens` (48,000). Retrieval fills only the room left after the mandatory parts. When the mandatory parts exceed the envelope but fit the ceiling, the API answers a typed `context_approval_required` (422). The Learner may approve explicitly: the quote request carries `approve_large_context`, the new quote records `large_context` and sizes its input bound (and therefore the USD cap) from the Pricing config. Above the ceiling the answer is `context_too_large` (422).
- Proto (additive): `ChatRequest.excerpts`.
- Engine: excerpts join the delimited untrusted data; the prompt orders stable canonical content before volatile content so provider prompt caching hits the prefix; injection text in a retrieved lesson stays data.
- Analytics: `tutor_reply_completed` and `$ai_generation` gain `envelope` and `cache_class` plus counts (`summary_count`, `excerpt_count`, `context_tokens`, cache tokens); never text.
- Client: context-too-large and approval states in the tutor, desktop and mobile, with focus management and reduced motion.
- Golden set: `tutor_chat` long-course retrieval, grounding, refusal, injection and cold-cache cases.

## Impact
API, engine, client, proto, one migration and pricing quote code change together. Prices stay in the Pricing config; the approval rule is recorded in ADR 0011 defaults.
