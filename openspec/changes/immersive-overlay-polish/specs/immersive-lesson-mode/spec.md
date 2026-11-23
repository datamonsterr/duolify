## MODIFIED Requirements

### Requirement: Answer panel over the Slide
In landscape the Tutor chat reply SHALL stream into a panel docked to the right edge of the screen, laid over the Slide stage, with a translucent background (about 70 to 80 percent alpha from design tokens, with a blur where supported, a solid-enough fallback where backdrop blur is unsupported, and an opaque background when the Learner prefers reduced transparency). The side rail laid over the stage MUST use the same surface. Text and controls MUST keep a 4.5:1 contrast ratio over both a dark and a white Slide. The selected region MUST stay visible and outlined; when the panel would cover it, the stage MUST shift or scale so the whole region remains visible. The panel MUST be icon-only apart from the Learner's question, the tutor reply, a short follow-up placeholder and the recording timer: it MUST NOT show a title, an auto-send checkbox or explanatory paragraphs. It MUST offer stop while streaming, a follow-up by voice or text, save as Quick note, collapse, and close as icon buttons with accessible names. Explanations, confirmations and errors MUST appear as toasts near the stage edge, not inside the panel. Closing MUST return to the same Slide and time and resume playback only if it was playing before the question. The panel MUST show the same thread as the lesson tutor sheet. In portrait and on screens too narrow for the panel, the lesson tutor sheet MUST be used, with a solid background, with the region shown on a miniature stage. Streaming MUST announce only the start and the finish of a reply to assistive technology. The mascot companion SHOULD show talking while a reply streams.

#### Scenario: Reply shown on the right
- **WHEN** a reply starts streaming in landscape
- **THEN** it appears in a translucent panel on the right and the Slide stays visible through it, while the outlined region remains visible on the left

#### Scenario: Readable over any Slide
- **WHEN** the panel is open over a white Slide and over a dark Slide
- **THEN** its text keeps at least a 4.5:1 contrast ratio on both

#### Scenario: Reduced transparency
- **WHEN** the Learner prefers reduced transparency
- **THEN** the panel and the side rail are opaque

#### Scenario: Region under the panel
- **WHEN** the selected region lies in the right third of the Slide
- **THEN** the stage shifts so the whole region is visible beside the panel

#### Scenario: Saved as a Quick note
- **WHEN** the Learner taps the save icon on a reply
- **THEN** a toast confirms the Quick note with an Undo action and the panel shows no text confirmation

#### Scenario: Close resumes
- **WHEN** the Learner closes the panel after asking while the Slide was playing
- **THEN** the Lesson returns to the same Slide and time and playback resumes
