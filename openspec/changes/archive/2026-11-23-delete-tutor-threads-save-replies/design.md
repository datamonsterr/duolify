## Context
Notes (E9) and threads (E8-01) exist. Deletion today removes `tutor_threads` (cascading messages) and leaves `tutor_turns` message ids dangling.

## Decisions
- Provenance lives in `quick_note_chat_sources(note_id PK -> quick_notes ON DELETE CASCADE, learner_id, thread_id, message_id, lesson_id, unavailable)`, not in `quick_notes`, so sqlc and the E9 contract stay untouched. Inserted in the same transaction as the note.
- Idempotency: action key `tutor-message:<message id>`; the notes upsert already dedupes per Learner and action key.
- Races: save takes `FOR SHARE` on the thread, delete takes `FOR UPDATE`, so a note is either saved before the delete (and marked unavailable) or refused after it.
- Delete tx: lock thread, `UPDATE quick_note_chat_sources SET unavailable=true, thread_id=NULL, message_id=NULL`, null message ids on `tutor_turns`, insert tombstone, delete thread. Later context assembly and `GET /turns/{key}` find no text.
- Tombstones keep only the thread uuid and a deletion time and are kept 35 days (backup rotation), then purged. On restore `ApplyDeletionTombstones` re-deletes any resurrected thread and re-marks sources.
- Replies above 2,000 characters are truncated to 2,000 characters at a rune boundary; the response says `truncated`.
- Only the Learner's own assistant replies can be saved; others are 404.
