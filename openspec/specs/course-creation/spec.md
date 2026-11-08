# course-creation Specification

## Purpose
Let a Learner create a Course on any topic with an AI agent. The Learner sees a Course preview first, then the agent generates only the Course outline. Lesson content is generated later, one Lesson at a time, under Gated generation.

## Requirements

### Requirement: Course preview before generation
The system SHALL show a Course preview (a sample Course outline and example Slides) for a topic before the Learner commits to creating a Course. The API MUST call the Engine `PreviewCourse` RPC and MUST NOT create a Course record during preview.

#### Scenario: Learner requests a preview
- **WHEN** a Learner submits a topic and asks for a preview
- **THEN** the API returns a sample Course outline and example Slides
- **AND** no Course or enrollment row is created

#### Scenario: Learner discards the preview
- **WHEN** a Learner leaves the preview without confirming
- **THEN** nothing is persisted except the preview analytics event

### Requirement: Outline-only generation
After the Learner confirms, the system SHALL generate the Course outline only (Units and Lesson titles with goals). The Engine `GenerateCourseOutline` RPC MUST NOT return Slides or Lesson tests.

#### Scenario: Learner confirms a preview
- **WHEN** a Learner confirms course creation
- **THEN** the API stores the Course, its Units and its Lesson stubs with status `locked`
- **AND** the first Lesson is marked `ready_to_generate`
- **AND** the Learner is enrolled in the Course

#### Scenario: Outline response contains lesson content
- **WHEN** the Engine returns an outline that contains Slides
- **THEN** the API MUST reject the response and retry or fall back

### Requirement: Gated lesson generation
The system SHALL generate Lesson N+1 only after the Learner passes the Lesson test of Lesson N. The API MUST refuse to call `GenerateLesson` for a Lesson whose predecessor has no recorded pass.

#### Scenario: Previous test not passed
- **WHEN** a client asks to generate a Lesson and the previous Lesson test is not passed
- **THEN** the API responds with a conflict error and the Engine is not called

#### Scenario: Previous test passed
- **WHEN** the Learner passes the Lesson test of Lesson N
- **THEN** the API enqueues a generation job for Lesson N+1
- **AND** progress is streamed to the client through SSE

### Requirement: Generation is a background job
Generation SHALL run as a River job, never on the request path. A job MUST be idempotent per Lesson: a retry MUST NOT create a second Lesson or charge Credits twice.

#### Scenario: Worker restarts mid-generation
- **WHEN** the worker restarts while a Lesson job runs
- **THEN** the job resumes or retries and the Lesson exists exactly once
- **AND** the Credits reservation is settled once

### Requirement: Credits for generation
Course preview, outline generation and Lesson generation SHALL reserve Credits before the Engine call and settle them with the reported `Usage`, as defined in the `credits` spec.

#### Scenario: Insufficient credits
- **WHEN** a Learner has fewer Credits than the estimate
- **THEN** the API refuses the request with `insufficient_credits` and the Engine is not called

### Requirement: Course analytics events
The API SHALL emit the PostHog events `course_preview_requested`, `course_preview_viewed`, `course_outline_generated`, `course_enrolled`, `lesson_generation_requested`, `lesson_generated` and `course_generation_failed` with the properties defined in the `analytics` spec.

#### Scenario: Outline generated
- **WHEN** an outline is stored
- **THEN** `course_outline_generated` is emitted with `course_id`, `unit_count`, `lesson_count`, `provider`, `model` and `latency_ms`

### Requirement: Course data backup and retention
Course, Unit, Lesson stub, enrollment and generated Lesson rows MUST live in Postgres and be covered by the nightly dump. Slide HTML MUST live in object storage with versioning. Deleting a Course MUST soft-delete first and hard-delete after 30 days.

#### Scenario: Restore drill
- **WHEN** the restore drill restores the latest dump
- **THEN** every Course has its Units and Lesson stubs, and Slide object keys resolve in storage

### Requirement: Course model testing and reliability
Course preview and outline generation MUST have Golden set cases (task `course_preview`, `course_outline`) and MUST pass `mise run engine:eval`. The Engine SHALL apply a per-call timeout, retry once, then fall back to the OpenRouter provider. The outline MUST pass deterministic checks: valid structure, no empty titles, no Slides.

#### Scenario: Primary provider times out
- **WHEN** the Claude API call exceeds the timeout twice
- **THEN** the Engine retries on the fallback provider and reports it in `Usage.provider`

#### Scenario: Eval gate
- **WHEN** a prompt change lowers a task score under its threshold in `evals/thresholds.toml`
- **THEN** `engine:eval` fails
