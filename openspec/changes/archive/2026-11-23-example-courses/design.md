# Design

## Content
Two Courses, English, beginner, 2 units x 3 lessons, one idea per lesson. Each lesson has four slides (idea, concept, worked example, recap), 2 or 3 test questions (one per taught topic, four choices) and at least two retry variants per topic. Lesson outlines are in the authored files; the unit and lesson titles are:

- Product Management Foundations. Unit 1 What a product manager does: the product manager's job; users and problems; outcomes, not outputs. Unit 2 From idea to shipped: choosing what to build first; writing a clear problem statement; measuring success.
- JavaScript Essentials. Unit 1 Values and variables: values and types; let and const; strings, numbers and comparisons. Unit 2 Functions and control flow: functions; conditionals; arrays and loops.

Authoring: written in the engine's lesson contract (`title, slides{slide_id, html, narration, duration_ms, topic_ids}, test, variant_pool`) and then reviewed and edited as an author would (accuracy, wording, terminology, answers and distractors). Files are `services/api/internal/examples/content/<slug>-v<N>.json`; a published version is immutable and a content change adds `-v<N+1>.json` while the old file stays.

Validation (test time, every version of every course): the production Go validator `lessontest.ValidateGeneratedLesson`, `player.ValidateHTML`, `player.ValidateManifest` through `sharing.LessonObjects` (checksums and digests), plus a Go mirror of the engine's Rust slide, narration and variant rules (the engine cannot be edited or called from Go; the same files were also run through `engine_core::lessontest::validate_lesson` with a scratch crate). JavaScript: `javascript_snippets_test.go` runs every snippet with `node` and requires the code to appear in the referenced slide or question and the correct choice to equal the printed output.

## Serving
Chosen: a pinned copy without a source course. The existing mechanism for "one learner-owned copy of versioned lessons that the player reads through `lesson_manifests`" is `sharing.createCopy`; it is reused as `createExampleCopy`. A system-owned published course, draft and publication link was rejected: it needs a system learner row and a `visibility<>'private'` course that the Marketplace flag and the sharing legal gates (18+ publisher, screening, takedown) are written around. A copy needs none of that.

- `course_copies` gets `example_slug` and the provenance `example` (migration `20261124000000_example_courses.sql`); `source_course_id`, `source_owner_id` and `draft_id` stay NULL. A partial unique index `(learner_id, example_slug)` enforces one copy per learner and slug; a per-learner-and-slug advisory lock makes concurrent enrolls return the same course.
- Objects are immutable under `lessons/examples/<slug>/v<N>/<unit>-<lesson>/` (slides and a manifest with SHA-256 digests, built by the same `LessonObjects` as published lessons). Identical bytes under identical keys make installation idempotent and safe on two replicas: the API publishes every version's objects at start (best effort) and enrollment writes the pinned version's objects again before commit. The prefix is outside `lessons/published/` and `lessons/generated/`, so no purge job can remove it when one learner deletes their copy.
- Versioning: a copy records the version and content hash it was created from and its manifests point at that version's objects. A new version is a new file and new object prefix; existing enrollments keep reading their pinned version, new enrollments get the newest, and a repeat enroll returns the existing copy.
- The player, lesson test, progress, streak, XP, bookmarks, notes and chat read the copy exactly as any course; nothing is special-cased. Lessons 2 to 6 are `locked` until the previous test is passed, like every copy. The copy rows (`generated_lessons`, `lesson_slides` with topic ids, `lesson_questions`, `lesson_retry_pools`) are written at enrollment, so no generation and no Credit movement ever happens for authored lessons.
- Course summaries and the course detail carry `example_slug` (and no `copy_origin`), which the client uses for the "Example" label.

## API
`GET /v1/courses/examples` (authenticated) returns `{"examples":[{"slug","title","description","level","units","lessons","estimated_minutes"}]}`, identical for every learner. Cache policy: `private, no-cache` with `Vary: Authorization` and a strong ETag (revalidated, 304 when unchanged); it is behind a session, so a shared cache must not store it. `POST /v1/courses/examples/{slug}/enroll` returns `{"course_id"}` with 200, idempotent per learner, makes the course active, and 404 `example_not_found` for an unknown slug. The optional body `{"surface":"courses|learn_empty|onboarding"}` only labels the analytics event.

## Client
Courses page and Learn home empty state render the same `ExampleCourses` section (design-system card, "6 lessons · about N min · Free"). Enrolling navigates through the router to the course's learn path. The section is part of the lazy Courses route chunk and the Learn home block is a small additive component. After lesson 6 the learn path shows an example-complete card with a "Create your own course" link; no paid generation starts.

## Privacy, backup, analytics
Erasure and export follow `course_copies` (export gains `example_slug`; the table is erased with the learner). `course_copies.example_slug` is a closed slug and is on the anonymizer safe list. Authored objects are in the protected object backup manifest and in the restore-drill fixture. Lesson events carry no content-source field today, so none is added; `example_course_enrolled` carries the copy's `course_id` for joining.
