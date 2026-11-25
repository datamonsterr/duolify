## MODIFIED Requirements

### Requirement: Outline-only generation
After explicit final cost consent, the system SHALL create an outline-only River job from the latest learner-confirmed edited outline revision. Ordered Units, Lesson titles and goals, additions and removals MUST be preserved exactly. Unapplied proposals MUST NOT enter the job. The private GenerateCourseOutline request MUST carry the edited outline, draft revision, trusted quote bounds and the draft's clarification answers. The model SHALL return only a list of Lesson plans keyed by confirmed Lesson ID. The Engine SHALL attach the validated plans to the unchanged request outline and return it. Each plan SHALL have a plain-text `objective`, 2 to 4 `key_points`, `prerequisite_lesson_ids` (earlier Lessons only), 1 to 3 `misconceptions`, an `example_hint` and `review_lesson_ids` (earlier Lessons only). The model SHALL write all plan text in the brief language, falling back to the learner locale and then to English. English objectives MUST start with an observable verb. Every objective MUST be non-empty plain text. Model output that includes outline fields, omits or adds a plan, duplicates a Lesson ID, references a later or unknown Lesson, or contains Slides, Lesson tests or HTML MUST be rejected within the bounded evaluated fallback policy. Plans SHALL be stored immutably on the Lesson stubs.

#### Scenario: Confirmed edited outline creates a course
- **WHEN** the learner accepts the revision-bound quote and confirms Create
- **THEN** validated success atomically stores the Course, ordered Units, locked Lesson stubs with their Lesson plans and one enrollment
- **AND** only the first Lesson is ready_to_generate and no lesson content is generated

#### Scenario: Model returns an outline or incorrect plans
- **WHEN** the model returns outline fields or its plans fail to cover the confirmed Lesson IDs exactly once
- **THEN** the output is rejected and the bounded fallback runs

#### Scenario: Plan points forward
- **WHEN** a Lesson plan lists a prerequisite or review Lesson that comes later in the outline
- **THEN** the output is rejected

#### Scenario: Stale or malformed output
- **WHEN** the revision changed before submission or the output includes lesson content
- **THEN** stale submission is rejected before provider work or invalid output fails bounded validation
- **AND** terminal failure releases undelivered reserved Credits and leaves no partial enrollment

### Requirement: Gated lesson generation
The system SHALL generate Lesson N+1 only after the Learner passes the Lesson test of Lesson N. The API MUST refuse to call `GenerateLesson` for a Lesson whose predecessor has no recorded pass. The `GenerateLesson` call MUST include the Learner's active Course instructions as defined in the `course-steering` capability, the Lesson plan when present, the Course lesson length, at most the 5 most recent history summaries and the Learner's weak topics. The generated Lesson MUST record which instructions it used. The Engine SHALL render the request as compact JSON of these fields only; usage limits and deadlines MUST NOT appear in prompt text.

#### Scenario: Previous test not passed
- **WHEN** a client asks to generate a Lesson and the previous Lesson test is not passed
- **THEN** the API responds with a conflict error and the Engine is not called

#### Scenario: Previous test passed
- **WHEN** the Learner passes the Lesson test of Lesson N
- **THEN** the API enqueues a generation job for Lesson N+1 with its plan and the Course lesson length
- **AND** progress is streamed to the client through SSE

#### Scenario: Active instructions
- **WHEN** the generation job for Lesson N+1 starts and the Learner has active instructions
- **THEN** the Engine call carries them and the Lesson stores their ids

#### Scenario: Course created before plans
- **WHEN** a Lesson stub has no plan
- **THEN** generation proceeds from the Lesson goal alone

## ADDED Requirements

### Requirement: Lesson teaching structure
Every generated Lesson SHALL follow the teaching contract in the always-on lesson prompt core, in every prompt mode: one objective (the plan objective, else the Lesson goal); the first Slide recalls a prerequisite topic or opens with a hook question; concepts are followed by a worked example before any check; the last Slide summarises; each wrong choice reflects a named misconception and every question explains why the right answer is right; at most 40 visible words per Slide; narration explains the Slide and does not read it word for word. The Slide and question counts SHALL follow the Course lesson length.

#### Scenario: Plan with misconceptions
- **WHEN** a Lesson plan lists misconceptions
- **THEN** the generated test's wrong choices draw on them

#### Scenario: Static quality warning
- **WHEN** a generated Lesson exceeds 40 visible words on a Slide or narration pace is outside 110 to 200 wpm
- **THEN** the Lesson is still delivered and a `quality_warnings` code is recorded on `$ai_generation`

### Requirement: Outline clarifying questions
The Course preview SHALL return 0 to 3 clarifying questions, each with an id, text and up to 4 quick-reply options, only when the outline depends on something the confirmed brief does not settle (prior knowledge of a prerequisite, the intended end result, depth on an ambiguous topic). The prompt skill `outline_clarify` SHALL carry these rules. The client SHALL show the questions in the outline review chat with Skip. An answer SHALL be sent as an outline-chat request, producing a quoted proposal the Learner accepts or discards; all answers SHALL be stored on the draft and passed to the planner as data. Clarifying questions MUST NOT be required to create a Course.

#### Scenario: Brief settles everything
- **WHEN** the brief states level, goal and included topics clearly
- **THEN** the preview returns no clarifying questions

#### Scenario: Learner answers
- **WHEN** a Learner answers "I already know variables" to a prior-knowledge question
- **THEN** an outline proposal removing or shortening the basics Lesson is quoted and shown as a diff
- **AND** the outline changes only if the Learner accepts it

#### Scenario: Learner skips
- **WHEN** the Learner skips every question
- **THEN** Course creation proceeds and the planner receives no clarifications

### Requirement: Lesson length per Course
Each Course SHALL store a lesson length of `short` (3 Slides, 2 questions), `medium` (5 Slides, 3 questions) or `long` (8 Slides, 5 questions), seeded from the brief and defaulting to `medium`. The Learner SHALL be able to change it in Course settings; the change applies only to Lessons not yet generated.

#### Scenario: Length changed
- **WHEN** a Learner changes a Course from `medium` to `short`
- **THEN** the next generated Lesson has 3 Slides and 2 test questions
- **AND** already generated Lessons are unchanged

### Requirement: Credit estimate for one Lesson
The preview, the outline confirmation step and Course settings SHALL show the estimated Credits for one Lesson of the Course: the `lesson_content` price for the Course lesson length plus the `narration` price when narration is on, from the active pricing version. The estimate SHALL update when the length or narration setting changes. It is an estimate, not a quote, and MUST NOT reserve Credits.

#### Scenario: Estimate shown
- **WHEN** a Learner opens Course settings
- **THEN** the screen shows "About N Credits per Lesson" for the current length and narration setting

#### Scenario: Length changes the estimate
- **WHEN** the active pricing version has different `lesson_content` prices per length and the Learner picks `long`
- **THEN** the estimate shows the `long` price
