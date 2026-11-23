## MODIFIED Requirements

### Requirement: Sharing analytics events
The system SHALL emit `course_visibility_changed` (from, to), `course_published` (version, visibility, lesson_count), `course_republished`, `share_link_created`, `share_link_revoked`, `share_link_opened` (has_account, surface `app` or `web`), `course_invite_sent` (method `username` or `friend`), `course_invite_accepted`, `course_invite_declined`, `course_enrolled_published` (entry_point `link`, `invite` or `marketplace`, version), `published_lesson_reused`, `published_lesson_generated_on_demand` (credits, pricing_version) and `course_update_applied`. Events MUST NOT contain Share link tokens, Usernames of other Learners or Slide content.

#### Scenario: Enrollment event
- **WHEN** a Learner enrolls through a Share link
- **THEN** `course_enrolled_published` is emitted once with entry_point `link`
