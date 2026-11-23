# Analytics coverage

This audit is in progress. The table currently covers the events examined below; it is not yet an inventory of every capability event. Counts and final status cannot be inferred from this partial table. Lines refer to the branch containing this document.

Common properties are `source` (emitting tier), `platform`, and `app_version`. The learner UUID is the PostHog distinct ID. `entry_point` is feature attribution and is not the common `source` field. Opted-out learners must not produce events. Financial state and consent remain in backed-up Postgres, not PostHog.

| Event | Spec capability | Emitted where | Properties match? | Covered by a test? | Status |
| --- | --- | --- | --- | --- | --- |
| `learner_signed_up` | analytics | `services/api/internal/accounts/accounts.go:417`, `services/api/internal/accounts/sso.go:311` | `method`, common fields | Email and Clerk integration tests, analytics allowlist unit test | ok |
| `check_in_credits_granted` | analytics | `services/api/internal/credits/check_in.go:147` | `amount`, `first_time`, `banked_after`, `capped`, common fields; zero amount at cap | `services/api/internal/credits/daily_check_in_integration_test.go`, `services/api/internal/analytics/check_in_test.go` | ok |
| `action_quote_shown` | analytics | `apps/client/src/lib/analytics.ts:81` and quote surfaces | `quote_id`, `feature`, `pricing_version`, common fields | Wrapper and quote panel tested; other surface emitters not yet individually verified | untested |
| `action_quote_accepted` | analytics | `services/api/internal/pricing/quotes.go:524` | `quote_id`, `feature`, `pricing_version`, common fields | `services/api/internal/pricing/quote_actions_with_immutable_pricing_versions_integration_test.go`, `services/api/internal/analytics/signup_test.go` | ok |
| `onboarding_tour_started` | analytics | `apps/client/src/features/onboarding/CoachMarks.tsx:59`, `apps/client/src/lib/analytics.ts:1122` | `entry_point`, common fields | `apps/client/src/features/onboarding/analytics.test.ts` | ok |
| `onboarding_welcome_shown` | analytics | `apps/client/src/features/onboarding/Welcome.tsx:26` | Common fields | `apps/client/src/features/onboarding/analytics.test.ts` | ok |
| `onboarding_path_chosen` | analytics | `apps/client/src/features/onboarding/Welcome.tsx:45` | `path`, common fields | `apps/client/src/features/onboarding/analytics.test.ts` | ok |
| `onboarding_tour_step_viewed` | analytics | `apps/client/src/features/onboarding/CoachMarks.tsx:70` | `step`, common fields | `apps/client/src/features/onboarding/analytics.test.ts` | ok |
| `onboarding_tour_completed` | analytics | `apps/client/src/features/onboarding/CoachMarks.tsx:162` | Common fields | `apps/client/src/features/onboarding/analytics.test.ts` | ok |
| `onboarding_tour_skipped` | analytics | `apps/client/src/features/onboarding/CoachMarks.tsx:161` | `at_step`, common fields | `apps/client/src/features/onboarding/analytics.test.ts` | ok |
| `example_courses_viewed` | analytics | `apps/client/src/lib/analytics.ts:363` | `surface`, common fields; runtime enum guard | `apps/client/src/features/onboarding/analytics.test.ts` | ok |
| `example_course_enrolled` | analytics | `services/api/internal/sharing/examples.go:253` | `slug`, `surface`, `course_id`, common fields | `services/api/internal/sharing/example_courses_integration_test.go` | ok |
| `immersive_mode_entered` | analytics | `apps/client/src/features/lesson-player/immersive/ImmersiveHost.tsx:123` | `course_id`, `lesson_id`, `slide_index`, `trigger`, common fields | `apps/client/src/features/lesson-player/immersive/immersive_analytics.test.ts` | ok |
| `immersive_mode_exited` | analytics | `apps/client/src/features/lesson-player/immersive/ImmersiveHost.tsx:151` | Same as entry plus `seconds_in_mode` bucket | `apps/client/src/features/lesson-player/immersive/immersive_analytics.test.ts` | ok |
| `immersive_control_used` | analytics | `apps/client/src/features/lesson-player/immersive/ImmersiveLayer.tsx:65` | `control`, `via`, common fields | `apps/client/src/features/lesson-player/immersive/immersive_analytics.test.ts` | ok |
| `lesson_region_question_sent` | analytics | `services/api/internal/chat/turn.go:329` | `course_id`, `lesson_id`, `slide_index`, `input`, `has_region`, `grounding`, common fields | `services/api/internal/chat/ask_about_a_selected_slide_region_integration_test.go` | ok |
| `lesson_anchored_note_created` | analytics, quick-notes | `services/api/internal/notes/service.go` (`EmitAnchoredNoteCreated`) | `course_id`, `lesson_id`, `slide_index`, `entry_point` (`typed`/`voice`), `has_region`, common fields (`source=api`); no text or region | `services/api/internal/analytics/immersive_events_test.go`, `services/api/internal/notes/anchored_quick_notes_integration_test.go` | ok |
| `quick_note_created` | analytics, quick-notes | `services/api/internal/notes/service.go` (server-side, once per note) | `entry_point` (`slide`/`lesson`/`chat`), `length`, common fields | `services/api/internal/notes/save_raw_quick_notes_before_enrichment_integration_test.go` | ok |
| `$ai_generation` | analytics, AI capabilities | Multiple API emitters, including `services/api/internal/credits/reservations.go:593` | Partial: some failure paths lack trustworthy usage or error details | Outbox and emitter tests exist; full coverage remains unaudited | wrong-props |
| `voice_recording_finished`, `voice_recording_cancelled` | analytics, voice-input | `apps/client/src/lib/analytics.ts` (`trackVoice`), emitted from `apps/client/src/features/voice/useVoiceInput.ts` and `useDictationWindow.ts` | `purpose`, `entry_point` (`answer_panel`/`tutor_sheet`/`quick_note`), `duration_bucket` (`lt_2s`/`2_5s`/`5_15s`/`15_30s`), common fields (`source=client`); no audio | `apps/client/src/features/voice/voice_analytics.test.ts`, `useVoiceInput.test.tsx` | ok |
| `voice_transcript_used`, `voice_transcript_discarded` | analytics, voice-input | `apps/client/src/lib/analytics.ts` (`trackVoice`), emitted from `apps/client/src/features/voice/useDictationWindow.ts` and `apps/client/src/features/notes/QuickNotePanel.tsx` | `purpose`, `entry_point`, `edited`; no transcript text | `voice_analytics.test.ts`, `useDictationWindow.test.tsx`, `apps/client/src/features/notes/dictate_quick_note.test.tsx` | ok |
| `lesson_region_selected` | analytics, immersive lesson | No emitter found | Not applicable | No | missing |
| `lesson_answer_panel_closed` | analytics, immersive lesson | No emitter found | Not applicable | No | missing |

## Dashboards

These definitions describe intended PostHog insights; collection completeness is still being audited. Use distinct learners, exclude the reserved operational UUID, and restrict to consented event data.

1. **Activation funnel:** `learner_signed_up` → either `course_enrolled` or `example_course_enrolled` → `lesson_completed`; join by distinct ID and require the completion `course_id` to match the selected course where the tool supports it. Break down by signup `method` and course path (generated or example). Enrollment is the available proxy for an example being opened; repeat opens do not emit example enrollment.
2. **D1/D7 retention:** cohort on `learner_signed_up`, return on `check_in_credits_granted` (including `amount=0`); count distinct IDs on day 1 and day 7 after signup, broken down by signup `method`. PostHog event time supplies UTC day boundaries; learner-local return days cannot be reconstructed from this event alone.
3. **Credits spent by feature:** sum `credits_settled.amount` over time, broken down by `feature` and `pricing_version`. Include only settled transactions and do not sum reservations or releases as spend.
4. **AI task success and fallback rate:** group `$ai_generation` by `feature`, `$ai_provider`, and `$ai_model`; count all attempts and compute shares with `$ai_is_error=true` and `fallback_used=true`. Plot `$ai_latency` p50/p95 separately. Coverage and missing usage must be audited before treating these as complete rates.
5. **Lesson completion rate:** unique distinct-ID / `course_id` / `lesson_id` combinations with `lesson_completed` divided by those with `lesson_started` in the chosen cohort and period. Break down by `course_id`, with separate created-course and example-course cohorts joined through their enrollment events. Confirm started-event coverage before using this as a product rate.

Quote conversion can join `action_quote_shown` and `action_quote_accepted` by `quote_id`, distinct ID and `feature`. Server-created automatic quotes may be accepted without a display and should not enter the shown-quote denominator.

## Metrics these events enable

- **Activation funnel:** `learner_signed_up` -> `course_enrolled` (or `example_course_enrolled`) -> `lesson_started` -> `lesson_completed` -> `lesson_test_passed`, by signup `method` and `platform`.
- **Retention:** D1/D7 via `check_in_credits_granted` (zero-credit capped check-ins still count as attendance); streak health by `first_time`/`capped`.
- **Monetisation:** quote conversion (`action_quote_shown` -> `action_quote_accepted` by `quote_id`), `credits_settled` by feature and `pricing_version`, `credits_insufficient` rate.
- **Notes engagement:** `quick_note_created` by `entry_point`; anchored-note share via `lesson_anchored_note_created` (`typed` vs `voice`, `has_region`).
- **AI quality and cost:** `$ai_generation` error, fallback and latency by feature/provider/model.

Attribution rule: `source` is the emitting tier and `entry_point` is the UI place or feature origin that triggered the event. Neither carries email or free text.

## Known gaps and privacy review in progress

- The region-selection and answer-panel-close events have no emitter in the current client; their UI flows must exist before their transitions can be measured.
- AI attempts are not yet fully audited. Quick-note enrichment previously emitted duplicate incomplete `$ai_generation` events; the direct emission has been removed in favor of the financial outbox. Its failure path still needs review for unknown provider usage and error attribution. Do not infer complete per-task success, latency, cost or fallback rates from the current stream.
- The catalogue prohibits email, free text, lesson content and raw prompts. The client boundary and server allowlist, consent propagation and every emitter still require a complete review before the privacy audit can be considered finished.
