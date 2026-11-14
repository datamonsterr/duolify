## ADDED Requirements

### Requirement: Moderator role and queue
Only accounts with a moderator grant SHALL see the moderation queue or act on it; anyone else MUST receive a neutral 404. The queue MUST list open items ordered by severity then age with a configurable first-response target (default 48 hours) and MUST flag items past it as overdue. Publications held as `needs_review` or `pending_screening` MUST be queue items and MUST NOT become visible until a moderator resolves them.

#### Scenario: Non-moderator
- **WHEN** a learner without a grant requests the queue
- **THEN** the API answers 404 and reveals nothing

#### Scenario: Overdue
- **WHEN** an open item is older than 48 hours
- **THEN** the queue marks it overdue and counts it in the overdue total

#### Scenario: Borderline publication
- **WHEN** a moderator dismisses a needs_review publication
- **THEN** it passes and may be published; rejecting it keeps it unpublishable

### Requirement: Audited enforcement
Actions `dismiss`, `warn`, `delist`, `takedown` and `ban_publishing` SHALL each require a closed reason code and a reason text and SHALL write one append-only audit row with the moderator, target, action and time. Review, report and user targets MUST use the same contract and every action that changes visibility MUST invalidate the Marketplace cache.

#### Scenario: Missing reason
- **WHEN** an action has no reason text
- **THEN** it is refused and nothing changes

#### Scenario: Append only
- **WHEN** anything tries to update or delete an audit row
- **THEN** the database refuses

### Requirement: Delist versus severe takedown
A takedown MUST delist the course, revoke its share link and block new enrollment. With a severe reason code (`unsafe` or `legal`) it MUST also block existing copies, which then show a notice and refuse lessons. Any other action MUST leave existing copies fully usable at their pinned version.

#### Scenario: Normal delist
- **WHEN** a moderator delists a spam course
- **THEN** enrolled learners keep access

#### Scenario: Severe takedown
- **WHEN** a moderator takes down a course as unsafe
- **THEN** existing copies are blocked with a notice

### Requirement: One appeal by another moderator
The author SHALL see the reason of every action against their content and MAY appeal each action once. A different moderator than the acting one MUST decide; the actor MUST be refused, and a second appeal MUST be refused. Overturning restores the action's effects: for a takedown this MUST include re-activating the share link(s) that the takedown revoked, using the same token hash so that previously shared URLs work again. Links that were already revoked for other reasons (owner revoked, link expired) before or after the takedown MUST NOT be re-activated. The appeal decision, link state change and moderation audit row MUST all commit in one transaction; a rollback must leave nothing half-done.

#### Scenario: Same moderator
- **WHEN** the acting moderator tries to decide the appeal
- **THEN** the API refuses and the appeal stays open

#### Scenario: Overturned takedown restores share link
- **GIVEN** an author published a course with a share link, a moderator took it down (revoking the link), and the author appealed
- **WHEN** a different moderator overturns the appeal
- **THEN** the original share URL (same token hash) works again, the course visibility is restored to `link`, the moderation state is cleared, and a `restore_link` audit row is written in the same transaction

#### Scenario: Owner-revoked link is not restored
- **GIVEN** an author explicitly revoked their share link before the takedown, and the takedown was later overturned
- **WHEN** the overturn completes
- **THEN** the owner-revoked link remains revoked and the course visibility stays `private` (or `public` if the listing was active)

### Requirement: Moderation analytics, backup and erasure
The API SHALL emit `moderation_action` (action, reason_code, target_type), `moderation_appeal` (outcome `filed`, `upheld` or `overturned`) and `course_visibility_changed`, with UUIDs and closed enums only and never reason or appeal text. Actions, appeals and queue items SHALL be in nightly dumps and kept three years; deleting an account MUST remove its personal references while keeping the audit rows.

#### Scenario: Author erased
- **WHEN** an author deletes their account
- **THEN** their actions and appeals remain without author reference or appeal text

### Requirement: Moderation surfaces
The client SHALL provide a moderator queue with filters and an action dialog, and an author notice with a one-time appeal, on desktop and mobile with loading, empty, error, long content, keyboard, light and dark and reduced-motion states.

#### Scenario: Mobile
- **WHEN** a moderator opens an item on a narrow screen
- **THEN** the detail and action dialog appear as a second screen and a sheet
