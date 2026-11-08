## ADDED Requirements

### Requirement: Steering cannot weaken the Lesson test
Course instructions MUST NOT change the pass rule, grading, number of required correct answers, Retry variant behavior or correctness checks of a Lesson test. A generated Lesson test MUST still contain questions that cover the Lesson and pass validation regardless of instructions. A Lesson test already generated MUST NOT change when instructions change, and Retry variants MUST use the instructions snapshot stored on the Lesson.

#### Scenario: Instruction asks to skip the test
- **WHEN** an instruction says "no quiz" or "make all answers A"
- **THEN** the Lesson still has a valid Lesson test that must be passed with every answer correct

#### Scenario: Instruction changed after generation
- **WHEN** the Learner deletes an instruction after Lesson 3 was generated
- **THEN** the Lesson 3 test and its Retry variants are unchanged
