# Design

## Authorized Course Enrollments and Active Selection
`GET /v1/courses` is protected by authenticated learner credentials. It queries Postgres `course_enrollments` joined with `courses`, `course_units` and `course_lesson_stubs`, returning enrolled courses ordered by creation/access with full confirmed units, ordered lesson goals, and level overrides.
The learner's active course is stored on `learner_profiles.active_course_id` (and `course_enrollments.last_active_at`).
Course selection is updated via `POST /v1/courses/{id}/select`. Foreign or un-enrolled course access returns 404 with neutral responses, preventing enumeration or status leakage.
On the client, `ShellSession` initializes and persists the active course in storage and restores it on application reload or restart, falling back to the enrolled course list when none is stored.

## Per-Course Level Override & Effective Profile Context Builder
`POST /v1/courses/{id}/level` allows learners to specify an override level (`beginner`, `intermediate`, `advanced`, `expert`) or reset to global (`""` or null). The value is stored in `course_enrollments.level_override`. Global `learner_profiles.level` is untouched.
The server provides `EffectiveProfile(ctx, learnerID, courseID)` which queries the global profile and overlays any non-empty `level_override` from the course's enrollment before returning `*enginev1.LearnerProfile` for downstream engine RPCs.
Setting or resetting a course level emits `learner_profile_updated` with `fields_changed: ["level"]`, respecting learner analytics opt-out and sending no private text.

## UI Parity with Canvases S03, S08, and S23
Course cards display confirmed edited titles, unit and lesson counts, progress bars and a current course marker.
The Learn path screen lists ordered units and lesson stubs with their titles and goals.
The Profile/Settings screen includes a "Per-course level" section reflecting whether each course uses the global level or an override, with actions to change or reset.
Desktop and mobile layouts maintain functional and accessibility parity, support dark/light modes and reduced motion, and provide loading, empty, error and retry states.

## Privacy, Backups, and Reliability
Database migration adds `level_override` and `last_active_at` to `course_enrollments` and `active_course_id` to `learner_profiles`.
These tables already cascade on learner deletion and are included in nightly encrypted dumps and data exports.
No new AI provider behavior is introduced; existing evaluation fixtures and gates remain untouched.
