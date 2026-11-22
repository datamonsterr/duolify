## 1. Tests first

- [x] 1.1 Unit with a fake Clerk client: deleted, already deleted (404, wrapped), rate limit, server error, bad key, network error; no deleter without a secret key; disabled eraser touches neither Clerk nor the database; worker schedules only with Clerk configured.
- [x] 1.2 API integration (Postgres): erasure while Clerk is down queues without calling Clerk and erases local data; failed retry counts attempts and keeps the request; later success drains it and a repeat is a no-op; email-only learner queues nothing; no Clerk configured calls nothing and loses nothing; tombstone replay queues.

## 2. Implementation

- [x] 2.1 Migration `20261122150000_clerk_user_deletion_queue.sql`.
- [x] 2.2 `clerk_deletion.go` (deleter, eraser, queue), erasure and tombstone replay, River job `clerk_user_deletion`, worker wiring in `cmd/api`.
- [x] 2.3 Nonproduction backups purge the table; `docs/data-inventory.md`, ADR 0016 note, `.env.example` comment.

## 3. Data and analytics

- [x] 3.1 Queue row removed on Clerk confirmation; no learner id stored; no analytics event.

## 4. Owner steps

- [ ] 4.1 Live check on staging: erase a test account that signed in with Google and confirm the Clerk user disappears within 10 minutes.

## 5. Independent security review

- [x] 5.1 Reproduce restore before linking, retry starvation and unsafe downgrade.
- [x] 5.2 Persist recoverable external deletion intents independently of database snapshots.
- [x] 5.3 Retry fairly with backoff and preserve pending identifiers on downgrade.
