## ADDED Requirements

### Requirement: Marketplace analytics events
The API SHALL emit `marketplace_browsed` (category, language, sort, result_count, has_cursor), `marketplace_searched` (query_length, category, language, sort, result_count) and `course_viewed` (course_id, version, source `marketplace`). Properties MUST be enums, UUIDs and integers. The search text, titles and descriptions MUST never be sent. The events MUST respect analytics opt-out, and the catalogue MUST list them before an emitter exists.

#### Scenario: Search sends length only
- **WHEN** a Learner searches "rust ownership"
- **THEN** `marketplace_searched` carries `query_length: 14` and no query text, and nothing is sent for an opted-out Learner
