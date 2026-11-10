# Design: Accessible Static Lesson Recovery

## Manifest Contract Extension
Each slide in the lesson manifest provides a static instructional `explanation`:
- `ObjectSlide`: `Explanation string` (`json:"explanation,omitempty"`), bounded to 20,000 characters.
- `Slide`: `Explanation string` (`json:"explanation"`). When unpopulated in historical storage, projects `NarrationText` as fallback so an explanation is always present.
- Manifest validation enforces that `explanation` is bounded, contains no private storage paths or assessment answer keys, and preserves instructional fidelity.

## Reduced Motion & Static Explanation
1. UI respects both `prefers-reduced-motion` and user profile preference (`motion: "reduce"`).
2. When reduced motion is active:
   - Sandboxed iframe receives `set_motion` with `reduced: true` and document root sets `data-motion="reduce"`.
   - Motion CSS and Web Animations are suspended; the slide presents a clear, static visual frame.
   - The full instructional explanation is displayed alongside the frame and remains accessible in captions and transcripts.
   - Playback controls remain operational with keyboard shortcuts (Space to play/pause, Left/Right arrow to seek).
   - Layout reflows at 320px without horizontal scroll; caption text supports 200% font resizing with contrast >= 4.5:1.

## Missing Asset and Slide Recovery
1. When a slide asset fails to load, is missing from storage, or a slide document encounters an unrecoverable failure:
   - The player halts active playback and enters the slide recovery state matching canvas `S09-Player.dc.html` / `D2-Player.dc.html`.
   - Stage renders a dashed-border card with an alert icon, title "This slide didn't load", and explanatory text: "A diagram asset is missing. Your place is saved."
   - Two recovery actions are presented: "Retry slide" and "Exit to path" (or Exit lesson).
   - The current slide's transcript remains accessible below the stage or via the transcript panel/sheet.
   - The "Next" navigation control is disabled ("Next · unavailable"), preventing premature completion or access to the lesson test.
   - The "Previous" navigation control remains operable when `index > 0`, allowing learners to navigate back to prior working slides.
   - Selecting "Retry slide" re-attempts loading the document/assets and emits `ui_recovery_selected` (`action: "retry"`).
   - Selecting "Exit to path" persists current position and returns to the originating route, emitting `ui_recovery_selected` (`action: "exit"`).

## Analytics Catalogue and Allowlist
- `lesson_player_error` allowlist is extended to include `asset_unavailable` and `slide_failure`.
- `ui_recovery_selected` is catalogued with `surface: "lesson_player"`, `error_code: "asset_unavailable"`, and `action: "retry" | "exit"`.
- Events require authenticated UUID identity, common properties (`platform`, `app_version`, `source`), respect opt-out, and strictly exclude learner text, slide HTML, or URLs.
