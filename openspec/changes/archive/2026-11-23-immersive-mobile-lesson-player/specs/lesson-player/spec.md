## ADDED Requirements

### Requirement: Stage contract for full-screen presentation
The player SHALL present a Slide at any size by scaling its 16:9 stage uniformly, without changing the Slide document, its clock or its sandbox. Presenting a Slide framed, full-screen or rotated MUST NOT alter the sandbox attributes or the content security policy, and the host MUST be able to place its own layers above the Slide iframe.

#### Scenario: Same Slide, two presentations
- **WHEN** a Slide is shown framed and then full-screen
- **THEN** the same Slide document keeps running on the same clock and its layout is identical apart from uniform scale
