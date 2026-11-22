## ADDED Requirements

### Requirement: Push-to-talk capture
The client SHALL let the Learner dictate a Tutor chat question or a Quick note by holding a microphone control and releasing it to finish. Recording MUST happen only while the control is held, or between two taps when the Learner uses tap-to-toggle. A visible recording indicator, level meter and timer MUST be shown for the whole recording. Sliding off the control or pressing Escape MUST cancel and discard the audio. A recording shorter than 0.5 seconds MUST be discarded; recording MUST stop at 30 seconds. Typed input MUST always be available beside the microphone.

#### Scenario: Hold, speak, release
- **WHEN** the Learner holds the microphone control, speaks for 4 seconds and releases
- **THEN** recording stops, the audio is sent for transcription and the transcript appears in the composer

#### Scenario: Cancel
- **WHEN** the Learner slides off the control while recording
- **THEN** the recording is discarded and nothing is uploaded

#### Scenario: Limit reached
- **WHEN** the Learner keeps holding for more than 30 seconds
- **THEN** recording stops at 30 seconds and the captured audio is transcribed

### Requirement: Transcript review before use
A transcript for a question SHALL be shown in the composer and sent automatically only after a 2-second window in which the Learner can edit or cancel; a Learner setting MUST allow requiring an explicit send. A transcript for a Quick note SHALL be saved immediately as a raw Quick note with an undo action. An empty or failed transcription MUST NOT create a message or a note and MUST offer retry or typing.

#### Scenario: Edit before sending
- **WHEN** the transcript appears and the Learner changes a word within 2 seconds
- **THEN** automatic sending stops and the edited text is sent only when the Learner sends it

#### Scenario: Nothing understood
- **WHEN** transcription returns no text
- **THEN** no message or note is created and the Learner is offered retry or typing

### Requirement: Transcription service
The API SHALL expose an authenticated transcription endpoint that accepts one audio recording, a purpose of `tutor` or `note`, and a Lesson id, and returns the text, detected language and duration. The API MUST reject audio longer than 30 seconds or larger than 1 MB, unsupported containers, a Lesson the Learner is not entitled to, and requests over the per-Learner rate limit or daily cap, before any Engine call. The Engine MUST transcribe through the configured primary model and fallback on the single AI provider route with provider data collection denied. The endpoint MUST accept the containers produced by supported clients (WebM/Opus and MP4/AAC).

#### Scenario: Transcribe a question
- **WHEN** an entitled Learner uploads a 6-second recording with purpose `tutor`
- **THEN** the response contains the transcript, language and duration

#### Scenario: Too long
- **WHEN** a recording of 45 seconds is uploaded
- **THEN** the API answers with a client error and makes no Engine call

#### Scenario: Daily cap
- **WHEN** a Learner exceeds the daily transcription cap
- **THEN** the API answers with a typed limit error naming when it resets, and typing remains available

### Requirement: Audio is processed and discarded
Recorded audio MUST be held in memory only for the duration of the request. The client, API and Engine MUST NOT write audio to disk, object storage, logs, analytics or backups, and MUST NOT retain it after the response. Only the transcript MAY persist, and only as the Tutor chat message or Quick note it becomes.

#### Scenario: Nothing retained
- **WHEN** a transcription request completes or fails
- **THEN** no audio bytes exist in storage, logs or queues, and no record refers to an audio object

### Requirement: Microphone permission and unavailable states
The client SHALL request microphone permission only when the Learner first uses a microphone control, with an explanation of why. With permission denied, offline, voice not enabled for the environment, or the daily cap reached, the microphone control MUST be disabled or explain the state, and typing MUST remain available. Voice MUST NOT be the only way to ask or to take a note.

#### Scenario: Permission denied
- **WHEN** the Learner denies microphone permission
- **THEN** the control explains how to enable it and the text composer stays usable

#### Scenario: Offline
- **WHEN** the device is offline
- **THEN** the microphone control explains that voice needs a connection and typed Quick notes still queue offline

### Requirement: Voice Credits and cost limits
A voice question SHALL cost the same Credits as the typed Tutor chat action and a voice Quick note the same as a typed Quick note; transcription MUST NOT have a separate Learner price in this change. Transcription provider usage MUST be settled against a rate in the active pricing version's quote policy; while the active version has no transcription rate, voice input MUST be unavailable and MUST explain that typing still works. A transcription that is not followed by a sent question or a saved note MUST NOT spend Credits.

#### Scenario: Same price as typing
- **WHEN** a Learner asks the same question once by typing and once by voice
- **THEN** both turns reserve and settle the same Credits

#### Scenario: No transcription rate
- **WHEN** the active pricing version has no rate for the transcription model
- **THEN** the microphone control is unavailable with an explanation and no Engine call is made

### Requirement: Voice analytics events
The client and API SHALL emit content-free events from the analytics catalogue for voice input: recording started, cancelled and finished, transcription succeeded and failed, and transcript used or discarded. Events MUST carry purpose, duration bucket, language, error class and identifiers as closed enums or ids, MUST NOT carry audio, transcript text or question text, and MUST respect analytics consent.

#### Scenario: Successful dictation
- **WHEN** a Learner dictates a question that is sent
- **THEN** `voice_recording_finished`, `voice_transcription_succeeded` and `voice_transcript_used` are emitted with purpose `tutor` and no text

### Requirement: Voice data backup and retention
Voice input SHALL create no stored data beyond the transcript text inside the Tutor chat message or Quick note it becomes, which MUST follow that record's backup, export, retention and erasure. Per-Learner transcription counters used for limits MUST hold no content, MUST expire within the limit window, and MUST be removed on account erasure.

#### Scenario: Erase a Learner
- **WHEN** a Learner's account is erased
- **THEN** their transcripts are removed with the messages and notes that contain them and no transcription counter remains

### Requirement: Transcription model testing and reliability
The Golden set SHALL contain transcription cases: short recordings with reference text covering the supported languages, accents, background noise, silence and technical vocabulary. Promotion MUST require the word error rate to be at or below the configured threshold, silence to yield empty text, latency within the configured budget, and cost per request within the configured limit. The Engine MUST apply a timeout, try the fallback model once, and answer with a typed retryable error when both fail. The transcription task MUST stay closed on a live Engine without promotion evidence.

#### Scenario: Primary fails
- **WHEN** the primary transcription model times out
- **THEN** the fallback model is tried once and, if it also fails, the Learner sees a retryable error and no message or note is created

#### Scenario: Silence
- **WHEN** a recording contains only background noise
- **THEN** the transcript is empty and the Learner is offered retry or typing
