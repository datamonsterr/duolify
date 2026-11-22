## MODIFIED Requirements

### Requirement: Sharing data backup and retention
Visibility, versions, Share link hashes, invites and Course copies MUST be stored in Postgres and covered by the nightly dump. Snapshot assets MUST live in object storage with versioning where supported and be covered by the object storage protection in `data-backup`. Invites SHALL be purged 90 days after they are final. Sharing tables MUST be listed in the data inventory.

#### Scenario: Restore
- **WHEN** Postgres and the bucket are restored
- **THEN** every Course copy resolves its pinned version and assets

### Requirement: Shared Character data backup and retention
Published Cast/spec/rig/voice payloads, hashes, moderation suppression state and Course copy references MUST live in Postgres and nightly dumps. S3 rig/compiled SVG/narration assets MUST use versioning where supported and be included in nightly encrypted checksum-pinned protected copies and asset manifests; on R2 overwritten versions cannot be recovered from version history; restore MUST resolve each copy's pinned versions under `custom-characters` and `data-backup`. Payloads/assets MUST follow E15 last-reference plus 30-day retention; raw prompts and private screening data MUST never enter published backups as Course content. Deletion and takedown records MUST be reapplied before restored versions are served.

#### Scenario: Restore published Characters
- **WHEN** database and asset backups are restored after the creator's private library is deleted
- **THEN** enrolled copies resolve their original approved Character snapshots without needing that library or a paid model call

### Requirement: Owner-only preview and publishing guard
Only the Course owner SHALL read a draft or its verdict; everyone else MUST get the same neutral not-found as an unknown draft. Publishing MUST be rejected for non-owners, for accounts under the configured minimum age (default 18).

#### Scenario: Foreign read
- **WHEN** another Learner requests the draft
- **THEN** the response is the neutral `draft_unavailable`

### Requirement: Enrollment creates a Course copy
Enrolling in a Published course through a Share link, a Course invite or the Marketplace SHALL create a Course copy for the Learner, pinned to the current version, with progress, XP, Streak, Lesson tests, notes and Tutor chat belonging to that Learner only. Enrolling twice MUST NOT create a second copy. Published Lessons present in the pinned version MUST be served to the Learner at zero Credits. Unlocking order MUST still follow the Lesson test gate.

See `Enroll a pinned copy from a live link` for related detailed requirements.

#### Scenario: Reuse
- **WHEN** a Learner passes Lesson 2 of a Published course whose version has Lesson 3
- **THEN** Lesson 3 unlocks from the snapshot and no Credits are spent

#### Scenario: Double enroll
- **WHEN** a Learner opens a link for a Course they already have a copy of
- **THEN** the existing Course copy opens

### Requirement: On-demand generation beyond the published lessons
When a Learner unlocks a Lesson that the pinned version does not contain, the API SHALL generate it for that Learner through Gated generation, after the previous Lesson test is passed. The Learner (the enrolling Learner, not the owner) MUST pay the Credits for it at the active Pricing config prices for Lesson content and narration, following the reserve and settle rules in `credits`. A quote MUST be shown first. The Lesson MUST be stored in the Learner's Course copy only. The course page and enrollment screen MUST show how many Lessons are ready and that the rest are generated for Credits.

See `Generate the next missing Lesson in a Course copy` for related detailed requirements.

#### Scenario: Beyond the snapshot
- **WHEN** a Learner passes the last published Lesson and has enough Credits
- **THEN** a quote is shown, the next Lesson is generated and charged at the quoted Pricing config version

#### Scenario: Not enough Credits
- **WHEN** the balance is below the quote
- **THEN** the API refuses with `insufficient_credits` and the Engine is not called

#### Scenario: Owner's content untouched
- **WHEN** a Learner generates a Lesson beyond the snapshot
- **THEN** the Published course and its versions are unchanged

### Requirement: Republish and updates
The owner SHALL be able to republish, which creates version n+1 after safety screening, with at most 3 publishes per Course per day. Course copies MUST keep their pinned version. The client MUST show "update available" with the changelog. A Learner SHALL be able to update a Course copy, which re-pins it to the new version: completed Lessons MUST stay completed, matched by a stable lesson key, and Lessons removed upstream MUST stay in the Learner's history as archived. A Course copy forked by Course steering MUST NOT be offered updates in v1.

See `Stable lesson keys and explicit updates` for related detailed requirements.

#### Scenario: Learner keeps version
- **WHEN** the owner republishes and a Learner does nothing
- **THEN** the Learner continues on the old version unchanged

#### Scenario: Learner updates
- **WHEN** a Learner who completed Lessons 1 to 3 updates to a version that retitles Lesson 2
- **THEN** Lessons 1 to 3 remain completed and Lesson 4 comes from the new version

### Requirement: Course invite
An owner SHALL be able to invite a Username or a friend to a Course with visibility `link` or `public`. The recipient MUST get an in-app notification and a push notification when their settings allow it. Invites MUST be `pending`, then `accepted`, `declined`, `expired` after 30 days or `revoked`. Accepting MUST enroll the recipient. An owner MUST send at most 20 invites per day and have at most 1 pending invite per recipient per Course. The recipient's `course_invites_from` setting and blocks MUST be honored; a refused invite MUST NOT reveal the reason.

See `Invite delivery and push adapter` for related detailed requirements.

#### Scenario: Invite a friend
- **WHEN** an owner invites a friend and the friend accepts
- **THEN** the friend is enrolled in a Course copy

#### Scenario: Setting nobody
- **WHEN** the recipient's `course_invites_from` is `nobody`
- **THEN** the sender sees the invite as sent and the recipient gets nothing

#### Scenario: Private course
- **WHEN** the owner invites to a `private` Course
- **THEN** the client asks the owner to change visibility to `link` first
