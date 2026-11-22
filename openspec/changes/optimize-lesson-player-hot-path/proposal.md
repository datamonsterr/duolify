## Why
Every lesson player call (`/v1/lessons/*`) fetched the manifest object, and a manifest read re-fetched and re-hashed every slide HTML and asset one by one (17 sequential object GETs for an 8-slide lesson with one asset per slide). The client also saved playback position every 2 seconds, each save paying the same cost. Predicates such as `lesson_id::text = $1` also bypassed the primary-key indexes.

## What Changes
- Authorization (owner, enrollment, deleted/blocked course) still runs in Postgres on every request. The decoded manifest is cached in a bounded in-process LRU keyed by the immutable manifest object key plus stored version, with a TTL; a warm read costs no object read, a cold read costs one GET.
- Manifest reads trust the SHA-256 digests stored in the manifest. Slide HTML and assets are read, validated and verified against those digests only when a slide is read. Narration audio routes no longer read the manifest.
- Malformed lesson, course and voice ids return 404 at the handler boundary; player and narration SQL use typed `$n::uuid` binds so primary-key indexes apply.
- Client: while playing, the server position heartbeat is 10 seconds instead of 2. Pause, seek, checkpoints, slide changes, hidden/pagehide and unmount still save immediately and every save still writes the local resume position.

## Impact
- Affected specs: lesson-player
- Affected code: services/api/internal/player, services/api/internal/narration, apps/client lesson player
- No schema, proto, analytics or AI changes. Data backup is unchanged: manifests, HTML and assets remain versioned objects under the existing backup policy. Model testing and reliability do not apply (no AI).
