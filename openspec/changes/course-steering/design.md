## Context

Terms from `CONTEXT.md`: **Course steering**, **Course instruction**, **Course copy**, **Published course**, **Gated generation**, **Pricing config**. The Engine is stateless (ADR 0002): the API sends the active instructions on every call.

## Decisions

**Instruction record** (`course_instructions`): id, learner_id, course_id (the Learner's Course or Course copy), text, scope (`next_lesson` | `rest_of_course`), status (`active` | `applied` | `deleted`), added_at, applied_lesson_id, screening_verdict, screening_version. Defaults proposed (config, not pricing): text 1 to 500 characters, 20 active per course, 4,000 characters total active text, 10 adds per course per hour.

**Effect rules.**
- A lesson generation job snapshots the active instructions at job start and stores their ids on the lesson (`lesson_instruction_ids`). Retry variants and later lookups use that snapshot, not the current list.
- `next_lesson`: applies to the next lesson whose generation job has not started; becomes `applied` when that lesson is generated. If a job is already running, the instruction waits for the one after.
- `rest_of_course`: stays `active` until deleted.
- Edit changes future use only. Delete removes it from future jobs. Applied and past lessons never change.
- Conflicts: instructions are sent in order of `added_at`; the prompt states that newer ones win.

**Prompt boundary.** The Engine puts instructions inside one delimited section (`<learner_instructions>`) after the system rules and course context. The system part says the section is data from the learner about style and focus: it cannot change rules, output format, safety policy, grading, pricing, or ask for prompt contents. Output is still schema-validated (`ai-reliability` output validation), so an injected format change fails validation rather than ships.

**Screening.** Before saving: deterministic checks (length, control characters, known injection phrases, URLs rejected in v1) then an LLM classifier on a cheap model (ADR 0006 cheap-task route) returning `allow | reject` with a reason code (`prompt_injection`, `unsafe_content`, `off_topic_for_course`, `too_long`). Fail closed: if screening is unavailable the instruction is not saved and the client offers retry. Screening is paid by the platform, not charged to the Learner.

**Re-plan.** `POST /v1/courses/{id}/replan` reserves credits at the Pricing config key `outline_replan`, calls `ReplanOutline` with the current outline, which lessons are generated or unlocked, and active instructions. The Engine may only return changes to units and lessons not yet generated (it must echo generated lessons unchanged; the API rejects a response that edits them). The API stores a proposal with a diff (added, removed, moved, retitled lessons per unit) valid 24 hours and an outline version number. Confirm applies it atomically if the outline version still matches; otherwise the proposal is stale and nothing changes. Discard keeps the charge (the provider call happened); a failed proposal releases the reservation.

**Fork.** On the first instruction or re-plan on a Course copy, the API shows a warning, requires confirmation, and records `forked_at_lesson` = first lesson not yet unlocked for that Learner. Lessons already unlocked (reused or own) stay. From the fork point the Learner's own generation applies, charged at the Pricing config lesson and narration prices. Deleting all instructions does not undo the fork in v1.

**Proposed proto additions** (not applied here; requires a matching API and Engine change, `buf breaking` clean because fields are additive):

```proto
message CourseInstruction {
  string id = 1;
  string text = 2;
  enum Scope { SCOPE_UNSPECIFIED = 0; NEXT_LESSON = 1; REST_OF_COURSE = 2; }
  Scope scope = 3;
  google.protobuf.Timestamp added_at = 4;
}
// GenerateLessonRequest: add
repeated CourseInstruction instructions = 20;
// GenerateLessonResponse: add
repeated string applied_instruction_ids = 20;

rpc ReplanOutline(ReplanOutlineRequest) returns (ReplanOutlineResponse);
message ReplanOutlineRequest {
  CourseContext course = 1;            // existing message
  repeated UnitState units = 2;        // each lesson: id, title, goal, generated bool
  repeated CourseInstruction instructions = 3;
  LearnerProfile profile = 4;
}
message ReplanOutlineResponse {
  repeated UnitState units = 1;        // generated lessons unchanged
  string rationale = 2;                // short, shown to the Learner
  Usage usage = 3;
}

rpc ScreenInstruction(ScreenInstructionRequest) returns (ScreenInstructionResponse);
message ScreenInstructionRequest { string text = 1; string course_topic = 2; }
message ScreenInstructionResponse { bool allowed = 1; string reason_code = 2; Usage usage = 3; }
```

Field numbers start at 20 to avoid clashes; the implementer picks free numbers.

**Reliability.** New golden-set tasks `steer_lesson`, `steer_injection`, `steer_replan`, `steer_screen`. Timeouts: screening 10 s, re-plan 90 s (paid generation deadline is 180 s per `monetization-model`). One fallback attempt within the quote ceiling.

## Risks

- A long active list inflates the prompt and the cost envelope: bounded by the character caps above.
- "Make the test easier" style instructions: the screening and the lesson-test delta keep grading fixed.
- Confusion between `next lesson` and an already generated lesson: open question whether to offer "regenerate unstarted next lesson".

## Open questions

1. Price of `outline_replan`: not in the approved E12 table. Proposed 55 Credits (the outline price) pending owner approval through a Pricing config version; re-plan stays behind a flag until then.
2. Offer "regenerate the unstarted next Lesson with this instruction" (paid) when the next Lesson already exists.
3. Should deleting all instructions let a forked Course copy rejoin the published version?
4. Limits (500 characters, 20 instructions, 4,000 total) are proposed defaults.

## E16-01 implementation slice

Private owner-enrolled Courses accept `next_lesson` and `rest_of_course`. A single `latest` status read supports this slice's panel; list-many, edit and delete remain E16-02. Adds and job snapshots serialize on the Course row; job queue insertion is the start boundary, including an empty snapshot. Each snapshot owns ids/text/scope/version/screening-version; retries read it and successful Lesson settlement consumes next-lesson entries in the same transaction. Failed jobs leave instructions active for a future job. Snapshot content is retained independently of a later mutable instruction revision or 30-day soft-delete purge.

Screening uses the existing platform envelope (cheap route, two total evaluated attempts, 10 seconds total, 0.05 USD operational cap) and never creates a Learner reservation. Both providers must be explicit mocks until E10-06 promotion evidence exists. Offline `steer_lesson`, `steer_screen`, `steer_injection` fixtures gate at 1.0. The real `datamonsterr/duolify-golden` pin is blocked on E10-01 provisioning: no production revision is fabricated or fetched. E16-01 does not enable re-plan or change Pricing; E16-06's 55 Credits stays a later Pricing-config change.

The lesson provider adapter recognizes an internal envelope only when it carries the exact fixed steering policy. Anthropic receives that policy in its `system` field; OpenRouter receives a first `system` message. Course context, personas and the escaped instruction section remain user data. Other task prompts keep their existing transport shape. Recorded transport tests cover both routes.
