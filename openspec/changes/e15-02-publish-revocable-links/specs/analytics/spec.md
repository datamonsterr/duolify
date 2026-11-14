## ADDED Requirements

### Requirement: Sharing analytics events
The system SHALL emit:
- `course_visibility_changed` (from: string, to: string, course_id: UUID) — emitted by API on visibility change
- `course_published` (course_id: UUID, version: int, visibility: enum, lesson_count: int) — emitted by API on first publish
- `share_link_created` (course_id: UUID) — emitted by API when a link is created; NO token
- `share_link_revoked` (course_id: UUID) — emitted by API when a link is revoked; NO token
- `share_link_opened` (course_id: UUID, has_account: bool, surface: "web") — emitted by API on public preview; NO token, NO IP

Events MUST NOT contain Share link tokens, plaintext token, Usernames of other Learners, Slide HTML or answers.

#### Scenario: Course published
- **WHEN** an owner publishes a Course with a passing verdict
- **THEN** `course_visibility_changed` and `course_published` and `share_link_created` are each emitted exactly once with no token or Course text

#### Scenario: Link revoked
- **WHEN** an owner revokes the active share link
- **THEN** `share_link_revoked` is emitted with only the course_id; no token or plaintext appears

#### Scenario: Preview opened
- **WHEN** any visitor opens the public preview URL
- **THEN** `share_link_opened` is emitted with course_id, has_account and surface="web"; no token or IP is recorded
