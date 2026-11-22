# Example courses

## Why
A learner who has just signed up has no course, and creating one costs Credits and waits for generation. Two ready-made, free, good-quality example courses (Product Management Foundations and JavaScript Essentials) give them something immediately playable and show what a Duolify course feels like before they create their own.

## What Changes
- Author two English beginner Courses (2 units x 3 lessons each) as Git-versioned fixtures in the exact shape of generated lessons: Motion HTML slides, a lesson test and a pre-generated retry pool. Every lesson passes the production lesson validators in a test; every JavaScript snippet and "what does this print" answer is verified with `node` in a test. Content is deterministic: no model call at request time.
- `GET /v1/courses/examples` lists them; `POST /v1/courses/examples/{slug}/enroll` gives the learner their own free copy pinned to the authored version, idempotently, and makes it the active course. No Credits are reserved, spent or granted and the engine is never called.
- The Courses page and the Learn home empty state offer a "Start with an example" section. Enrolled examples behave like any other course (progress, streak, XP, tests, bookmarks, notes, tutor chat at normal Credit cost), are clearly labelled "Example", and end with a "Create your own course" next step.
- Analytics: `example_courses_viewed` and `example_course_enrolled` (closed enums).
- Privacy and backup: the copy is covered by export and erasure; authored objects are in the protected object backup and the restore drill; the new `course_copies.example_slug` column is classified in the anonymizer registry.

## Impact
Reuses the pinned-copy machinery (`course_copies`, `lesson_manifests`, player manifests with digests); the lesson player has no special case. New migration adds `course_copies.example_slug` and the `example` provenance. Specs touched: course-sharing, course-creation, analytics, data-backup. No engine, proto or pricing change.
