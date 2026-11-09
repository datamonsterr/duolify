## ADDED Requirements

### Requirement: Course visibility
Every Course SHALL have a Course visibility of `private` (default, only the owner), `link` (anyone with the Share link) or `public` (listed in the Marketplace). Only the owner MUST be able to change it. Changing to `link` or `public` MUST publish a new version after safety screening. `public` MUST also require listing eligibility: title, description, category, language, at least the configured minimum of generated Lessons (default 3), and an owner account at least 3 days old with a verified email.

#### Scenario: Default
- **WHEN** a Learner creates a Course
- **THEN** its visibility is `private` and nobody else can open it

#### Scenario: Not eligible for public
- **WHEN** the owner sets `public` with only 1 generated Lesson
- **THEN** the API refuses with `not_listing_eligible` and lists the missing items

### Requirement: Versioned frozen snapshot
Publishing a Course SHALL create an immutable, numbered version containing the Course outline, every generated Lesson (Slide manifest, Motion HTML Slides, narration text, Lesson test questions and Retry variant pool), narration audio assets, a changelog and a content hash. A version MUST NOT change after creation. Later edits or new Lessons by the owner MUST NOT alter a published version.

#### Scenario: Owner generates more lessons
- **WHEN** the owner generates Lesson 6 after publishing version 1
- **THEN** version 1 still has Lessons 1 to 5 and Lesson 6 appears only after a republish

#### Scenario: Hash
- **WHEN** a version is read twice
- **THEN** the content hash is identical

### Requirement: Republish and updates
The owner SHALL be able to republish, which creates version n+1 after safety screening, with at most 3 publishes per Course per day. Course copies MUST keep their pinned version. The client MUST show "update available" with the changelog. A Learner SHALL be able to update a Course copy, which re-pins it to the new version: completed Lessons MUST stay completed, matched by a stable lesson key, and Lessons removed upstream MUST stay in the Learner's history as archived. A Course copy forked by Course steering MUST NOT be offered updates in v1.

#### Scenario: Learner keeps version
- **WHEN** the owner republishes and a Learner does nothing
- **THEN** the Learner continues on the old version unchanged

#### Scenario: Learner updates
- **WHEN** a Learner who completed Lessons 1 to 3 updates to a version that retitles Lesson 2
- **THEN** Lessons 1 to 3 remain completed and Lesson 4 comes from the new version

### Requirement: Snapshot privacy
A snapshot MUST be built from an allowlist of Course content only. It MUST NOT include the owner's Learner profile, Quick notes, Bookmarks, Tutor chat, progress, test answers, Course instructions or any other learner-specific data. A scanner MUST reject a snapshot that contains a field outside the allowlist and MUST flag emails, phone numbers and API keys in text. Owners MUST NOT be able to see which Learners enrolled; they SHALL see only aggregate counts.

#### Scenario: Notes present
- **WHEN** the owner has Quick notes on a Lesson and publishes
- **THEN** the snapshot contains no note text

#### Scenario: Email in a slide
- **WHEN** a Slide contains an email address
- **THEN** publishing is held for review with the finding

### Requirement: Share link
The owner SHALL be able to create a Share link for a Course with visibility `link` or `public`. The link MUST contain an unguessable token of at least 128 bits, stored only as a hash. Visitors without an account MUST be able to open a preview with title, creator Username, description, outline, up to 3 preview Slides, enrollment count and version, and the page MUST be marked `noindex`. Opening the link in the app MUST deep link to the Course. Enrolling MUST require an account. The owner MUST be able to revoke the link at once and rotate to a new token; revoked links MUST return not found.

#### Scenario: Preview without account
- **WHEN** a visitor opens a valid link in a browser
- **THEN** the preview is shown without any learner data and with a sign-up call to action

#### Scenario: Revoke
- **WHEN** the owner revokes the link
- **THEN** the next open of the old URL returns not found and enrolled Learners keep access

#### Scenario: App installed
- **WHEN** a signed-in Learner opens the link on a device with the app
- **THEN** the app opens the Course preview screen

### Requirement: Course invite
An owner SHALL be able to invite a Username or a friend to a Course with visibility `link` or `public`. The recipient MUST get an in-app notification and a push notification when their settings allow it. Invites MUST be `pending`, then `accepted`, `declined`, `expired` after 30 days or `revoked`. Accepting MUST enroll the recipient. An owner MUST send at most 20 invites per day and have at most 1 pending invite per recipient per Course. The recipient's `course_invites_from` setting and blocks MUST be honored; a refused invite MUST NOT reveal the reason.

#### Scenario: Invite a friend
- **WHEN** an owner invites a friend and the friend accepts
- **THEN** the friend is enrolled in a Course copy

#### Scenario: Setting nobody
- **WHEN** the recipient's `course_invites_from` is `nobody`
- **THEN** the sender sees the invite as sent and the recipient gets nothing

#### Scenario: Private course
- **WHEN** the owner invites to a `private` Course
- **THEN** the client asks the owner to change visibility to `link` first

### Requirement: Enrollment creates a Course copy
Enrolling in a Published course through a Share link, a Course invite or the Marketplace SHALL create a Course copy for the Learner, pinned to the current version, with progress, XP, Streak, Lesson tests, notes and Tutor chat belonging to that Learner only. Enrolling twice MUST NOT create a second copy. Published Lessons present in the pinned version MUST be served to the Learner at zero Credits. Unlocking order MUST still follow the Lesson test gate.

#### Scenario: Reuse
- **WHEN** a Learner passes Lesson 2 of a Published course whose version has Lesson 3
- **THEN** Lesson 3 unlocks from the snapshot and no Credits are spent

#### Scenario: Double enroll
- **WHEN** a Learner opens a link for a Course they already have a copy of
- **THEN** the existing Course copy opens

### Requirement: On-demand generation beyond the published lessons
When a Learner unlocks a Lesson that the pinned version does not contain, the API SHALL generate it for that Learner through Gated generation, after the previous Lesson test is passed. The Learner (the enrolling Learner, not the owner) MUST pay the Credits for it at the active Pricing config prices for Lesson content and narration, following the reserve and settle rules in `credits`. A quote MUST be shown first. The Lesson MUST be stored in the Learner's Course copy only. The course page and enrollment screen MUST show how many Lessons are ready and that the rest are generated for Credits.

#### Scenario: Beyond the snapshot
- **WHEN** a Learner passes the last published Lesson and has enough Credits
- **THEN** a quote is shown, the next Lesson is generated and charged at the quoted Pricing config version

#### Scenario: Not enough Credits
- **WHEN** the balance is below the quote
- **THEN** the API refuses with `insufficient_credits` and the Engine is not called

#### Scenario: Owner's content untouched
- **WHEN** a Learner generates a Lesson beyond the snapshot
- **THEN** the Published course and its versions are unchanged

### Requirement: Unpublish and deletion
The owner SHALL be able to set visibility back to `private`, which MUST stop new enrollment, remove the Course from the Marketplace and revoke Share links. When the owner deletes a Course, Course copies MUST keep access to their pinned version and its assets. Snapshot assets MUST be purged 30 days after the last Course copy referencing them is gone. When the owner deletes their account, creator attribution MUST become "deleted user", personal data MUST be removed, and snapshots MUST be kept for enrolled Course copies.

#### Scenario: Owner deletes the course
- **WHEN** the owner deletes a Course with 40 Course copies
- **THEN** the 40 Learners still open and finish their pinned version

#### Scenario: Last copy removed
- **WHEN** the last referencing Course copy is deleted
- **THEN** the snapshot is queued for purge and removed after 30 days

### Requirement: Sharing abuse limits
The API MUST enforce, per account and configurable: at most 10 Published courses, 3 publishes per Course per day, 20 invites per day, 30 enrollments per day, and per-IP rate limits on the public preview endpoint. Exceeding a limit MUST return a rate-limit error and write an abuse signal for moderators.

#### Scenario: Enrollment spam
- **WHEN** an account enrolls in 31 courses in a day
- **THEN** the 31st request is refused with a rate-limit error

### Requirement: Sharing analytics events
The system SHALL emit `course_visibility_changed` (from, to), `course_published` (version, visibility, lesson_count), `course_republished`, `share_link_created`, `share_link_revoked`, `share_link_opened` (has_account, surface `app` or `web`), `course_invite_sent` (method `username` or `friend`), `course_invite_accepted`, `course_invite_declined`, `course_enrolled_published` (source `link`, `invite` or `marketplace`, version), `published_lesson_reused`, `published_lesson_generated_on_demand` (credits, pricing_version) and `course_update_applied`. Events MUST NOT contain Share link tokens, Usernames of other Learners or Slide content.

#### Scenario: Enrollment event
- **WHEN** a Learner enrolls through a Share link
- **THEN** `course_enrolled_published` is emitted once with source `link`

### Requirement: Sharing data backup and retention
Visibility, versions, Share link hashes, invites and Course copies MUST be stored in Postgres and covered by the nightly dump. Snapshot assets MUST live in object storage with versioning and be covered by the object storage protection in `data-backup`. Invites SHALL be purged 90 days after they are final. Sharing tables MUST be listed in the data inventory.

#### Scenario: Restore
- **WHEN** Postgres and the bucket are restored
- **THEN** every Course copy resolves its pinned version and assets

### Requirement: Sharing model testing and reliability
On-demand Lesson generation MUST use the same Golden set gate, fallback, timeout and cost limits as `course-creation` and `ai-reliability`; a failed generation MUST release the reservation and keep the previous Lessons available. Publish-time safety screening is specified in `marketplace` and applies to `link` visibility as well.

#### Scenario: Generation fails for a Course copy
- **WHEN** all providers fail while generating a Lesson beyond the snapshot
- **THEN** the Learner is not charged and can retry
