## ADDED Requirements

### Requirement: Immutable screened publication draft
An owner SHALL be able to build a publication draft of their own Course: an immutable numbered snapshot with a SHA-256 content hash containing the outline, generated Motion HTML Slides, Lesson test questions, the private Retry pool and optional narration references. Narration audio MUST NOT be included while `SHARING_INCLUDE_NARRATION` is false (default, ADR 0012). The builder MUST reject any field outside the allowlist (profile, notes, bookmarks, chat, answers, progress, Course instructions) and MUST flag emails, phone numbers and API keys as findings without storing the matched text. A draft MUST NOT change after creation.

#### Scenario: Private data in a source row
- **WHEN** the Course owner has Quick notes and Tutor chat
- **THEN** the snapshot contains none of that text and the scanner reports no finding for it

#### Scenario: Email in a Slide
- **WHEN** a Slide contains an email address
- **THEN** the draft verdict is at least `needs_review` with a `personal_data` finding at `lesson N · slide M` and no email text

### Requirement: Screening verdict fails closed
The API SHALL call `ScreenCourse` with a bounded platform-funded sample of at most 12 Lessons and 60000 input tokens, a 120 second deadline and one evaluated fallback. The verdict MUST be `pass`, `needs_review` or `block`. Any error, timeout, malformed or out-of-envelope response, or cost above the USD cap MUST store `pending_screening`. Private learning MUST be unaffected, and no public link is created.

#### Scenario: Engine unavailable
- **WHEN** the engine returns an error
- **THEN** the draft is `pending_screening`, the Course stays fully usable and the owner can retry

#### Scenario: Hidden payload
- **WHEN** a deep Slide hides an instruction to ignore policy
- **THEN** the verdict is `block` or `needs_review`, never `pass`

### Requirement: Owner-only preview and publishing guard
Only the Course owner SHALL read a draft or its verdict; everyone else MUST get the same neutral not-found as an unknown draft. Publishing MUST be rejected for non-owners, for accounts under the configured minimum age (default 18) and, in this change, for everyone with `visibility_unavailable`.

#### Scenario: Foreign read
- **WHEN** another Learner requests the draft
- **THEN** the response is the neutral `draft_unavailable`

### Requirement: Design parity
The owner preview MUST match `docs/design/canvas/E15-Share.dc.html` on desktop (dialog) and mobile (sheet) with loading, empty, offline, error, long-content, keyboard focus, light/dark and reduced-motion states; tokens win over canvas values.

#### Scenario: Screening pending
- **WHEN** the draft is `pending_screening`
- **THEN** the panel says learning is unaffected and offers Retry screening
