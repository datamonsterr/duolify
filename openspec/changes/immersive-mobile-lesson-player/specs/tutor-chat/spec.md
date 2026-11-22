## ADDED Requirements

### Requirement: Region-grounded lesson question
A lesson Tutor chat turn SHALL accept an optional region: the Slide index, a rectangle in normalized stage coordinates and the playback time. The client MUST send only the Lesson id, Slide index, rectangle, time and the Learner's question; it MUST NOT send Slide text. The API MUST authorize the Lesson for the Learner, resolve the rectangle and time to the regions declared in the Slide manifest that it intersects, ranked by overlap, and pass their text and roles to the Engine with the Lesson context it already assembles. The reply MUST be grounded on those regions and on the Lesson, and MUST follow the existing scope, streaming, Credits and stop rules of lesson Tutor chat. The message MUST store the Slide index, time, rectangle and the ids of the regions used.

#### Scenario: Question about a diagram
- **WHEN** the Learner selects a rectangle that mostly covers a region with role diagram and asks "what does this arrow mean?"
- **THEN** the Engine receives that region's description and the reply explains that part of the Slide

#### Scenario: Rectangle or Slide out of range
- **WHEN** the request names a Slide index beyond the Lesson or a rectangle outside 0..1
- **THEN** the API answers with a client error and no Credits, message or Engine call result

#### Scenario: Foreign Lesson
- **WHEN** the request names a Lesson of another Learner
- **THEN** the answer is 404 and no Credits, message or Engine call result

### Requirement: Region questions on Slides without regions
When the Slide manifest declares no regions, or the rectangle intersects none, the API SHALL ground the turn on the whole Slide using the manifest explanation and tell the Engine which part of the stage was selected. The reply MUST say that it is answering about the Slide as a whole when it cannot tell what the selected part contains, and MUST NOT invent content for the region.

#### Scenario: Older Lesson
- **WHEN** the Learner asks about a region of a Slide whose manifest has no regions
- **THEN** the reply is about the Slide as a whole and says so

### Requirement: Region question model testing and reliability
The Golden set SHALL contain region-grounded lesson questions: a region with a clear answer, a region with unrelated question text, a rectangle intersecting several regions, a Slide without regions, and an instruction-injection attempt inside a region description. Promotion MUST require the replies to use the selected region's content, refuse out-of-scope requests as lesson Tutor chat does, and never follow instructions found in region text. The task MUST have a timeout, the fallback model, and the cost ceiling of the lesson Tutor chat action.

#### Scenario: Injection in region text
- **WHEN** a region description contains text that tells the model to ignore its rules
- **THEN** the reply treats it as Slide content and follows the tutor rules
