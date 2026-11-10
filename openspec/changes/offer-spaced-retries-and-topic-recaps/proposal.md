# Offer spaced retries and topic recaps

## Why
E4-01 established initial lesson generation and testing, and E4-02 introduced topic linkages and pre-generated retry variant pools. Ticket E4-03 implements spaced retries and topic recaps: when learners make mistakes, retry variants are queued with server-side spacing (`min_gap = 2` where possible). If a learner misses a topic's original question and `max_variants_per_topic = 3` variants in a row, the app offers a taught-slide recap without marking the topic correct. Furthermore, when `max_variants_per_test = 12` variants are served in a single test session, the app offers the learner to leave and resume later. A lesson test passes only when every topic has been resolved and no unresolved retry obligations remain.

## What Changes
- Enforce server-side spaced retry queue ordering with configurable `min_gap = 2`, `max_variants_per_topic = 3`, and `max_variants_per_test = 12` pulled from configuration rather than scattered literals.
- Persist question queue state, variants served, resolved topics, and first-try correctness across answer submissions and test session resumption in Postgres.
- Offer taught-slide recap when consecutive wrong variants on a topic reach `max_variants_per_topic = 3`; viewing the recap provides taught slides without resolving the topic.
- Offer leave/resume when variants served in a test session reach `max_variants_per_test = 12`; leaving preserves the in-progress session.
- Only an empty unresolved-topic/variant obligation set can complete the test.
- Match canvas S10-Test desktop and mobile states: clear correction sheet with explicit Continue, persistent retry queue count chip, topic resolution progress, accessible keyboard focus (keys 1-N, Enter) and touch transitions.
- Emit allowlisted analytics: `lesson_test_answer_wrong`, `retry_variant_queued`, `retry_variant_served`, `retry_variant_answered`, `lesson_test_recap_shown`, and `ui_test_retry_presented` without learner text.
- Maintain data retention in Postgres, nightly backup fixtures, and account deletion erasure within 30 days.

## Impact
API lessontest service queue logic and endpoints, client LessonTest component and test route, analytics allowlist and catalogue, configuration defaults, and backup coverage. Pass/completion path and next-lesson preparation remain scoped to E4-05.
