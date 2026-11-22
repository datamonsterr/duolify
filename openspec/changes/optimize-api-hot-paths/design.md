## Pricing snapshot

`RuntimeCatalogue` keeps the last validated catalogue and when it was confirmed against Postgres. A request inside the TTL (5 s) serves it with no SQL. The first request after expiry reloads it; concurrent requests keep serving the previous snapshot meanwhile (`TryLock`, single flight). A failed reload keeps the last good catalogue, raises `pricing_config_load_failed` and is retried once per TTL, not once per request. A loaded version that differs from the served one is exposed only after its activation marker exists, as before. `ActivateDue` publishes the version it activates immediately.

Freshness: replicas do not notify each other, so a replica that did not activate a version shows it within the TTL. The browser and edge add `Cache-Control: public, max-age=10`, so a scheduled flip reaches a client within about 15 s plus clock skew. The pricing specs state no numeric bound; the previous 60 s `max-age` was already looser than that. Quotes never read this snapshot; they resolve the active version in Postgres at quote time.

## Cache policy

| Route | Policy | Why |
|---|---|---|
| `GET /v1/pricing` | `public, max-age=10`, ETag | Same catalogue for everyone; bounded by the snapshot TTL. |
| `GET /v1/features` | `public, max-age=60`, ETag | Process-wide release flags. |
| `GET /v1/characters/default-cast` | `public, max-age=60`, ETag | Compiled-in fixture. |
| `GET /v1/auth/sso/config` | `public, max-age=60`, ETag | Deployment config; the publishable key is public. |
| `GET /v1/courses/shared-demo` | `public, max-age=300`, ETag, 304 | Embedded fixture. |
| `GET /v1/objects/sha256-*` | `public, max-age=31536000, immutable` | Content-addressed (unchanged). |
| `GET /v1/courses` | `private, no-cache`, ETag, `Vary: Authorization` | Per learner; always revalidated, 304 when unchanged. |
| marketplace, shares preview, health, stream-check | `private, no-store` | Marketplace depends on the viewer; share preview is token gated; probes must not be cached. |
| everything else | `private, no-store` | Default. |

An authenticated response is never `public`; a test walks representative per-learner routes with and without a token.

## Course list

Progress is computed in one statement from the lesson stubs with window functions (`unit_index` and `lesson_index` are positions among the non-archived units and the unit's lessons), so statements and bytes no longer grow with lessons per course. The web client turns a full tree (an offline copy) into the same summary on its side.

## Creation stream

No notification helper exists, so the stream backs off: 200 ms first, doubling to 3 s, reset to 200 ms on a state change. Postgres LISTEN/NOTIFY was not added: the worker that finishes a creation runs in another process and needs a dedicated listener connection per API replica, which is more than this change should carry.
