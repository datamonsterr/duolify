## ADDED Requirements

### Requirement: Republish and update analytics events
The API SHALL emit `course_republished` (course_id, version, lesson_count, verdict) and `course_update_applied` (course_id, source_course_id, from_version, to_version, kept, added, archived). Properties MUST be UUIDs, enums and counts, never titles, changelog text or Usernames; opt-out MUST be honored and the catalogue MUST list both events before an emitter exists.

#### Scenario: Update emits
- **WHEN** a Learner updates a copy
- **THEN** `course_update_applied` is captured with counts only
