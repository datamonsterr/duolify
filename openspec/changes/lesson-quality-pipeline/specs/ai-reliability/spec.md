## ADDED Requirements

### Requirement: Lesson quality rubric
`engine-eval` SHALL score `lesson_slides` and `course_outline` cases on the rubric in `design.md` (R1 to R10): static checks for R1, R3, R9 and R10, and an LLM judge for R2, R4 to R8. The judge SHALL make one binary call per criterion, quote evidence from the output, and use a model family different from the generator route. Per-criterion pass rates SHALL be reported and gated in `evals/thresholds.toml`. Quality thresholds MUST be met before the `course_plan` route or the teaching core is promoted.

#### Scenario: Judge on the same family
- **WHEN** the judge route and the generator route are the same model family
- **THEN** the eval refuses to run the judge

#### Scenario: Degraded anchor
- **WHEN** the judge scores a degraded anchor (worked example removed, distractors made implausible)
- **THEN** the matching criteria fail

### Requirement: Judge calibration
A judge model or rubric change SHALL be accepted only when it agrees with owner-labelled anchors at Cohen's kappa of at least 0.6 per criterion, on at least 20 anchors. Anchors come from the authored example Courses and their degraded copies.

#### Scenario: Low agreement
- **WHEN** a new judge model scores kappa 0.4 on R7
- **THEN** the judge change is rejected and the previous judge stays

### Requirement: Course plan route selection
`engine-eval` SHALL run an A/B over the `course_plan` candidates in `design.md` D2 on at least 10 live outline cases, and SHALL select the cheapest candidate within 0.05 of the best mean judge score. The selection SHALL be recorded as promotion evidence before `models.toml` changes.

#### Scenario: Cheaper candidate close enough
- **WHEN** Haiku scores 0.88 and Sonnet 0.91
- **THEN** Haiku is selected

### Requirement: Lesson quality golden rows
The golden set SHALL contain at least 30 `lesson_slides` cases covering programming, languages, maths, sciences, humanities and practical skills, the three lesson lengths, at least 3 non-English locales, and cases with and without plans and history, plus at least 10 `course_outline` planner cases and 5 clarifying-question preview cases (some where none should be asked).

#### Scenario: Run gate
- **WHEN** `mise run engine:eval` runs on the live set
- **THEN** every new case passes its deterministic checks and the quality thresholds

### Requirement: Narration pace bound
Generated narration SHALL be at most 3.3 words per second of `duration_ms`, and the lesson prompt SHALL ask for at least 330 ms per narration word.

#### Scenario: Fast narration
- **WHEN** a Slide has 40 narration words and `duration_ms` 10000
- **THEN** validation fails
