## ADDED Requirements

### Requirement: Confidence tap
Before submitting a test, retry or review answer, the Learner SHALL be able to tap "Sure", "Think so" or "Guess" (optional; no tap is recorded as none). The level SHALL be stored with the attempt. A wrong answer marked "Sure" SHALL show a highlighted "Surprise" correction with the explanation, and the topic SHALL be due for Review the next day. Confidence MUST NOT change pass rules or Credits.

#### Scenario: Confident miss
- **WHEN** a Learner taps "Sure" and answers wrong
- **THEN** the correction is highlighted and the topic schedule is due the next day
