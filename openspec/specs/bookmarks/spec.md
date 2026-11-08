# bookmarks Specification

## Purpose
Let a Learner save a pointer to a Slide or Lesson and return to it quickly.

## Requirements

### Requirement: Create and remove bookmarks
A Learner SHALL bookmark a Slide or a Lesson with one tap and remove it with one tap. A Bookmark MUST be unique per Learner and target.

#### Scenario: Bookmark a Slide
- **WHEN** a Learner bookmarks a Slide
- **THEN** a Bookmark with `lesson_id` and `slide_id` is stored

#### Scenario: Duplicate bookmark
- **WHEN** a Learner bookmarks the same target twice
- **THEN** only one Bookmark exists

### Requirement: Open a bookmark
Opening a Bookmark SHALL open the Lesson at the saved Slide. When the Lesson content is not cached the client MUST fetch it.

#### Scenario: Open offline
- **WHEN** the Lesson is cached and the device is offline
- **THEN** the Bookmark opens at the Slide

### Requirement: List and filter
The client SHALL list Bookmarks by Course with a Lesson title and Slide index. Bookmarks to a removed Lesson MUST be shown as unavailable and removable.

#### Scenario: Lesson removed
- **WHEN** a bookmarked Lesson is deleted
- **THEN** the Bookmark shows as unavailable

### Requirement: Offline use
Bookmarks MUST be creatable offline and MUST sync once per action when online.

#### Scenario: Bookmark offline
- **WHEN** a Learner bookmarks while offline
- **THEN** it is queued and synced later without duplicates

### Requirement: Bookmark analytics events
The system SHALL emit `bookmark_added`, `bookmark_removed` and `bookmark_opened` with `target_type`, `course_id`, `lesson_id` and `slide_id`.

#### Scenario: Bookmark added
- **WHEN** a Bookmark is created
- **THEN** `bookmark_added` is emitted with `target_type` (`slide` or `lesson`)

### Requirement: Bookmark data backup and retention
Bookmarks MUST be stored in Postgres, covered by the nightly dump, and deleted with the account within 30 days. Bookmarks do not use AI, so no model reliability requirement applies.

#### Scenario: Restore
- **WHEN** a dump is restored
- **THEN** every Bookmark target resolves to an existing Lesson or is flagged unavailable
