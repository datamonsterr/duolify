## ADDED Requirements

### Requirement: Accessible guided feature tour
Learn SHALL provide five to seven contextually available coach marks using native tokens, a dimmed backdrop, spotlight cutout, target-pointing arrow, dots and a sentence step indicator, Back/Next/Skip and final Done. Missing, hidden or disabled targets SHALL be omitted. Settings SHALL expose Show me around. The implementation SHALL load lazily without a new tour dependency.

#### Scenario: Keyboard and reduced motion
- **WHEN** a learner uses keyboard controls or reduced motion
- **THEN** focus is trapped in the modal, Esc skips, arrows navigate, Enter advances from the heading, controls remain reachable, and spotlight movement is not animated

#### Scenario: Small viewport or resize
- **WHEN** the viewport is 390px wide, resized, scrolled or reduced by a keyboard
- **THEN** tooltip placement flips/clamps, arrow tracks the visible target and Skip stays reachable
