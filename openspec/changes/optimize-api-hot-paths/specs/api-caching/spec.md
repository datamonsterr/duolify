## ADDED Requirements

### Requirement: Explicit cache policy per route
Every public, non-personal GET SHALL declare its own `Cache-Control` and an `ETag`, and SHALL answer a matching `If-None-Match` with 304 and no body. Every response that depends on the caller SHALL be `private`, never `public`; per-learner reads that benefit from revalidation SHALL use `private, no-cache` with `Vary: Authorization`, and all other routes SHALL stay `private, no-store`.

#### Scenario: Public feature flags
- **WHEN** a client requests `GET /v1/features`, `/v1/characters/default-cast`, `/v1/auth/sso/config` or `/v1/courses/shared-demo`
- **THEN** the response is publicly cacheable with an ETag, and repeating the request with that ETag returns 304

#### Scenario: Authenticated routes
- **WHEN** a learner requests any route that depends on the session
- **THEN** the response is never publicly cacheable, with or without a token

### Requirement: Course list is a summary
`GET /v1/courses` SHALL return, per enrolled course, the card fields (`id`, `title`, `description`, `level_override`, `pinned_version`, `moderation_blocked`, `copy_origin`) and a `progress` object (`lessons_total`, `lessons_done`, and `current` with `unit_index`, `lesson_index`, `unit_lessons`, or null), plus `active_course_id`, and SHALL NOT include units or lessons. The number of statements and the response size SHALL NOT depend on the number of lessons per course. The response SHALL be `private, no-cache` with an ETag and support `If-None-Match`.

#### Scenario: Large course list
- **WHEN** a learner with five courses of twenty lessons opens the course list
- **THEN** the response carries no lesson tree, takes two statements, and an unchanged repeat request returns 304
