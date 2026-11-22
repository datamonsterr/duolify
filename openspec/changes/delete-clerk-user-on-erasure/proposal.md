# Delete the Clerk user when a Duolify account is erased

Follow-up from `clerk-sso-sign-in` (ADR 0016, task 4.3). Erasing a Duolify account removed the learner and the Clerk binding but left the Clerk user, with its email and provider links, in Clerk. Erasure now also deletes that Clerk user.

## Impact

- **API:** Account erasure (`POST /v1/me/deletion-request`) and tombstone replay after a restore queue the learner's Clerk user id in `clerk_user_deletion_requests` in the same transaction that deletes the learner (the id would otherwise cascade away). The erasure response reports `clerk_user_deletion_queued`.
- **Worker:** A River periodic job (`clerk_user_deletion`, every 10 minutes, one attempt per run) deletes queued Clerk users through the Clerk Backend SDK (`user.Client.Delete`, `DELETE /v1/users/{id}`). A user Clerk reports as not found (404) counts as deleted. Any other failure keeps the request queued with an attempt count and the job returns an error naming the failure count, never an id.
- **Behaviour:** The learner's request never waits on Clerk. Local erasure never depends on Clerk. With no `CLERK_SECRET_KEY` the job is not scheduled and Clerk is never called; queued requests wait until Clerk is configured.
- **Data and backup:** New table `clerk_user_deletion_requests` (Clerk user id, request time, attempts, last attempt). It holds no learner id, is emptied from nonproduction backups, and rows are deleted once Clerk confirms. Data inventory updated.
- **Analytics:** none. **AI / Engine / proto:** none.
- **Migration:** `20261122150000_clerk_user_deletion_queue.sql`.
