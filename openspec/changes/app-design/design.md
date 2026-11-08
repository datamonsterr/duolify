# Design decisions

## Scope and source precedence
Read `CONTEXT.md`, ADRs 0005, 0007, 0009, E13, all capability specs and the current client. Preserve existing `/`, `/courses`, `/course/$courseId`, `/lesson/$lessonId`, `/profile`, `/credits` route shapes. Reuse the scaffold's sidebar/content/support-rail structure and sandboxed Motion HTML concept. Remove hearts from the design baseline.

The owner's request and glossary require every original question and each queued retry variant to be resolved correctly. Wrong answers show a correction, then a paraphrased question with paraphrased choices later; they do not lose a life. The older `lesson-test` spec's default 80% threshold and reordered-identical retries are stale. ADR 0007 supersedes reward-credit passages in credits, gamification, friend-streaks and analytics. Effort earns XP, badges and freezes; daily free credits expire locally and are spent before purchased credits. No legacy capability is edited within this restricted change.

## Visual decisions
Warm paper, ink-violet actions, apricot highlights, geometric orbit illustrations. Use familiar learning-path hierarchy with original shapes and copy. Humanist system typography avoids network fonts. Physical button depth gives action feedback without mandatory animation. Semantic tokens pair foregrounds with specific surfaces in both modes; `design-system.md` contains calculated contrast evidence.

## Artifacts and boundaries
`tokens.json` follows the DTCG 2025.10 format (a W3C Community Group report, not a W3C Recommendation). Light and dark are explicit groups; shared dimensions and timing have typed values. `theme.css` maps these to Tailwind v4 `@theme inline` aliases and media/data-theme overrides. The offline HTML embeds the same CSS values; it does not depend on Tailwind or network resources. A sandboxed iframe holds an authored Motion HTML slide. A parent clock demonstrates aligned motion, captions and fake narration playback.

The prototype is intentionally four core screens plus finish: path, player, lesson test, paywall. Auxiliary note/tutor actions open reviewable demo sheets. No real audio synthesis, generation, grading, payment, analytics or persistence happens. Production correctness remains server-owned.

## Accessibility and responsiveness
Use native controls, a skip link, visible focus, 44px targets, 320px reflow, scalable text, logical properties and wrapping. Sidebars become bottom navigation below 48rem; the support rail appears at 75rem. Player and test use a focused shell. Dialogs restore trigger focus, Escape closes, headings receive focus after navigation. Narration never starts automatically. Reduced motion removes spatial transitions and shows a static slide with the same explanatory text.

## Verification
Write browser acceptance checks before prototype implementation, then token/contrast integration checks and numeric unit fixtures. Validate with pinned OpenSpec through mise and `mise run ci:openspec`. Run the offline file in Chromium with network disabled, desktop/mobile, light/dark, keyboard, reduced motion, wrong-answer/retry, all-correct, repeated-error and paywall cancel/pending states. Record evidence in `docs/design/verification.md`.

## Risks and follow-up
Resolve spec drift in separate feature changes before production. E12 supplies actual prices, allowances and mobile billing rules. Shared-friend-day freeze policy needs explicit config UX before E6. Static checks and Chromium are design evidence, not a complete assistive-technology or Tauri audit. Review screen designs with learners before wiring the app.
