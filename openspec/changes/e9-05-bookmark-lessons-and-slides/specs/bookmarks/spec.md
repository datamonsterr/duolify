## MODIFIED Requirements

### Requirement: Create and remove bookmarks
A Learner SHALL bookmark a Slide or a Lesson with one tap and remove it with one tap. A Bookmark MUST be unique per Learner and target.

#### Scenario: Bookmark a Slide
- **WHEN** a Learner bookmarks a Slide
- **THEN** a Bookmark with `lesson_id` and `slide_id` is stored

#### Scenario: Duplicate bookmark
- **WHEN** a Learner bookmarks the same target twice
- **THEN** only one Bookmark exists

### Requirement: Open a bookmark
Opening a Bookmark SHALL open the Lesson at the saved Slide. When the Lesson content is not cached the client MUST fetch it or offer online recovery.

#### Scenario: Open offline
- **WHEN** the Lesson is cached and the device is offline
- **THEN** the Bookmark opens at the Slide

#### Scenario: Open uncached offline
- **WHEN** the Lesson is not cached and the device is offline
- **THEN** an offline recovery message indicates the slide is not downloaded and offers to open when online

### Requirement: List and filter
The client SHALL list Bookmarks by Course with a Lesson title and Slide index. Bookmarks to a removed Lesson MUST be shown as unavailable and removable, and MUST NOT be silently redirected.

#### Scenario: Lesson removed
- **WHEN** a bookmarked Lesson is deleted
- **THEN** the Bookmark shows as unavailable and retains a Remove action
