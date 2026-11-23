## 1. Lesson test (E5)
- [x] 1.1 e2e: wrong answer shows correction, variant appears later, test ends only when all topics correct
- [x] 1.2 Integration: session resume, offline replay idempotent, variant limits and recap
- [x] 1.3 Unit: queue ordering (`min_gap`), pool exhaustion fallback, topic state
- [x] 1.4 Implement sessions, correction, variants; remove percentage threshold and whole-test retry
- [x] 1.5 Analytics events (lesson-test and analytics specs); backup notes in `docs/data-inventory.md`
- [x] 1.6 Golden-set cases `lesson_test`, `lesson_test_variant` with paraphrase checks and judge rubric

## 2. Credits (E7)
- [x] 2.1 e2e: daily grant, spend order, upsell on exhaustion
- [x] 2.2 Integration: idempotent daily grant, `expire` at local day end, reservation across midnight
- [x] 2.3 Unit: sizing check of the allowance against unit costs (3 notes + 5 long chat messages)
- [x] 2.4 Migration: new ledger kinds, drop `reward`; remove reward config and events (the `reward` kind stays in the CHECK only for historical rows; trigger `prevent_credit_reward_entry` blocks new writes, migration `20261109190000_credit_wallet.sql`)
- [x] 2.5 Analytics: `daily_credits_granted`, `daily_credits_expired`, `credits_upsell_shown`

## 3. Gamification and friend streaks (E6)
- [x] 3.1 Tests: automatic freeze at rollover, cap, no purchase path, per-side Friend streak freeze
- [x] 3.2 Implement milestone freeze grants for Streak and Friend streak; remove Credit rewards

## 4. Tutor chat and Quick notes
- [x] 4.1 Tests: daily allowance first, refusal with draft kept and upsell, pending enrichment resumes next day
- [x] 4.2 Implement client upsell screen and API error payload (`insufficient_credits` with next grant time)

## 5. Narration and player (E14)
- [x] 5.1 e2e: Lesson plays with narration, mute, speed, captions; caption-only fallback
- [x] 5.2 Integration: hash dedup, per-character charge, cache hit costs zero, failure releases reservation
- [x] 5.3 Unit: alignment validation, hash function, status transitions
- [x] 5.4 Check current ElevenLabs docs, add `SpeechProvider` (ElevenLabs and mock), `ELEVENLABS_API_KEY` in `.env.example`
- [x] 5.5 Proto change in `proto`, `services/api` and `engine` together (see `design.md`)
- [x] 5.6 Analytics events, backup and retention notes, golden-set cases `lesson_narration`

## 6. Specs housekeeping
- [x] 6.1 At archive: edit `Purpose` of `credits` and `gamification` to drop rewards
- [x] 6.2 Update `docs/data-inventory.md` for test sessions, ledger kinds, narration assets
