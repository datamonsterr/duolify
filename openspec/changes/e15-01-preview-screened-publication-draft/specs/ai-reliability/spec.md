## ADDED Requirements

### Requirement: Course screening reliability
`screen_course` SHALL have schema-checked output (`verdict`, closed `reason_codes`), a 120 second deadline, one evaluated fallback within the USD cap, and golden-set rows for safe, borderline, multilingual, deep-hidden-payload and injection cases at a pinned `datamonsterr/duolify-golden` revision with recorded mock fixtures. `mise run engine:eval` MUST pass. Live promotion stays gated by E10-06 (ADR 0011).

#### Scenario: Malformed verdict
- **WHEN** the provider returns an invalid verdict twice
- **THEN** the engine reports failure and the API stores `pending_screening`
