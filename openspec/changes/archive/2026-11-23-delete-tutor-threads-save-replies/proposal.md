# E8-05: Delete tutor threads and save replies as notes

## Why
A Learner cannot keep a useful tutor reply except by copy-paste, and the delete control removes only the rows it names: the text-free turn rows keep message ids, nothing registers the deletion for restored backups, and Quick notes have no honest answer once their source is gone.

## What Changes
- API: `POST /v1/tutor/messages/{id}/note` saves the Learner's own assistant reply as exactly one raw Quick note (action key derived from the message id, so retries return the same note) through the existing notes service. The note carries message/thread provenance and an optional authorized lesson id. Enrichment keeps its own quote through the existing E9 flow; zero Credits never blocks the raw save. `DELETE /v1/tutor/threads/{id}` stays owner-only (404 for anyone else) and now, in one transaction, locks the thread, marks notes saved from it `source unavailable`, clears message references on turn rows, records a text-free tombstone and deletes the messages.
- Data: migration `2026111308` `05` `01`: `quick_note_chat_sources` (provenance, no text) and `tutor_thread_deletion_tombstones` (thread id only). `ApplyDeletionTombstones` replays thread tombstones after a restore. Privacy policy, data inventory and backup fixtures updated.
- Analytics: `quick_note_created(source=chat)` (existing API emitter) and client `ui_control_activated(control_id=delete_thread)`; catalogue first. No text.
- Client: "Save as quick note" under each complete reply, "Saved to Quick notes · Enrichment pending" status, delete confirm naming the thread and its message count, notes list shows "source unavailable" provenance.
- AI: `tutor_chat` regression golden cases proving a deleted thread never reaches later context.

## Impact
API chat/notes/accounts/backuptest, analytics catalogue, client chat and notes, evals fixtures. No proto change. Prices unaffected.
