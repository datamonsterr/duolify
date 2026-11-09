# Accessible static lesson recovery (E3-05)

## Why
Learners with reduced-motion preferences, missing or corrupt slide assets, or network playback interruptions must still receive full instructional meaning. The player must display a static visual frame with a complete explanation, provide accessible Retry and Exit recovery options without prematurely granting completion or test access, and preserve the learner's place across desktop and mobile platforms.

## What changes
- Manifest contract introduces a static instructional `explanation` field per slide in `ObjectSlide` (storage) and `Slide` (DTO), validated and backed up without leaking private assessment or storage keys.
- Client lesson player displays a meaningful static frame and full explanation when reduced motion is requested (OS preference or learner setting), disabling nonessential spatial animations while keeping controls, captions (at 200% zoom), and transcripts fully operable with keyboard and screen reader.
- When an asset is missing or slide document fails, the player renders the canvas-specified recovery stage ("This slide didn't load", "A diagram asset is missing. Your place is saved."), offers "Retry slide" and "Exit to path" actions, preserves position, keeps transcript accessible, and disables advancement so completion is never granted.
- The analytics allowlist and catalogue register `asset_unavailable` under `lesson_player_error` and `ui_recovery_selected` (`surface=lesson_player`, `error_code=asset_unavailable`, `action=retry|exit`), strictly enforcing UUID identity, common properties, opt-out, and zero private learner/slide text.
- Parity across desktop and mobile layouts (< 48rem sheets, >= 48rem panels), 320px reflow without horizontal scrolling, and light/dark theme contrast.

## Impact
- Affected areas: `services/api/internal/player`, `services/api/internal/analytics`, `apps/client/src/features/player`, `apps/client/src/features/lesson-player`, `apps/client/src/lib/analytics`.
- Specs: `lesson-player`, `analytics`, `design-system`.
- Data backup: manifest explanations join immutable object storage backups; learner preferences remain stored in `learner_profiles` under existing E1 backup/deletion policies.
- AI reliability: no new AI behavior or evals; existing gates remain preserved.
