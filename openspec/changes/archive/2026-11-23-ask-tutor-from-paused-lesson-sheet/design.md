## Context
`assemble` builds the Engine request only from rows keyed by learner and Course. Lesson stubs link to a Course through `course_units`.

## Decisions
- Lesson verification: one query `course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id WHERE l.id=$1 AND l.learner_id=$2 AND u.course_id=$3 AND u.learner_id=$2`; no row means 404 `not_found`, identical for foreign, other-Course and unknown ids. Only title and goal of that stub become `current_lesson`.
- `slide_index` is validated (0..9999) and used only for analytics-free UI state; it is not sent to the Engine.
- Closing restores state: the player records `{slide, time}` at open and re-applies them once on close if they drifted, then focuses the opener. It does not auto-resume; the learner presses Play (conservative default recorded in ADR 0011).
- Live region: the log is not `aria-live` while streaming; a separate polite status announces "Tutor is answering" and "Tutor replied" once each.
- Drafts: `sessionStorage` key `tutor-draft:<learner>:<course>`; `session.clear()` removes every `tutor-draft:` key.
