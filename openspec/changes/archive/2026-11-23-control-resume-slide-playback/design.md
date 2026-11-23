# Design

The parent owns elapsed slide time, playback rate and wall-clock watch time. Its bridge drives CSS/Web Animations in an opaque sandbox; only active-frame messages with an exact schema are accepted. Each slide mount uses a fresh iframe; stale events cannot mutate the new cursor. Pause retains time; replay seeks to zero and plays; seeking preserves pause state. Ended offers explicit Next/test and never completes a lesson.

GET/PUT /v1/lessons/{lesson}/position authorize enrollment and validate the version, slide ID and bounded integer time against the immutable manifest. Postgres stores one cursor per learner/lesson, not achievement progress. Writes are serialized by the client. Device session storage is learner/lesson scoped, cleared on logout and disposable. Offline/save failure retains the local cursor and shows recovery; durable offline reconciliation is deferred. Reopening pauses at the stored time until explicit Play. Safe internal return paths are passed by the originating path with a course-path fallback.

The new learner table cascades erasure and joins export, tombstone replay and 14-day encrypted nightly Postgres dumps. No slide content or learner text is persisted in cursors or analytics.
