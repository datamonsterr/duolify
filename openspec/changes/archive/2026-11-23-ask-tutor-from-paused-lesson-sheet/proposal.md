# E8-04: Ask the tutor from a paused lesson sheet

## Why
The lesson player's "Ask tutor" panel is a placeholder. A learner stuck on a slide must leave the lesson to ask, losing place, and the tutor cannot see which lesson they are on.

## What Changes
- API: `POST /v1/courses/{id}/tutor/quote` and `/tutor/messages` accept an optional `lesson` object (`lesson_id`, `slide_index`). The API verifies the lesson belongs to the authenticated Learner and to that Course (otherwise 404, nothing charged or stored) and fills `ChatRequest.current_lesson` from its own rows. The client never sends lesson text. No proto, migration or Engine change (`current_lesson` already exists).
- Analytics: `tutor_message_sent` gains boolean `has_lesson_context`; `ui_sheet_opened` accepts `sheet_id=lesson_tutor`; `ui_recovery_selected` is emitted for tutor recovery actions (`surface=lesson_tutor`, `action` in `retry`, `reload`, `top_up`, `read_transcript`). Allowlist and catalogue precede emitters. Never text.
- Client: the player's Ask tutor panel hosts the existing tutor (`TutorChat`, `useTutor`) in the shared responsive panel. Opening pauses playback; closing restores focus to the opener and the same slide and playback time, exactly once. Stop stays keyboard reachable; the live region announces start and finish only. Drafts are kept offline or at zero credits without resend, are learner-scoped and are cleared on logout.
- Data: local drafts (`sessionStorage`, learner-scoped) listed in the data inventory.

## Impact
API chat handlers, analytics allowlist, client chat and player. Prices unaffected. AI behaviour unchanged.
