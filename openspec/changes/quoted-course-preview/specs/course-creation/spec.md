## ADDED Requirements

### Requirement: Confirmed brief preview consent
A paid personalized preview MUST use an owned unexpired confirmed brief revision and an accepted fresh quote bound to that revision. Brief edits MUST invalidate preview consent. Clarifying answers and quote acceptance alone MUST NOT generate or reserve. Insufficient paid Credits MUST cause zero Engine calls.

#### Scenario: Changed brief
- **WHEN** a preview request names a different revision from its accepted quote
- **THEN** the API rejects it before reserving or calling the Engine

#### Scenario: Paid preview delivery
- **WHEN** a Learner confirms the brief, reviews the versioned paid-only quote and explicitly consents
- **THEN** the API loads that exact revision and current profile, reserves through E7-03 and returns validated sample outline and sandboxed example Slides without Course or enrollment creation

### Requirement: Ephemeral preview reliability and recovery
The flow SHALL show Brief → Preview → Outline → Create with draft-preserving Back, discard, shop return and failure recovery. Regeneration SHALL require fresh quote and consent. The Engine SHALL validate nonempty outline titles/goals and safe slides, apply configured deadlines and at most the accepted bounded evaluated fallback within quote/USD limits. Terminal failure SHALL release undelivered Credits. Preview output SHALL remain ephemeral; existing 30-day confirmed brief and seven-year financial retention/export/deletion policies SHALL apply without a new learner table. Nightly backup and restore SHALL retain financial records and brief expiry/erasure behavior. API analytics SHALL emit course_preview_requested, course_preview_viewed and course_generation_failed with lengths, identifiers and reason codes only, honoring opt-out; each provider attempt SHALL report actual usage through $ai_generation. Offline course_preview Golden set cases SHALL include beginner, expert, empty and non-English profiles; live promotion remains gated by E10-06.

#### Scenario: Malformed primary
- **WHEN** the primary returns a structurally invalid preview
- **THEN** one evaluated fallback may run only within the accepted remaining cost and deadline

#### Scenario: Terminal failure or discard
- **WHEN** generation fails or a delivered preview is discarded
- **THEN** undelivered reservations are released on failure, the draft remains available and neither path creates Course or enrollment rows
