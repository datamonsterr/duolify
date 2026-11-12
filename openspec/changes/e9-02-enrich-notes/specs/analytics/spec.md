## ADDED Requirements

### Requirement: Quick note enrichment events
The analytics catalogue SHALL include `quick_note_enriched` and `quick_note_enrichment_failed` events emitted upon note enrichment completion or terminal failure. Both events SHALL include standard learner distinct ID and common properties (`platform`, `app_version`, `source`), and respect analytics opt-out. Note text, summary text, and tags MUST NOT enter PostHog analytics under any circumstance.

#### Scenario: Note enrichment completed
- **WHEN** a note is successfully enriched with summary and tags
- **THEN** `quick_note_enriched` is emitted without note or summary text

#### Scenario: Note enrichment failed
- **WHEN** a note enrichment job encounters a terminal failure
- **THEN** `quick_note_enrichment_failed` is emitted without note or summary text
