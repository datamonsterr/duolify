# Design: Install approved identity and optional feedback

## Context
ADR 0013 confirmed that the original placeholder mark (two offset rounded tiles forming an opening), green theme, and design tokens constitute the approved v1 Duolify identity. No UI audio assets are shipped for v1, making all UI sound triggers silent no-ops with zero network fetch. Optional feedback preferences remain configurable on Settings (S23).

## Brand Identity Architecture
- **BrandMark**: Accessible SVG component representing the two rounded geometric tiles. Tile 1 is `#58C322` (lime) and Tile 2 is `#1677C9` (secondary sky blue). Tested and sharp across 16px, 24px, 32px, and 48px sizes. When rendered decoratively, `aria-hidden="true"` is applied. When standalone, an accessible label ("Duolify") is provided.
- **BrandWordmark**: Renders the lowercase brand name "duolify" in bold Nunito (900 weight), using token-defined color and tracking.
- **Brand**: Combines Mark and Wordmark into a unified link or header element.
- **App Icons & Manifest**:
  - `apps/client/public/icon.svg`: Canonical vector source.
  - `apps/client/public/manifest.webmanifest`: Web manifest with name, theme color `#1f7a0d`, background color `#f7f9f4`, and icon declarations.
  - `apps/client/src-tauri/icons`: Multi-platform Tauri icon assets generated from the approved vector mark.

## Optional Feedback Engine & Platform Bridge
- **Feedback Controller**: Exposes methods for user-triggered interaction cues: `playCue(type: "tap" | "completion" | "correction")`.
  - Sound: Since no audio assets ship for v1, audio cues execute as a silent no-op without any network requests.
  - Haptics: Executes `navigator.vibrate` or Tauri vibration bridge if supported, unless `draft.haptics === "off"` or `draft.motion === "reduce"` or OS prefers reduced motion.
  - Narration ducking: Narration remains the primary audio; UI cues duck below narration and never autoplay or punish mistakes.
- **Settings Screen (S23)**:
  - Exposes toggles for UI sounds and Haptics (`system`, `on`, `off`).
  - Persists changes via `profileApi.saveSettings`.
  - Supports keyboard navigation, screen reader accessibility, and responsive desktop/mobile viewports.
