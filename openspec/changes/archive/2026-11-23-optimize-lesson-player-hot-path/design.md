# Design
Integrity stays where authorized-lesson-manifest, cache-opened-lessons and accessible-static-lesson-recovery put it: manifests are strictly decoded and validated whenever the manifest object is read (cold cache or after TTL); every publisher (fixture, generated, shared copy) writes HTML digests (and asset digests when assets exist); each slide read validates the HTML again and refuses bytes whose digest differs from the manifest; the client verifies digests before caching a bundle. A slide without stored digests (legacy) is read once when its manifest is first loaded to derive them, and the result is cached.

Cache key: `version + manifest_object_key`; values are learner independent. Bound 256 entries (LRU), TTL 10 minutes, both configurable; a negative TTL disables the cache. The TTL bounds how long out-of-band tampering of a stored manifest object could go unnoticed in a running process; a restart or expiry re-reads and re-validates it. The cache is consulted only after the Postgres lookup that joins the learner's enrollment, so one learner can never be served another's lesson.

Position and sync validate slide id, time and version against the cached manifest, so they read an object only on the first request after a cold start. Every position write still re-checks enrollment and version in SQL.

Position heartbeat: 10 s while playing. Specs do not fix the cadence. Offline sync and local resume are unchanged.
