## ADDED Requirements

### Requirement: Course invite analytics events
The API SHALL emit `course_invite_sent` (method `username` or `friend`, course_id, invite_id), `course_invite_accepted` and `course_invite_declined` (course_id, invite_id), and `course_enrolled_published` with source `invite`. Properties MUST be UUIDs and enums, never Usernames or titles, and opt-out MUST be honored. The catalogue MUST list the events before an emitter exists.

#### Scenario: Accept emits enrollment
- **WHEN** a Learner accepts an invite
- **THEN** `course_invite_accepted` and `course_enrolled_published` with source `invite` are captured and nothing is sent for an opted-out Learner
