## ADDED Requirements

### Requirement: Browse and search
The Marketplace SHALL list only Courses with visibility `public` that passed safety screening and are not delisted. A Learner MUST be able to browse by category (fixed taxonomy), filter by language, search by text over title, description and topic tags, and sort by `popular`, `new` or `rating`. Results MUST be paginated with a cursor. `popular` MUST rank by enrollments in the last 30 days weighted by completion rate; `rating` MUST use a Bayesian average with a configured minimum count; `new` MUST use publish date.

#### Scenario: Search and filter
- **WHEN** a Learner searches "rust" with language `en` and sort `rating`
- **THEN** matching public Courses in English are returned best rated first

#### Scenario: Delisted course
- **WHEN** a Course is delisted
- **THEN** it no longer appears in any list or search result

### Requirement: Course page
Each listed Course SHALL have a page with title, description, creator attribution (Username and avatar, or "deleted user"), category, language, version and publish date, the Course outline, up to 3 preview Slides, Lessons ready versus total, enrollment count, rating average and count, and reviews. Enrollment counts MUST be aggregate integers. The page MUST state that Lessons beyond those ready are generated for Credits.

#### Scenario: View
- **WHEN** a Learner opens a Course page
- **THEN** all fields above are shown and preview Slides play without enrolling

### Requirement: Ratings and reviews
A Learner SHALL be able to rate a Course 1 to 5 stars and optionally write a review up to 2,000 characters only after completing at least N Lessons of that Course (config, default 3) through their Course copy. A Learner MUST have at most one review per Course, MAY edit or delete it, and the review MUST record the version reviewed. An owner MUST NOT review their own Course. Review text MUST be screened for abuse before it is shown and MUST be limited to 20 reviews per account per day.

#### Scenario: Too early
- **WHEN** a Learner who completed 1 Lesson tries to review
- **THEN** the API refuses with the number of Lessons still needed

#### Scenario: Edit
- **WHEN** a Learner edits their review
- **THEN** the aggregate rating updates and the review keeps one entry

#### Scenario: Owner reviews own course
- **WHEN** an owner tries to review their Course
- **THEN** the API refuses

### Requirement: Report flow
Any Learner SHALL be able to report a Course, a review or a user with a reason (`unsafe`, `inappropriate`, `spam`, `copyright`, `misleading`, `other`) and optional text. A reporter MUST have at most one open report per target and at most 10 reports per day. When distinct reporters reach the configured threshold (default 3) the Course MUST be hidden from listings until a moderator decides. The reporter MUST NOT be revealed to the owner.

#### Scenario: Threshold
- **WHEN** a third distinct Learner reports a Course
- **THEN** it is hidden from listings and enters the moderation queue with high priority

#### Scenario: Duplicate report
- **WHEN** the same Learner reports the same Course twice
- **THEN** the second is merged into the first

### Requirement: Moderation queue and takedown
Moderators (role `moderator`) SHALL see a queue ordered by severity then age, with a first-response target of 48 hours. Actions MUST be `dismiss`, `warn`, `delist`, `takedown` and `ban_publishing`, each requiring a reason and written to an append-only audit log. A takedown MUST delist the Course, revoke its Share link and block new enrollment; for severe reasons (`unsafe`, legal) existing Course copies MUST also be blocked, otherwise they keep access. The owner MUST be notified with the reason and MAY appeal once.

#### Scenario: Normal takedown
- **WHEN** a moderator takes down a spam Course
- **THEN** it is delisted and enrolled Learners keep access to their version

#### Scenario: Severe takedown
- **WHEN** a moderator takes down a Course for unsafe content
- **THEN** existing Course copies are blocked and Learners see a notice

#### Scenario: Appeal
- **WHEN** an owner appeals once
- **THEN** a different moderator reviews it and a second appeal is refused

### Requirement: AI safety screening before publishing
Every version published with visibility `link` or `public` MUST be screened before it goes live: deterministic checks (URL denylist, personal data patterns, size) and a classifier over title, description, outline and sampled Lesson and narration text, returning `pass`, `needs_review` or `block` with category codes. `block` MUST refuse publishing. `needs_review` MUST hold the version in the moderation queue. `pass` MUST go live. If screening is unavailable the version MUST stay `pending_screening` (fail closed) while the owner's private learning continues. Screening cost MUST be paid by the platform and bounded per version (sampled Lessons and token cap).

#### Scenario: Unsafe course
- **WHEN** the owner publishes a Course teaching weapon assembly
- **THEN** screening returns `block` and the Course stays `private`

#### Scenario: Borderline
- **WHEN** a Course on chemical safety is borderline
- **THEN** it goes to the moderation queue and is not listed until approved

#### Scenario: Screener unavailable
- **WHEN** the classifier times out
- **THEN** the version is `pending_screening` and publishing retries automatically

### Requirement: Free courses in v1
All Courses in the Marketplace SHALL be free to enroll in v1. Learners MUST still pay Credits for Lessons generated on demand beyond the published ones. Paid Courses and creator revenue share MUST NOT be implemented in this change.

#### Scenario: Enroll
- **WHEN** a Learner enrolls from the Marketplace
- **THEN** no payment or Credits are taken at enrollment

### Requirement: Marketplace analytics events
The system SHALL emit `marketplace_browsed` (category, language, sort), `marketplace_searched` (query_length, result_count, filters), `marketplace_course_viewed` (course_id, source), `review_submitted` (rating, version), `review_deleted`, `course_reported` (reason, target_type), `course_hidden_by_reports`, `moderation_action` (action, reason_code, target_type), `moderation_appeal` (outcome), `course_screening_completed` (verdict, category_codes, latency_ms) and `$ai_generation` for each screening call. Events MUST NOT contain search text, review text or report text.

#### Scenario: Search event
- **WHEN** a Learner searches the Marketplace
- **THEN** `marketplace_searched` has length and count and no text

### Requirement: Marketplace data backup and retention
Listings, ratings, reviews, reports, moderation actions and screening results MUST be stored in Postgres and covered by the nightly dump. Reports and moderation actions SHALL be retained 3 years and screening results 1 year. When a Learner deletes their account, review text and report text MUST be removed and the rating MUST stay in the aggregate without attribution.

#### Scenario: Account deleted
- **WHEN** a reviewer deletes their account
- **THEN** their review text and name disappear and the course average is unchanged

### Requirement: Marketplace model testing and reliability
Safety screening MUST have Golden set cases in the Hugging Face dataset for `screen_course` and `screen_review`: safe courses in several languages, borderline educational topics (for example chemistry safety, history of war), unsafe courses, courses with a hidden unsafe payload deep in later Lessons, spam and phishing links, prompt-injection text inside Slides, and abusive reviews. `mise run engine:eval` MUST fail below the thresholds in `evals/thresholds.toml`; proposed starting values are recall on hard-block categories at least 0.98, false-block rate on safe and borderline-educational cases at most 0.05, and injection-in-content resistance at least 0.99. Course screening SHALL time out in 120 seconds as a background job and review screening in 10 seconds, with at most one fallback attempt and a per-version cost cap. Usage MUST be reported for every call.

#### Scenario: Hidden payload
- **WHEN** a golden case hides unsafe instructions in Lesson 9 of 12
- **THEN** sampling plus the classifier flags it or the eval fails

#### Scenario: Fallback
- **WHEN** the primary provider fails during screening
- **THEN** the fallback provider is used once and `fallback_used` is recorded
