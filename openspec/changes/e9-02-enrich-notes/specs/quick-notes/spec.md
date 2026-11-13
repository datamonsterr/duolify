## MODIFIED Requirements

### Requirement: Auto-enrichment
After saving, the API SHALL enqueue an asynchronous River enrichment job that reserves daily-first eligible Credits (`quick_note` action) and calls the Engine `EnrichNote` RPC with the list of allowed course lesson references. The resulting summary, tags, and grounded lesson links MUST be stored in `quick_note_enrichments` with version and model/provider metadata, and MUST NOT overwrite learner raw text. Any lesson link not present in the course's allowed lesson references MUST be dropped. On terminal failure, reserved Credits MUST be released and status set to `enrichment_failed` while retaining the raw note.

#### Scenario: Grounded enrichment succeeds
- **WHEN** the Engine returns summary, tags, and valid course lesson links
- **THEN** the note displays the summary, tags, and links separately from original text
- **AND** `quick_note_enriched` and `$ai_generation` are emitted
- **AND** reserved Credits are settled

#### Scenario: Hallucinated or foreign link rejected
- **WHEN** the Engine returns a lesson link not present in the allowed course lessons
- **THEN** the foreign link is dropped before persisting enrichment
- **AND** only valid course lesson links are displayed

#### Scenario: Terminal failure preserves raw text
- **WHEN** enrichment fails or times out
- **THEN** the reservation is released with no credits charged
- **AND** the note is preserved as raw text with status `enrichment_failed`
- **AND** `quick_note_enrichment_failed` is emitted with retry available
