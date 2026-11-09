## ADDED Requirements

### Requirement: Localized appearance controls
The client SHALL provide stable message identifiers, English fallback, Arabic RTL, an expanded-copy fixture and native Intl plural, number, date and currency formatting. Route identity and editing state SHALL survive preference changes. Names SHALL be bidi-isolated. Layout SHALL reflow at 320px and 200% text.

#### Scenario: Mid-form preferences
- **WHEN** a learner changes locale, appearance or motion while editing chat, summary or outline
- **THEN** drafts, quick reply selections, focus, proposal and undo state remain intact and translated controls wrap

### Requirement: Restrictive motion resolution
System or learner reduced motion SHALL suppress spatial movement, automatic scrolling and decorative animation. Instructional content SHALL retain a static equivalent explanation.

#### Scenario: System reduction wins
- **WHEN** the system requests reduced motion and the user preference does not
- **THEN** the effective preference remains reduced, including after a system setting change

### Requirement: Private preference analytics and lifecycle
The client SHALL emit ui_appearance_changed with mode in system, light or dark and settings ui_control_activated with control_id in locale, motion or reset only. Events SHALL include common UUID properties, respect identity and transport opt-outs and exclude learner content. Preferences SHALL use a host adapter where available and otherwise remain disposable memory; reset SHALL clear them.

#### Scenario: Reset and restore
- **WHEN** the preference host is reset or recreated without a durable adapter
- **THEN** defaults return without restoring drafts or sending private data
