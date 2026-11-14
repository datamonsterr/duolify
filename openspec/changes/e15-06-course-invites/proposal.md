## Why
A Learner who owns a shared Course wants to invite a friend or a Username to it, and the invitee needs a quiet, private way to find and accept the invite. Invites ride on the E6 authorization contract (`CanInvite`, blocks, `course_invites_from`) and on the E15-03 pinned-copy enrollment. Push is only an adapter: per ADR 0012 there are no APNs/FCM credentials, so production delivers in-app and push is a fake in tests and dev.

## What Changes
- Table `course_invites` (+ `course_invite_deliveries` outbox). Learner-ID based, so a rename changes nothing. States `pending`, `accepted`, `declined`, `expired` (30 days, config), `revoked`.
- Owner routes `POST|GET /v1/courses/{id}/invites`; recipient routes `GET /v1/invites/received`, `POST /v1/invites/read`, `POST /v1/invites/{id}/accept|decline`; sender route `POST /v1/invites/{id}/revoke`. Actor always from the session.
- `CanInvite` runs in the transaction that writes the invite, after locking the pair. A learner whose `course_invites_from` refuses the sender (`nobody`, or a non-friend under `friends`) gets a neutral "sent" and nothing is delivered. An unknown, hidden, self or blocked-either-way username is the same neutral `invite_unavailable`, like search.
- Limits (config): 20 invites per sender per rolling day, one pending delivered invite per recipient and Course, 30-day expiry.
- Accept enrolls through the E15-03 `enrollCourse` (source `invite`), once and idempotently with an existing copy.
- Block callback registered with `social.OnBlock` cancels invites in both directions inside the block transaction.
- Delivery: in-app inbox always (the invite row, bounded list); push only through a `PushSender` adapter (disabled by default, fake in tests and dev). Dedupe by `(invite, channel)`, recipient settings (`course_invites_from`, new `course_invite_push`) and blocks are re-checked at send time, payload carries ids and an enum only.
- Analytics `course_invite_sent`, `course_invite_accepted`, `course_invite_declined`, `course_enrolled_published(source=invite)`.
- Data: tables in the nightly dump, final invites purged after 90 days, privacy export/erasure, data inventory, backup fixture, restore PII classification.
- Client: invite sheet on the Share screen, received invites inbox, per `docs/design/canvas/E15-Share.dc.html` and `S14-Friends.dc.html`, desktop and mobile.

## Out of scope
Live APNs/FCM transport (needs E15-12 credentials), invite links by email/SMS, group invites, bulk invites.
