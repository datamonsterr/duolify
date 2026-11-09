## ADDED Requirements

### Requirement: Client green theme foundation
The client SHALL import the canonical semantic CSS mapping and bundle licensed Nunito WOFF2 weights 600, 800 and 900 without runtime font-service requests. The course list, cards, buttons and path SHALL use the revised light/dark tokens and 72px node size. Lime SHALL be used only for text-free progress and decorative geometry. Cards SHALL expose one linked title and semantic, labelled progress. Controls SHALL retain 44px targets, visible focus, pending duplicate protection and reduced-motion parity.

#### Scenario: Course recovery and reflow
- **WHEN** a learner opens Courses at desktop or mobile width
- **THEN** the same search, loading, empty, offline, unavailable and error/retry states are readable in both themes, with long content and 200% text

### Requirement: Course controls analytics boundary
Course controls SHALL emit the catalogued `ui_control_activated` event with stable control IDs through the shared analytics boundary only when an enabled authenticated UUID context is present. Events SHALL include common properties and SHALL omit learner text. Consent revocation and analytics failure SHALL never prevent navigation or recovery.

#### Scenario: Course search privacy
- **WHEN** an opted-in learner clears a course search
- **THEN** the event includes only surface `courses`, control_id `clear_search` and common properties, never the query

#### Scenario: Missing analytics context
- **WHEN** E1-06 has not supplied an enabled learner context or the learner opts out
- **THEN** course controls work without emitting an analytics event
