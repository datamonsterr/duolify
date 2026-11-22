## ADDED Requirements

### Requirement: Authored example courses
The system SHALL offer two authored, English, beginner example Courses (Product Management Foundations and JavaScript Essentials), each of 2 units and 6 Lessons with Motion HTML Slides, a Lesson test and a pre-generated retry pool, stored as Git-versioned fixtures. Every authored Lesson SHALL pass the same validators as a generated Lesson, and every JavaScript code example and printed-output answer SHALL be verified by running it. Serving content SHALL NOT call a model, the engine or any provider.

#### Scenario: Authored content is valid
- **WHEN** the authored fixtures are tested
- **THEN** every Lesson of every version passes the production Lesson, markup, manifest and digest validators
- **AND** every JavaScript snippet prints the output the Lesson shows

### Requirement: Example catalogue
`GET /v1/courses/examples` SHALL require a session and return `{"examples":[{"slug","title","description","level","units","lessons","estimated_minutes"}]}`, identical for every Learner, with `Cache-Control: private, no-cache`, `Vary: Authorization` and a strong ETag answered with 304 when unchanged.

#### Scenario: List the examples
- **WHEN** a signed-in Learner requests the catalogue
- **THEN** the two examples are returned with 6 Lessons each and a minutes estimate
- **AND** a repeat request with the ETag returns 304

### Requirement: Free idempotent example enrollment
`POST /v1/courses/examples/{slug}/enroll` SHALL give the Learner exactly one own copy of the example pinned to the authored version current at enrollment, return `{"course_id"}`, and make that Course active. A repeat call SHALL return the same Course and keep its pinned version; an unknown slug SHALL return 404. Enrollment, reading the Lessons and passing their tests SHALL reserve, spend and grant no Credits, SHALL NOT call the engine, and SHALL work at a zero balance. The copy SHALL be read by the lesson player through the same manifests as any Course.

#### Scenario: New learner at zero Credits
- **WHEN** a Learner with no Credits enrolls, reads the manifest and slides of all six Lessons in order and passes each test
- **THEN** no Credit ledger entry exists and the engine received no request

#### Scenario: Enroll twice
- **WHEN** the same Learner enrolls in the same example twice, even concurrently
- **THEN** both calls return the same Course id and exactly one copy exists

#### Scenario: A new authored version ships
- **WHEN** a newer version of an example is published
- **THEN** learners already enrolled keep reading the version they are pinned to
- **AND** new enrollments receive the newest version

### Requirement: Example labelling and completion
An example Course SHALL be labelled as an example wherever it is listed or opened, and course summaries and details SHALL carry `example_slug` and no creator attribution. After the last Lesson the Learner SHALL see a completion state with a "Create your own course" next step; no paid generation SHALL start by itself. Deleting an enrolled example follows normal Course deletion and the Learner may enroll again.

#### Scenario: Finish an example
- **WHEN** a Learner passes the sixth Lesson test
- **THEN** the path shows the completion state and a link to Create a course, and no Credits are spent
