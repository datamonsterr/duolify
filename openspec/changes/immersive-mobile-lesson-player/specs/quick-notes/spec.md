## ADDED Requirements

### Requirement: Quick note anchored to a Lesson moment
A Quick note created from the Lesson player SHALL be able to carry an anchor: the Lesson, the Slide index, the playback time and, if one is selected, the region rectangle. The note MUST record whether it was typed or dictated. Opening an anchored note MUST offer to open the Lesson on that Slide at that time with the region outlined. The anchor MUST survive offline capture and later sync, and a note whose Lesson was removed MUST remain readable as a source-unavailable note.

#### Scenario: Dictated note on a region
- **WHEN** the Learner selects a region, holds the note control, speaks and releases
- **THEN** a raw Quick note with the transcript, the Slide, the time, the rectangle and source `voice` is saved, with undo

#### Scenario: Open from the note
- **WHEN** the Learner opens that note and chooses "show in Lesson"
- **THEN** the Lesson opens on that Slide at that time with the region outlined

#### Scenario: Typed note offline
- **WHEN** the Learner types a note on a region while offline
- **THEN** it is queued with its anchor and synced once when the device is online
