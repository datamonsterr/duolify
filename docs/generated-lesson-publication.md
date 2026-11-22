# Generated lesson publication (#220)

The lessontest generation worker used to commit `generated_lessons` and
`lesson_slides` without publishing a player manifest or its objects. PR #219
bridged those DB-only lessons on the first authorized playback request.

Generation settlement now projects only the public slide fields through
`player.GeneratedObjects` and its HTML/manifest validation. It writes the slide
objects and then the manifest object **before opening the settlement transaction**.
Keys under `lessons/generated/<generation-id>/<deck-sha256>/` include a digest of
the ordered slide content, identifiers, timing and narration. Immutable S3 writes
accept retries only when the existing bytes match. A partial upload or DB rollback
can leave unreferenced objects, but cannot overwrite another deck; retry reuses the
same keys without requiring deletion.

`lesson_manifests` commits in `Financial.SettleWith` alongside the generated
content, pinned Cast, current lesson status, credit settlement and completed job.
An object-write or DB failure leaves the validated result checkpoint available;
retry publishes that saved result without another provider call. Existing manifest
rows win. Player reads of newly completed lessons need no publication writes.
Authorization, quote acceptance, credit calculation and grading are unchanged.

## Legacy repair and bridge removal

Keep the read-time bridge until the backfill has run in each environment. The API
`worker` command, unless `LESSON_TEST_TRACER_ENABLED=false` (default on, ADR 0017), enqueues the versioned
River kind `generated_lesson_publication_backfill_v1` in `generated-publication`.
This is a one-off startup job, not a periodic sweep. Concurrent active jobs are
deduplicated, and restarting the worker may enqueue another harmless run. River
retries failures up to ten attempts; starting a worker again can resume a repair
whose retries were exhausted.

The job scans missing manifests in pages of 100 and uses the same authorized,
unlocked, enrolled, undeleted and unblocked lesson checks as the bridge. It keeps
processing other lessons if an individual repair fails and reports the failures
for retry. Already published lessons are skipped. Locked or blocked lessons stay
hidden; if they later become accessible, the bridge remains available. The bridge
also writes content-addressed objects before its DB transaction and rechecks
authorization under row locks before inserting a manifest row.

Check River for a completed job and the `generated lesson manifest backfill
completed` worker log. This read-only query should return zero eligible missing
manifests before considering bridge removal:

```sql
SELECT count(*)
FROM generated_lessons g
JOIN course_lesson_stubs l ON l.id = g.lesson_stub_id
JOIN course_units u ON u.id = l.unit_id
JOIN courses c ON c.id = u.course_id
JOIN course_enrollments e ON e.course_id = c.id AND e.learner_id = g.learner_id
WHERE l.learner_id = g.learner_id AND c.learner_id = g.learner_id
  AND l.status <> 'locked' AND c.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM course_copies cc
                  WHERE cc.copy_course_id = c.id AND cc.blocked_at IS NOT NULL)
  AND NOT EXISTS (SELECT 1 FROM lesson_manifests m
                  WHERE m.lesson_id = g.lesson_stub_id);
```

No schema migration or provider/prompt change is needed. Existing database and
object backups cover the manifest references; generated manifests retain the
`lessons/generated/` prefix recognized by asset protection and restore. The
existing `lesson_generated` event occurs after publication and settlement; the
repair emits only operational worker logs, with no new learner analytics or paid
work. Provider eval behavior is unchanged.
