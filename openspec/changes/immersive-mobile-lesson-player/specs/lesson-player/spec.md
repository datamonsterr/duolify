## ADDED Requirements

### Requirement: Slide regions in the Slide manifest
The Slide manifest SHALL be able to declare, for each Slide, a list of regions. Each region MUST have a stable id within the Slide, a box in normalized stage coordinates, an optional time window on the Slide clock, a role from a closed set (title, explanation, example, diagram, code, question, caption) and a plain-text description of what the region shows. Publication MUST reject a manifest whose regions lie outside the stage, exceed the configured count per Slide or text length per region, contain markup or control characters, or use an unknown role. A manifest without regions MUST remain valid, and Lessons published before this requirement MUST keep playing unchanged.

#### Scenario: Valid regions are published
- **WHEN** a generated Lesson declares five regions on a Slide, all inside the stage and within the limits
- **THEN** the manifest is published with those regions

#### Scenario: Region outside the stage
- **WHEN** a region's box extends beyond the stage
- **THEN** publication rejects the Lesson and no manifest is written

#### Scenario: Older Lesson
- **WHEN** a Lesson whose manifest has no regions is opened
- **THEN** it plays as before and region questions fall back to whole-Slide grounding

### Requirement: Regions are generated with the Lesson
Lesson generation SHALL emit regions for every Slide it generates, describing the content the Motion HTML places in each box. Regions MUST be validated together with the Slide, and a Lesson MUST NOT be published when its Slides are valid but its regions are not, after the allowed attempts. Authored Lessons MAY omit regions.

#### Scenario: Regions fail validation
- **WHEN** generation returns a Slide whose regions are invalid on every allowed attempt
- **THEN** the generation job fails, the reservation is released, and no Lesson is published

### Requirement: Stage contract for full-screen presentation
The player SHALL present a Slide at any size by scaling its 16:9 stage uniformly, without changing the Slide document, its clock or its sandbox. Presenting a Slide framed, full-screen or rotated MUST NOT alter the sandbox attributes or the content security policy, and the host MUST be able to place its own layers above the Slide iframe.

#### Scenario: Same Slide, two presentations
- **WHEN** a Slide is shown framed and then full-screen
- **THEN** the same Slide document keeps running on the same clock and its layout is identical apart from uniform scale
