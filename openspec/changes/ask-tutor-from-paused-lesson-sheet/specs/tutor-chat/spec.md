## ADDED Requirements

### Requirement: Lesson tutor sheet with authorized current lesson context
The lesson player SHALL open the tutor in the shared responsive panel. Opening it MUST pause playback; closing it MUST restore focus to the opener and the same slide and playback time exactly once. The client MUST send only a lesson id and slide index; the API MUST verify that the lesson belongs to the authenticated Learner and to the Course in the URL, MUST build `current_lesson` from its own rows, and MUST answer 404 `not_found` for another Learner's, another Course's or an unknown lesson without charging, storing or calling the Engine. Another Learner's data MUST NOT enter the context. Learner text stays untrusted data in the prompt.

#### Scenario: Ask about the current lesson
- **WHEN** the Learner opens the sheet on a slide and sends a question
- **THEN** playback is paused, the prompt carries that lesson's title and goal, and closing the sheet returns focus, slide and time

#### Scenario: Foreign lesson id
- **WHEN** the request names a lesson of another Learner or another Course
- **THEN** the answer is 404 and no credit, message or Engine call results

### Requirement: Accessible streaming and draft recovery in the lesson sheet
Stop MUST be reachable by keyboard. The sheet MUST NOT announce tokens; it MUST announce only the start and the finish of a reply. Offline or with no eligible Credits the unsent draft MUST be kept, the next reset or top-up shown, and nothing resent automatically. Local drafts MUST be scoped to the signed-in Learner and cleared on logout.

#### Scenario: Zero credits
- **WHEN** the send is refused for credits
- **THEN** the draft stays in the composer, the next grant time and top-up link show, and no request repeats until the Learner acts

#### Scenario: Logout
- **WHEN** the Learner signs out
- **THEN** every stored tutor draft is removed and the next Learner on the device sees none
