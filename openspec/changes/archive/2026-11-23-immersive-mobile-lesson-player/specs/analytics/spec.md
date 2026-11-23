## ADDED Requirements

### Requirement: Immersive lesson and voice analytics catalogue
The analytics catalogue SHALL contain these events with only the listed properties, all identifiers or closed enums:
- `immersive_mode_entered` and `immersive_mode_exited`: `course_id`, `lesson_id`, `slide_index`, `trigger` (control, rotation, back, escape), `platform`, and on exit `seconds_in_mode` bucket.
- `immersive_control_used`: `control` (play, pause, previous, next, replay_slide, autoplay_on, autoplay_off, speed, captions, narration, slide_list, bookmark, lock, unlock), `via` (button, double_tap, swipe, keyboard).
- `lesson_region_selected`: `course_id`, `lesson_id`, `slide_index`, `method` (two_finger, select_area, keyboard), `area_bucket`, `regions_matched` bucket.
- `lesson_region_question_sent`: `course_id`, `lesson_id`, `slide_index`, `input` (voice, text), `has_region`, `grounding` (regions, whole_slide).
- `lesson_answer_panel_closed`: `course_id`, `lesson_id`, `outcome` (completed, stopped, failed), `saved_note`, `follow_ups` bucket.
- `lesson_anchored_note_created`: `course_id`, `lesson_id`, `slide_index`, `source` (typed, voice), `has_region`.
- `voice_recording_finished` and `voice_recording_cancelled`: `purpose` (tutor, note), `duration_bucket`.
- `voice_transcription_succeeded` and `voice_transcription_failed`: `purpose`, `duration_bucket`, `language`, `served_model`, `fallback_used`, and on failure `error_class`.
- `voice_transcript_used` and `voice_transcript_discarded`: `purpose`, `edited`.
Events MUST NOT contain audio, transcript text, question text, note text, Slide text or region descriptions, and MUST be dropped for a Learner who has opted out.

#### Scenario: Opted-out Learner
- **WHEN** a Learner who opted out of analytics asks a region question by voice
- **THEN** none of these events is sent

#### Scenario: No free text
- **WHEN** any event in this catalogue is captured
- **THEN** its properties contain only the listed identifiers and enum values
