## ADDED Requirements

### Requirement: Listing eligibility and release flag
The API SHALL list a Course in the Marketplace only when the Marketplace release flag is on and the owner is an adult publisher with a verified email, an account at least 3 days old (config), and the latest publication draft passed screening with at least 3 generated Lessons (config), a title, a description, a category from the fixed taxonomy and a language. Listing caps (minimum age days, minimum Lessons, tags per Course, listings per owner) MUST be configuration. The flag MUST default to off; while off every Marketplace route MUST answer a neutral not-found and no Course may be listed.

#### Scenario: Ineligible owner
- **WHEN** an owner whose account is younger than the configured age lists a Course
- **THEN** the API refuses with `not_eligible` and the reason codes, and the Course stays out of the catalogue

#### Scenario: Flag off
- **WHEN** the release flag is off and anyone calls a Marketplace route
- **THEN** the response is a neutral 404 and `GET /v1/features` reports `marketplace_enabled:false`

### Requirement: Filtered, ranked, paginated catalogue
`GET /v1/marketplace/courses` SHALL return only listed, not delisted, public Courses, filtered by category and language and matched by Postgres full-text search over title, description and topic tags, sorted by `popular`, `new` or `rating`, with an opaque cursor and a bounded page size. `popular` MUST rank 30-day enrollments weighted by completion rate, `rating` MUST rank a Bayesian average with a configured minimum count and MUST accept Courses with zero ratings, and `new` MUST rank by publish date. Ties MUST be broken by a stable key so no Course repeats or is skipped across pages. Counts MUST be aggregate integers. A signed-in Learner MUST NOT see Courses of a user blocked either way.

#### Scenario: Search and filter
- **WHEN** a Learner searches "rust" with language `en` and sort `rating`
- **THEN** matching English Courses are returned best Bayesian rating first

#### Scenario: Pagination
- **WHEN** a Learner follows `next_cursor` until it is empty
- **THEN** every matching Course appears exactly once in the requested order

#### Scenario: Blocked creator
- **WHEN** a signed-in Learner who blocked (or was blocked by) an owner lists the catalogue
- **THEN** that owner's Courses are absent

### Requirement: Derived cache
Redis SHALL only cache result pages derived from Postgres. Losing Redis MUST rebuild on the next read, and a delist, relist or new listing MUST remove every cached page immediately.

#### Scenario: Delist leaves the cache
- **WHEN** a cached page contains a Course and the owner delists it
- **THEN** the cached page is deleted and the next read does not contain the Course

### Requirement: Marketplace Course page and free enrollment
`GET /v1/marketplace/courses/{id}` SHALL return the title, description, creator attribution (Username and avatar, or "deleted user"), category, language, version, publish date, outline, up to 3 allowlisted preview Slides, Lessons ready versus total, aggregate enrollment count and rating average and count, and SHALL state that Lessons beyond those ready are generated for Credits. A signed-in Learner MUST be able to enroll for free through the pinned-copy flow. There MUST be no paid Course and no revenue share.

#### Scenario: Open and enroll
- **WHEN** a Learner opens a listed Course and enrolls
- **THEN** preview Slides play without enrolling and enrollment creates one pinned copy at zero Credits

#### Scenario: Delisted or unknown
- **WHEN** the Course is delisted, unknown or hidden by a block
- **THEN** the API answers the same neutral 404
