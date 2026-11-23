# course-sharing Specification

## Purpose
Let a Learner publish a Course as an immutable, screened, versioned snapshot, share it by link or invite, and let others enroll in a pinned copy that reuses the published Lessons and generates further Lessons at their own cost.
## Requirements
### Requirement: Publisher birth date and adult gate
Publishing, republishing and rotating a Share link MUST require an account birth date proving the owner is at least 18. The API SHALL compute calendar age from the server's UTC date; client ages and signup age confirmations MUST NOT establish publishing eligibility. A missing date MUST return `birth_date_required`; a known under-18 date MUST return `adult_required`, without changing visibility. The first-publish desktop dialog and mobile sheet SHALL offer a labelled, keyboard-accessible birth-date form on `birth_date_required`, announce validation and under-18 errors, and resume the requested action after saving. Existing screening and publication restrictions MUST still apply.

Only a nullable date (no time, age, document or extra verification metadata) SHALL be stored on the account. An authenticated learner MAY set their own date once through `PUT /v1/me/birth-date`, accepting only `birth_date` in strict `YYYY-MM-DD` form, a real date between `1900-01-01` and the server's current UTC date inclusive. The database write MUST update only an account whose birth date is NULL, including under concurrent requests. A different date submitted after the first save MUST return HTTP 409 with `birth_date_locked`; the identical date MAY return HTTP 204 idempotently. Corrections MUST go through support. On `birth_date_locked`, the client SHALL calmly explain that a date is already on the account and corrections go through support, without offering another date submission. Minors SHALL see the existing under-18 refusal without a retry-with-another-date path. February 29 birthdays MUST become eligible on March 1 in a non-leap anniversary year. The birth date MUST be included in account export, erased with account deletion and restored only subject to the existing deletion-ledger replay. Encrypted account backups SHALL include it; anonymized restore drills SHALL classify it as PII and replace it with NULL. Neither birth date nor age SHALL enter analytics, published content, AI input or logs; this flow SHALL add no analytics event or AI behavior and SHALL preserve existing screening reliability gates.

#### Scenario: Missing birth date
- **WHEN** an owner without a stored birth date first tries to publish
- **THEN** the server returns `birth_date_required` and the client asks for the date before continuing

#### Scenario: Minor attempts to publish
- **WHEN** an owner whose eighteenth birthday is tomorrow tries to publish
- **THEN** the server returns `adult_required`, the client explains the 18+ requirement, and the Course remains private

#### Scenario: Adult continues safely
- **WHEN** the owner saves a valid birth date and today is their eighteenth birthday in UTC
- **THEN** the client resumes the action and the existing screening and publish restrictions determine the outcome

#### Scenario: Minor cannot replace a saved birth date
- **WHEN** a learner refused as under 18 submits an older birth date
- **THEN** the API returns HTTP 409 with `birth_date_locked`, preserves the original date, and publishing still returns `adult_required`; the client explains support corrections without another date-entry or retry action

#### Scenario: First-save retry
- **WHEN** a learner retries the identical birth date after the initial save
- **THEN** the API returns HTTP 204 without changing the stored date; simultaneous submissions of different dates can save only one date

#### Scenario: Privacy lifecycle
- **WHEN** a learner exports and then deletes their account, or an anonymized restore drill runs
- **THEN** export contains the birth date, deletion removes it including after deletion-ledger replay, and the anonymized database contains no birth dates

### Requirement: Course visibility
Every Course SHALL have a Course visibility of `private` (default, only the owner), `link` (anyone with the Share link) or `public` (listed in the Marketplace). Only the owner MUST be able to change it. Changing to `link` or `public` MUST publish a new version after safety screening. `public` MUST also require listing eligibility: title, description, category, language, at least the configured minimum of generated Lessons (default 3), and an owner account at least 3 days old with a verified email.

#### Scenario: Default
- **WHEN** a Learner creates a Course
- **THEN** its visibility is `private` and nobody else can open it

#### Scenario: Not eligible for public
- **WHEN** the owner sets `public` with only 1 generated Lesson
- **THEN** the API refuses with `not_listing_eligible` and lists the missing items

### Requirement: Versioned frozen snapshot
Publishing a Course SHALL create an immutable, numbered version containing the Course outline, every generated Lesson (Slide manifest, Motion HTML Slides, narration text, Lesson test questions and Retry variant pool), narration audio references (content hashes of the owner's ready narration; ADR 0013), a changelog and a content hash. A version MUST NOT change after creation. Later edits or new Lessons by the owner MUST NOT alter a published version.

#### Scenario: Owner generates more lessons
- **WHEN** the owner generates Lesson 6 after publishing version 1
- **THEN** version 1 still has Lessons 1 to 5 and Lesson 6 appears only after a republish

#### Scenario: Hash
- **WHEN** a version is read twice
- **THEN** the content hash is identical

### Requirement: Republish and updates
The owner SHALL be able to republish, which creates version n+1 after safety screening, with at most 3 publishes per Course per day. Course copies MUST keep their pinned version. The client MUST show "update available" with the changelog. A Learner SHALL be able to update a Course copy, which re-pins it to the new version: completed Lessons MUST stay completed, matched by a stable lesson key, and Lessons removed upstream MUST stay in the Learner's history as archived. A Course copy forked by Course steering MUST NOT be offered updates in v1.

See `Stable lesson keys and explicit updates` for related detailed requirements.

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
The system SHALL emit `course_visibility_changed` (from, to), `course_published` (version, visibility, lesson_count), `course_republished`, `share_link_created`, `share_link_revoked`, `share_link_opened` (has_account, surface `app` or `web`), `course_invite_sent` (method `username` or `friend`), `course_invite_accepted`, `course_invite_declined`, `course_enrolled_published` (entry_point `link`, `invite` or `marketplace`, version), `published_lesson_reused`, `published_lesson_generated_on_demand` (credits, pricing_version) and `course_update_applied`. Events MUST NOT contain Share link tokens, Usernames of other Learners or Slide content.

#### Scenario: Enrollment event
- **WHEN** a Learner enrolls through a Share link
- **THEN** `course_enrolled_published` is emitted once with entry_point `link`

### Requirement: Sharing data backup and retention
Visibility, versions, Share link hashes, invites and Course copies MUST be stored in Postgres and covered by the nightly dump. Snapshot assets MUST live in object storage with versioning where supported and be covered by the object storage protection in `data-backup`. Invites SHALL be purged 90 days after they are final. Sharing tables MUST be listed in the data inventory.

#### Scenario: Restore
- **WHEN** Postgres and the bucket are restored
- **THEN** every Course copy resolves its pinned version and assets

### Requirement: Sharing model testing and reliability
On-demand Lesson generation MUST use the same Golden set gate, fallback, timeout and cost limits as `course-creation` and `ai-reliability`; a failed generation MUST release the reservation and keep the previous Lessons available. Publish-time safety screening is specified in `marketplace` and applies to `link` visibility as well.

#### Scenario: Generation fails for a Course copy
- **WHEN** all providers fail while generating a Lesson beyond the snapshot
- **THEN** the Learner is not charged and can retry

### Requirement: Shared copies reuse the owner's narration audio

A version SHALL reference the owner's ready narration audio by content hash. A Course copy MUST play that audio, with its caption and alignment data, without new narration generation and without any Credit quote, reservation or charge. Regeneration MUST happen only for a slide whose audio is not referenced or whose asset is missing, at the existing per-learner pricing. Audio MUST be served only to the Learner holding the copy and MUST stop when the copy is blocked by a severe takedown; an overturned takedown MUST restore it. Assets MUST stay deduplicated by content hash and be retained while any copy or enrollable version needs them, then removed by the narration retention policy.

#### Scenario: Learner plays reused audio for free
- **WHEN** a Learner enrolls in a published Course whose owner narration exists
- **THEN** the copy holds ready narration rows for the referenced slides, no Credits move, and only the copy's Learner can fetch the audio

#### Scenario: Audio is missing
- **WHEN** a referenced asset is missing or purged at enrollment
- **THEN** that slide has no reused row and narration is generated for the Learner under the existing quote and pricing

#### Scenario: Severe takedown
- **WHEN** a severe takedown blocks a copy
- **THEN** its reused audio is no longer served and its reference is released

### Requirement: Published Character snapshot allowlist
Publishing or republishing SHALL snapshot the approved Cast and Characters attached to the Course, including each generated Lesson's effective Cast, with immutable Character specs, schema/rig/voice-config versions and required rig/narration assets. The publish flow MUST disclose that Character name, bio, backstory, speaking style, persona and appearance become Course content and MUST require confirmation before export. E15's snapshot allowlist MUST include only those content fields, not raw creation prompts, owner identifiers, private library links, screening records or unrelated Characters. Exported fields and appearance MUST pass screening; failure/unavailable screening MUST block publication of the new version without affecting private learning.

#### Scenario: Publish with a custom guide
- **WHEN** the owner confirms the Character disclosure and the new version passes screening
- **THEN** the frozen Course version includes its guide's approved spec/assets and the existing E15 content hash covers them

#### Scenario: Raw prompt in export
- **WHEN** a snapshot candidate contains a Character creation prompt or a private library owner ID
- **THEN** the allowlist scanner rejects publication and no private field becomes visible

#### Scenario: Unsafe Character during publish
- **WHEN** attached Character data fails current screening or the screener is unavailable
- **THEN** the new version stays unpublished and existing approved Course versions remain unchanged

### Requirement: Pinned Character reuse and Course copy boundaries
A Course copy MUST reuse its published version's Cast and Character assets at zero `character_create` cost, including as input to on-demand generation beyond published Lessons. Such new Lessons MUST retain E15's Lesson/narration quotes and charges. Library edits/deletion MUST NOT change a published Character snapshot; republishing MUST create a new Course version and existing copies MUST remain pinned under E15 update rules. An unforked Course copy MUST refuse Cast overrides and explain the published Cast/steering-fork choices. After the existing confirmed Course steering fork, the API SHALL permit the Learner to select an owned Cast for future jobs, but prior Lessons MUST remain unchanged.

#### Scenario: Owner changes the Character
- **WHEN** the owner changes a guide's appearance after publishing version 1
- **THEN** copies pinned to version 1 keep the old appearance and only a republish can expose a newer Character version

#### Scenario: Beyond published Lessons
- **WHEN** a Learner funds the next on-demand Lesson beyond the published snapshot
- **THEN** the job uses the pinned published Cast and charges Lesson/narration prices without Character creation Credits

#### Scenario: Override refused before fork
- **WHEN** a Learner tries to replace the Cast of an unforked Course copy
- **THEN** no selection changes and the client explains that the published Cast remains until the existing confirmed steering fork

### Requirement: Shared Character deletion and safety takedown
Deleting or unpublishing the source Character/Course MUST stop future private selection/new enrollment as appropriate but MUST preserve approved snapshot content while enrolled Course copies reference it, following E15. Creator account deletion MUST remove personal ownership data and pseudonymize attribution without exposing library records. Unsafe Character takedowns MUST suppress the affected appearance/persona in existing copies and use an approved Default cast fallback without changing grading; immutable stored payloads MUST NOT be silently rewritten. Unreferenced published payloads/assets MUST purge 30 days after the last reference ends.

#### Scenario: Creator account deleted
- **WHEN** the creator deletes their account while other Learners retain Course copies
- **THEN** private Character records and personal ownership data follow account deletion while the approved content-only published payload remains with pseudonymous attribution

#### Scenario: Character safety takedown
- **WHEN** a published Character is marked unsafe
- **THEN** runtime rendering/persona resolution suppresses it in favor of a safe default and existing test results stay unchanged

### Requirement: Shared Character analytics events
The API SHALL emit PostHog `character_snapshot_published` (course_version, character_count, custom_count) once for each successfully published version containing a Cast. Published reuse MUST retain existing E15 events; Character default substitution MUST use the relevant rendering/tutor/voice fallback event from the `analytics` delta. Events MUST respect opt-out and exclude Character content, raw prompts and private ownership data.

#### Scenario: Snapshot event
- **WHEN** publication with a Cast succeeds
- **THEN** `character_snapshot_published` reports the Course version and aggregate Character counts without names or personas

### Requirement: Shared Character data backup and retention
Published Cast/spec/rig/voice payloads, hashes, moderation suppression state and Course copy references MUST live in Postgres and nightly dumps. S3 rig/compiled SVG/narration assets MUST use versioning where supported and be included in nightly encrypted checksum-pinned protected copies and asset manifests; on R2 overwritten versions cannot be recovered from version history; restore MUST resolve each copy's pinned versions under `custom-characters` and `data-backup`. Payloads/assets MUST follow E15 last-reference plus 30-day retention; raw prompts and private screening data MUST never enter published backups as Course content. Deletion and takedown records MUST be reapplied before restored versions are served.

#### Scenario: Restore published Characters
- **WHEN** database and asset backups are restored after the creator's private library is deleted
- **THEN** enrolled copies resolve their original approved Character snapshots without needing that library or a paid model call

### Requirement: Shared Character model testing and reliability
Golden set `character_snapshot`, `character_screen` and `character_injection` cases MUST cover content-only export, privacy, immutable reuse, age/IP/likeness screening and persona injection. Snapshot/privacy validity MUST score 1.00; screening and injection MUST meet `custom-characters` thresholds in `evals/thresholds.toml` and pass `mise run engine:eval`. Character screening MUST use the 10 s per-call deadline and bounded retry/provider fallback within E15's publish deadline and platform-paid screening cost cap. Unavailable screening MUST fail closed; runtime invalid Characters MUST use the Default cast within existing generation/chat budgets, and base generation failure MUST release its reservation.

#### Scenario: Private field regression
- **WHEN** an export/model change includes a raw creation prompt or alters a pinned Character payload
- **THEN** deterministic snapshot checks fail and publication cannot pass the eval gate

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
Only the Course owner SHALL read a draft or its verdict; everyone else MUST get the same neutral not-found as an unknown draft. Publishing MUST be rejected for non-owners, for accounts under the configured minimum age (default 18).

#### Scenario: Foreign read
- **WHEN** another Learner requests the draft
- **THEN** the response is the neutral `draft_unavailable`

### Requirement: Design parity
The owner preview MUST match `docs/design/canvas/E15-Share.dc.html` on desktop (dialog) and mobile (sheet) with loading, empty, offline, error, long-content, keyboard focus, light/dark and reduced-motion states; tokens win over canvas values.

#### Scenario: Screening pending
- **WHEN** the draft is `pending_screening`
- **THEN** the panel says learning is unaffected and offers Retry screening

### Requirement: Enroll a pinned copy from a live link
A signed-in Learner SHALL create exactly one Course copy per source Course from a live Share link. The copy MUST be pinned to the latest passing published version, owned by the Learner, and idempotent on repeat. A private, revoked, unknown, deleted or block-hidden link MUST refuse new enrollment with the neutral `link_not_found`; existing copies MUST keep working. Per-account enrollments per day MUST be limited by config and refused with `rate_limited`.

#### Scenario: Enroll twice
- **WHEN** a Learner enrolls through the same link twice
- **THEN** one copy exists and both calls return its id, the second with `created=false`

#### Scenario: Revoked link
- **WHEN** the owner revokes the link and a new Learner tries to enroll
- **THEN** the API returns `link_not_found`, no copy is created, and existing copies still open

### Requirement: Reuse published lessons for free
Lessons already generated in the pinned version MUST be served from the copy at zero Credits (no quote, reservation or charge) and MUST NOT expose answer keys in manifests. Lesson gate order MUST still require the predecessor pass. Lessons missing from the snapshot MUST be reported as needing generation, left for E15-04.

#### Scenario: Unlock without spending
- **WHEN** a Learner passes Lesson 1 of a copy whose version contains Lesson 2
- **THEN** Lesson 2 becomes `current` and the Credit balance is unchanged

#### Scenario: Learner-specific state
- **WHEN** two Learners enroll from one link and one passes Lesson 1
- **THEN** the other still has Lesson 2 locked, and the creator sees only the enrollment count

### Requirement: Generate the next missing Lesson in a Course copy
At the end of the published content of a Course copy, the API SHALL show how many Lessons are ready out of the total and an explicit quote at the current Pricing config version before generating the next missing Lesson. Generation MUST require that the previous Lesson test is passed and that the Learner gave explicit consent to that quote. The generated Lesson MUST be stored only in the Learner's copy; the published snapshot, its content hash and every other copy MUST stay unchanged. The generating Learner pays; the owner receives nothing and no update is forked.

#### Scenario: Quote then generate
- **WHEN** a Learner passed the last published Lesson, sees the quote and consents
- **THEN** the next Lesson is generated through the gated pipeline and charged at the quoted version

#### Scenario: Previous Lesson not passed
- **WHEN** a Learner requests a quote or generation before passing the previous Lesson
- **THEN** the API refuses with `previous_lesson_not_passed` and the Engine is not called

#### Scenario: No consent or stale quote
- **WHEN** consent is missing, or the quote expired or its Pricing config version is no longer active
- **THEN** the API refuses with `consent_required` or `quote_stale`, charges nothing and the Learner can request a new quote

#### Scenario: Not enough Credits
- **WHEN** the balance is below the quoted content and audio ceilings
- **THEN** the API refuses with `insufficient_credits` before any Engine call

#### Scenario: Failure
- **WHEN** generation fails after reservation
- **THEN** undelivered components are released, earlier Lessons stay available and the Learner can retry

#### Scenario: Owner content untouched
- **WHEN** a Learner generates a Lesson beyond the snapshot
- **THEN** the snapshot, content hash, owner Course and other copies are byte-identical

### Requirement: Invite delivery and push adapter
The API SHALL deliver an invite to the recipient's in-app inbox whenever the invite is allowed, and SHALL send push only through an adapter that is disabled unless configured. Delivery MUST be deduplicated per invite and channel, MUST re-check blocks and the recipient's settings at send time, and MUST carry only identifiers and an enum, never Course material or a Username. A denied, disabled or failing push MUST NOT remove the in-app invite. Push credentials MUST come only from secret-store environment variables.

#### Scenario: Push denied
- **WHEN** the recipient turned course invite push off or the adapter is disabled
- **THEN** the invite is still in the recipient's inbox and no push is sent

#### Scenario: Neutral refusal
- **WHEN** the recipient's `course_invites_from` is `nobody` or the pair is blocked
- **THEN** the sender sees the invite as sent and the recipient's inbox and push stay empty

#### Scenario: Accept once
- **WHEN** the recipient accepts twice, or already owns a copy
- **THEN** exactly one pinned copy exists and the second accept returns the same copy

### Requirement: Stable lesson keys and explicit updates
Every published Lesson SHALL carry a stable key that survives retitles and reordering. A Learner's Course copy SHALL move to a newer version only when the Learner updates it with the changelog shown; the update MUST map Lessons by key, keep completed Lessons, tests, progress, notes and Learner-generated Lessons, add new Lessons, and keep Lessons removed upstream in the Learner's history as archived. A copy forked by Course steering MUST NOT be offered or receive updates. A pin or update with a stale lock version MUST be rejected without detail. Objects of a published version MUST be retained while any copy pins it or references its Lessons.

#### Scenario: Update with retitled and removed lessons
- **WHEN** the owner retitles Lesson 2, removes Lesson 3 and republishes, and a Learner who completed Lessons 1 and 2 updates
- **THEN** Lessons 1 and 2 stay completed with the new title, Lesson 3 appears as archived history and the pin is the new version

#### Scenario: Stale update
- **WHEN** two updates race with the same lock version
- **THEN** one succeeds and the other is rejected neutrally

### Requirement: Republish cap and screening
Republishing SHALL be limited to three versions per Course per rolling day (configuration) and every version SHALL be screened anew; only a passing version becomes live.

#### Scenario: Fourth republish
- **WHEN** an owner republishes a fourth time within a day
- **THEN** the request is refused with the limit error and no version is created

### Requirement: Authored example courses
The system SHALL offer two authored, English, beginner example Courses (Product Management Foundations and JavaScript Essentials), each of 2 units and 6 Lessons with Motion HTML Slides, a Lesson test and a pre-generated retry pool, stored as Git-versioned fixtures. Every authored Lesson SHALL pass the same validators as a generated Lesson, and every JavaScript code example and printed-output answer SHALL be verified by running it. Serving content SHALL NOT call a model, the engine or any provider.

#### Scenario: Authored content is valid
- **WHEN** the authored fixtures are tested
- **THEN** every Lesson of every version passes the production Lesson, markup, manifest and digest validators
- **AND** every JavaScript snippet prints the output the Lesson shows

### Requirement: Example catalogue
`GET /v1/courses/examples` SHALL require a session and return `{"examples":[{"slug","title","description","level","units","lessons","estimated_minutes"}]}`, identical for every Learner, with `Cache-Control: private, no-cache`, `Vary: Authorization` and a strong ETag answered with 304 when unchanged.

#### Scenario: List the examples
- **WHEN** a signed-in Learner requests the catalogue
- **THEN** the two examples are returned with 6 Lessons each and a minutes estimate
- **AND** a repeat request with the ETag returns 304

### Requirement: Free idempotent example enrollment
`POST /v1/courses/examples/{slug}/enroll` SHALL give the Learner exactly one own copy of the example pinned to the authored version current at enrollment, return `{"course_id"}`, and make that Course active. A repeat call SHALL return the same Course and keep its pinned version; an unknown slug SHALL return 404. Enrollment, reading the Lessons and passing their tests SHALL reserve, spend and grant no Credits, SHALL NOT call the engine, and SHALL work at a zero balance. The copy SHALL be read by the lesson player through the same manifests as any Course.

#### Scenario: New learner at zero Credits
- **WHEN** a Learner with no Credits enrolls, reads the manifest and slides of all six Lessons in order and passes each test
- **THEN** no Credit ledger entry exists and the engine received no request

#### Scenario: Enroll twice
- **WHEN** the same Learner enrolls in the same example twice, even concurrently
- **THEN** both calls return the same Course id and exactly one copy exists

#### Scenario: A new authored version ships
- **WHEN** a newer version of an example is published
- **THEN** learners already enrolled keep reading the version they are pinned to
- **AND** new enrollments receive the newest version

### Requirement: Example labelling and completion
An example Course SHALL be labelled as an example wherever it is listed or opened, and course summaries and details SHALL carry `example_slug` and no creator attribution. After the last Lesson the Learner SHALL see a completion state with a "Create your own course" next step; no paid generation SHALL start by itself. Deleting an enrolled example follows normal Course deletion and the Learner may enroll again.

#### Scenario: Finish an example
- **WHEN** a Learner passes the sixth Lesson test
- **THEN** the path shows the completion state and a link to Create a course, and no Credits are spent

