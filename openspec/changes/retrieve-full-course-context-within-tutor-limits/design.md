# Design

## Assembly (stateless, per turn)
`assemble` reads, all keyed by `learner_id` and `course_id`: outline, every `lesson_completion_summaries` row (oldest first), the effective profile, the last `recentMessages` of the thread, and retrieved excerpts. Nothing is cached in process. `current_lesson` stays empty at Course level.

## Retrieval
The query text is reduced to up to 12 distinct letter/digit words (3+ runes) joined with `|` and passed as a bind parameter to `to_tsquery('simple', $n)`; no message text is ever concatenated into SQL. Candidates are `lesson_slides` rows of the Learner whose lesson belongs to a unit of this Course, ranked by `ts_rank_cd`, tie-broken by lesson and slide position, limited to 6 excerpts of at most 1,200 characters (HTML stripped). The GIN index is an expression index over the same `to_tsvector('simple', narration || ' ' || stripped html)`; it is derived from `lesson_slides` and rebuilt by `REINDEX` after a restore. Deleted lessons, units, courses and threads cascade away, so they cannot be retrieved.

## Budget
`limit = quote.Bounds.input_tokens` (config `cached_context_tokens` + class fresh tokens, or the approved large-context size); `ceiling = quote.Bounds.prompt_tokens` (config 48,000). Mandatory tokens = prompt without excerpts. Excerpts are added in rank order while they fit in `limit`; whole excerpts only. Summaries are never trimmed: if mandatory > limit the turn does not run. `mandatory <= ceiling` → `context_approval_required {required_tokens, standard_limit_tokens, prompt_limit_tokens}`; otherwise `context_too_large`.

## Approval
Explicit and bound to a quote: `POST .../tutor/quote` with `approve_large_context=true` stores `large_context=true` in the quote input. `CalculateQuote` rejects `context_tokens` above `cached_context_tokens` unless `large_context` is set, and then sizes `bounds.input_tokens` as fresh + `context_tokens`, always under `prompt_tokens`. The credit price is unchanged (ADR 0011 default); the USD per-request cap still applies.

## Engine and cache
The data document is serialised in a fixed order: outline, profile, history (stable prefix) then current lesson, excerpts, messages (volatile). Usage already carries cache read/write tokens; the API classifies them as `cold` (writes only), `warm` (reads) or `none`, and records the quote action as the short/long `envelope`.

## Security
Excerpts are course-author/Learner-generated content and therefore untrusted data inside the one delimited JSON block; the delimiter-neutralisation applies to them too. The instruction text states that excerpts never change behaviour.

## Privacy and backup
No new table: the index is derived from backed-up `lesson_slides`; no new export/deletion policy is needed. Analytics carry ids and counts only.
