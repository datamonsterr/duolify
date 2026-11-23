## ADDED Requirements

### Requirement: Original default mascot
Duolify SHALL ship one default guide mascot, working name Tinko: an original friendly green robot built from Duolify design tokens. It MUST NOT be based on, derived from or resemble any existing brand character (no owl, no Duo name, silhouette, face, pose or trade dress). Names considered: Tinko, Nodo, Glimmo; the final name needs an owner trademark check. The art MUST be hand-written SVG made of parametric parts (body, head, visor, eyes, mouth, antenna, ear pods, arms, accent) coloured only by CSS variables bound to tokens, use two to three greens and one warm accent, and stay recognisable at 32 px and 160 px. Reference drawings are `docs/design/mascot/mascot-sheet.svg` and `mascot-idle.svg`.

#### Scenario: Small size
- **WHEN** the mascot renders at 32 px on light and dark surfaces
- **THEN** the head, visor, eyes and antenna remain distinguishable

#### Scenario: Token binding
- **WHEN** appearance changes between light and dark
- **THEN** the mascot recolours through variables with no literal colours outside the allowlist

### Requirement: Mascot states and triggers
The mascot SHALL have eight states: `idle` (default), `listening` (composer or widget focused, Learner typing), `thinking` (waiting for the server or an AI answer), `talking` (guide message appearing or streaming), `happy` (answer accepted, step done), `celebrate` (streak milestone, Lesson passed, Course created, setup completed), `confused` (validation error, unknown input, recoverable error) and `sleepy` (offline, or idle over 60 s; offline adds a slashed-signal glyph). State MUST be derived from UI events, never from model output. State changes MUST NOT block input or delay a message.

#### Scenario: Lesson passed
- **WHEN** a Learner passes a Lesson test
- **THEN** the mascot shows `celebrate` and returns to `idle` after the animation

#### Scenario: Offline
- **WHEN** the device goes offline
- **THEN** the mascot shows `sleepy` with the offline glyph and the offline text bubble

### Requirement: Lightweight animation
Animation SHALL use inline SVG with CSS keyframes (Web Animations API for sequencing) and no animation runtime library. The mascot code, styles and art MUST total at most 30 KB minified. Animations MUST target 60 fps using only transform and opacity, MUST pause when off-screen (IntersectionObserver) or the page is hidden, and MUST never block interaction. With `prefers-reduced-motion` or the in-app motion setting, every state MUST render as a static pose with the same meaning (glyphs, mouth and eyes keep the state readable). Animations MUST NOT flash more than three times per second.

#### Scenario: Reduced motion
- **WHEN** reduced motion is on
- **THEN** states change as static poses and nothing moves

#### Scenario: Off-screen
- **WHEN** the mascot scrolls out of view
- **THEN** its animations pause and resume when it returns

### Requirement: Mascot placement
The mascot SHALL appear as the avatar and guide in every guided chat and Tutor chat (when the Cast main guide is the default), in onboarding, in empty states, in celebrations (streak, Lesson passed) and in error states. In the framed lesson player it SHALL be a 48 px corner companion in the player chrome outside the slide viewport; in the immersive player it MUST be hidden unless it speaks a hint or celebration. It MUST NOT cover slide content, controls or the answer panel. The Learner MUST be able to hide the companion in Settings. It MUST have an accessible name only where it conveys information; decorative placements MUST be `aria-hidden`.

#### Scenario: Immersive player
- **WHEN** a Learner is in immersive mode and no hint is spoken
- **THEN** no mascot is shown

#### Scenario: Hidden by choice
- **WHEN** the Learner hides the companion
- **THEN** it is not shown in the player but still appears in chat as the guide avatar

### Requirement: Mascot never inside generated slides by default
The mascot is a client interface guide and MUST NOT be drawn into generated Motion HTML slides, narration speakers or lesson tests unless the Learner selects it as a Character in a Cast under `custom-characters`. Rendering it in the player chrome MUST NOT affect slide content, manifests, grading or generation prompts.

#### Scenario: Default cast
- **WHEN** a Lesson is generated with the Default cast
- **THEN** the slides contain Default cast Characters only and not the mascot

### Requirement: Mascot customisation
The Learner SHALL be able to customise the default mascot from a "Your guide" picker: recolour from a contrast-validated palette allowlist, add up to two accessories from an enum, or replace it with one of their own approved Characters. Customisation MUST be free, MUST NOT call a model, and MUST fall back to default Tinko if a chosen Character is deleted, unavailable or fails validation. The server MUST validate the preference against the shared part schema.

#### Scenario: Recolour
- **WHEN** the Learner picks a different body colour
- **THEN** the mascot updates everywhere and the choice persists across devices

#### Scenario: Character removed
- **WHEN** the chosen Character is deleted
- **THEN** the guide reverts to default Tinko without error

### Requirement: Shared part schema with custom characters
Mascot appearance and `custom-characters` rig output SHALL share `RigPartSpec v1`: `rig` (`robot-v1` or a Character rig id), `palette` slots `body`, `bodyEdge`, `trim`, `visor`, `eye`, `accent` (hex values from the allowlist that pass 3:1 against both canvases), `accessories` (enum array, at most 2) and the eight state names. The Character expressions `idle`, `happy`, `sad`, `thinking`, `celebrate` MUST map to `idle`, `happy`, `confused`, `thinking`, `celebrate`; states a rig does not define MUST fall back to `idle`. The `custom-characters` rewrite MUST adopt or extend this version rather than fork it.

#### Scenario: Rig without listening
- **WHEN** a Character rig has no `listening` pose
- **THEN** the guide uses its `idle` pose in that state

### Requirement: Mascot preference storage, export and backup
The preference SHALL be stored as the `mascot` object in Learner settings in Postgres (mode `default` or `character`, palette overrides, accessories, optional `character_id`, companion visible flag). It MUST be returned by the settings API, included in the account export, deleted with the account, and covered by encrypted nightly dumps (14 days) and the restore drill. Partial updates MUST preserve omitted fields.

#### Scenario: Export
- **WHEN** a Learner exports their data
- **THEN** the mascot preference is included

### Requirement: Mascot analytics
The system SHALL emit content-free PostHog events `mascot_customized` (`mode`, `fields_changed`), `mascot_companion_toggled` (`visible`) and `mascot_state_fallback` (`reason`, when art or Character fails). Per-state display MUST NOT be tracked per frame. Opt-out and common properties apply.

#### Scenario: Customised
- **WHEN** a Learner saves a new accessory
- **THEN** `mascot_customized` carries `fields_changed=accessories` and no free text

### Requirement: Mascot accessibility and tone
Mascot art MUST not carry essential information alone: every state change that matters is also in text. Speech bubbles MUST follow brand voice (brief, friendly, no guilt, no pushy purchase prompts, no pet names). The mascot MUST mirror placement to the start edge in RTL without flipping its art. Contrast for glyphs and bubbles MUST use token pairs.

#### Scenario: Error state
- **WHEN** an error occurs and the mascot shows `confused`
- **THEN** the error message and retry action are present as text and controls
