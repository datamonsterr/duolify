## ADDED Requirements

### Requirement: Topic schedule
The API SHALL keep a schedule per Learner and taught topic, created when the Learner passes the Lesson that teaches it. Steps are 1, 3, 7 and 21 days. A first-try correct answer in a Review session advances one step; a wrong answer resets to step 1; a wrong answer marked "Sure" makes the topic due the next day. A topic retires after first-try correct recall in 3 separate sessions. Schedules are Learner progress data: backed up nightly, exported and erased with the account.

#### Scenario: Pass creates schedules
- **WHEN** a Learner passes a Lesson that taught 3 topics
- **THEN** 3 schedules exist, each due 1 day later

#### Scenario: Miss resets
- **WHEN** a Learner misses a step-3 topic in Review
- **THEN** the topic returns to step 1, due 1 day later

### Requirement: Daily Review session
When at least one topic is due, Home SHALL offer Review. A session SHALL have at most 10 questions from stored question variants: due topics first, oldest due first, at most 2 per topic, mixed across Lessons and Courses. It uses no generation and no Credits. Completing a session with at least 5 answers, or all due topics, SHALL count as the day's activity for the Streak.

#### Scenario: Nothing due
- **WHEN** no topic is due
- **THEN** Home shows no Review prompt

#### Scenario: Review keeps the Streak
- **WHEN** a Learner completes a Review session and no Lesson that day
- **THEN** the Streak counts the day
