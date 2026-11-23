# Immersive lesson player design review (2026-11-23)

Mocked Playwright screenshots (`apps/client/e2e/{immersive-lesson,anchored-notes,region-questions,voice-input}.spec.ts`) at 390x844 portrait, 844x390 landscape and 1280x800 desktop, compared with `docs/design/design-system.md`, `region-selection.md` and `immersive-lesson-platforms.md`. The design canvas has no immersive screens, so tokens, target sizes (44 px), opaque panel and safe-area rules are the reference.

## Deviations found and fixed

1. Auto-send checkbox in the voice control rendered as a 44 px block above its text (the global `input` rule gave it full width and a minimum height). Now a 22 px checkbox on one row with its label (`voice.css`; `voice-input.spec.ts` asserts the size).
2. Landscape phone (844x390): the composer filled the answer panel and squeezed the reply and "Save as Quick note" out of view. The composer is now a compact grid under 500 px height and the log has a minimum height (`immersive.css`; `region-questions.spec.ts` asserts the reply is fully in view).

## Checked, no deviation

Opaque right-hand panel, region outline kept clear of the panel, controls overlay targets, side rail, segmented progress, Slide list, lock, anchored note with "Show in Lesson", push-to-talk idle, recording and countdown states. Portrait uses the rotated stage fallback and the tutor sheet. Native system bars, orientation lock and keep-awake belong to the Tauri bridge (follow-up, out of scope).
