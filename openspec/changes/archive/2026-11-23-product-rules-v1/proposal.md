## Why

The owner fixed product rules on 2026-11-08 (ADR 0007, ADR 0009, `CONTEXT.md`): a Lesson test passed only by answering every question correctly, daily free credits instead of reward credits, freezes earned by effort, and spoken narration for every Slide. The E0 specs still describe an 80% pass threshold, `reward` credits, bought freezes and silent Slides. This change aligns the specs before E3 to E14 start building.

## What Changes

- **lesson-test**: pass only when every topic has a correct answer; immediate correction on a wrong answer; *Retry variants* (paraphrased question and choices) queued later in the same test; variant limits and recap Slide; engine variant generation; analytics; golden-set paraphrase checks. Percentage threshold and whole-test retry are removed.
- **credits**: ledger kinds per ADR 0007 (`daily_grant`, `subscription_grant`, `expire` added; `reward` removed); Daily free credits sized for 3 Quick notes and 5 to 8 Tutor chat messages, expiring at the Learner's local day end, spent first; **BREAKING** Reward credits removed.
- **gamification** and **friend-streaks**: Streak freezes earned by effort, applied automatically, never bought or rewarded as Credits; each Learner's own freeze protects their side of a Friend streak; Friend streak milestone Credit rewards removed.
- **tutor-chat** and **quick-notes**: spend Daily free credits first; defined behavior with an exhausted allowance (upsell, no silent failure).
- **narration** (new): per-Slide narration text, ElevenLabs TTS via `SpeechProvider`, content-hash storage, captions, mute, speed, caption-only fallback, per-character Credit cost, analytics, backup, reliability.
- **lesson-player**: plays Narration in sync with Slides.
- **analytics**: catalog updated (variant, daily-credit, upsell, narration events; `reward_credits_granted` removed).
- `design.md` proposes proto changes only. No proto or code is edited here.

## Capabilities

### New Capabilities
- `narration`: spoken audio and captions for each Slide.

### Modified Capabilities
- `lesson-test`, `credits`, `gamification`, `friend-streaks`, `tutor-chat`, `quick-notes`, `lesson-player`, `analytics`.

## Impact

- Areas: specs now; later api, engine, client, proto (see `design.md`).
- ADRs relied on: [0006](../../../docs/adr/0006-llm-providers-and-evals.md), [0007](../../../docs/adr/0007-credits-and-payments.md), [0009](../../../docs/adr/0009-narration-elevenlabs.md).
- Epics: E5 (lesson test), E6 (gamification), E7 (credits), E14 (narration). Prices, pack sizes and the numeric allowance come from the separate monetization change (E12); this change defines no prices.
- The `Purpose` paragraphs of `credits` and `gamification` still mention rewards; fix them by a direct edit when this change is archived (delta files cannot change Purpose).
