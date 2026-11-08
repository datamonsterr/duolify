## Context

Owner rules from 2026-11-08 (ADR 0007, ADR 0009) change five capabilities and add one. This design records decisions and a **proposal only** for contract changes. Proto, API and engine code are not edited by this change; the real edit needs a matching change in `proto`, `services/api` and `engine` (repo rule).

## Goals / Non-Goals

**Goals:** testable rules for Lesson test, Credits, freezes, narration; a contract proposal the implementers can review.
**Non-Goals:** prices, pack sizes, tiers, numeric allowance (E12); voice choice; client visual design (E13).

## Decisions

- **Pass rule**: a topic is the unit of mastery. Pass = every `topic_id` has a correct answer. No score threshold; `first_try_correct` is kept for analytics and the perfect-test XP bonus.
- **Termination**: variants cap (`max_variants_per_topic`), recap Slide, fallback to the source Question with reordered choices when generation fails. The test never blocks on the Engine.
- **Variant pool**: generate `variants_per_topic` (2) with the Lesson (one call, cheap, no mid-test latency); on-demand RPC covers the tail. Pool is stored in Postgres, not sent to the client until served, so the client cannot read answers.
- **Daily free credits**: grant row at first request of a local day (lazy, idempotent by learner and date), plus an `expire` worker at local day end. Lazy grant avoids a per-timezone cron fan-out.
- **Freeze**: purely an inventory counter with a cap; no Credit conversion on cap overflow.
- **Narration async**: Lesson is playable before audio exists; manifest status drives the player. Cost is charged per character, reserved before the call, zero on cache hit.
- **Hash key**: `sha256(text | voice_id | model_id | settings_json)`; objects `narration/<hash>.mp3` and `narration/<hash>.alignment.json`.

## Proposed contract changes (proposal only, not applied)

Files: `proto/duolify/engine/v1/engine.proto`. All changes are additive (buf breaking-safe) with new field numbers.

```proto
message Question {            // existing 1..5
  string topic_id = 6;        // groups a Question and its variants
  string variant_of = 7;      // question_id of the source; empty for originals
}

message TopicVariants {
  string topic_id = 1;
  repeated Question variants = 2;   // pool generated with the Lesson
}

message Lesson {              // existing 1..3
  repeated TopicVariants variant_pool = 4;
}

message Slide {               // existing 1..4
  // narration (4) stays: text written at generation time
  string narration_hash = 5;  // set by API after synthesis, not by Engine
}

service EngineService {
  rpc GenerateRetryVariants(GenerateRetryVariantsRequest) returns (GenerateRetryVariantsResponse);
  rpc SynthesizeNarration(SynthesizeNarrationRequest) returns (SynthesizeNarrationResponse);
}

message GenerateRetryVariantsRequest {
  LessonRef lesson = 1;
  Question source = 2;                 // missed Question, with answer
  repeated Question earlier_variants = 3;
  uint32 count = 4;
  repeated string history = 5;         // same shape as GenerateLessonRequest.history
}
message GenerateRetryVariantsResponse { repeated Question variants = 1; Usage usage = 2; }

message SynthesizeNarrationRequest {
  string slide_id = 1;
  string text = 2;
  string language = 3;
  // voice and model come from engine config, not from the caller
}
message SynthesizeNarrationResponse {
  bytes audio = 1;                     // mp3
  string audio_mime = 2;
  uint32 duration_ms = 3;
  repeated CharTiming alignment = 4;
  string voice_id = 5;                 // echoed so the API can compute the hash
  string model_id = 6;
  uint32 characters = 7;               // billed characters
  Usage usage = 8;                     // feature = narration
}
message CharTiming { string character = 1; uint32 start_ms = 2; uint32 end_ms = 3; }
```

Open point: audio as one unary `bytes` field is simplest for slides of a few seconds (well under gRPC's 4 MB default); switch to a server stream if a Slide may exceed that. The API computes the content hash from (text, voice_id, model_id, settings); the Engine must therefore report voice, model and settings, so the lookup can also be done before the call using config shared with the Engine (see open questions).

Alternative to an RPC for narration: a `GenerateLessonEvent` variant `NarrationReady` on the existing stream. Rejected: synthesis is async and cache-driven by the API, a separate RPC keeps `GenerateLesson` simple.

Client manifest (API to client JSON) gains per Slide: `narration_text`, `narration_status`, `audio_url`, `alignment_url`. API adds endpoints for test sessions: start or resume, submit answer (returns correction and next Question), skip to recap.

## Data model sketch

`test_sessions(id, learner_id, lesson_id, state, started_at, completed_at)`, `test_questions(session_id, question_id, topic_id, variant_of, position, status)`, `test_answers(session_id, question_id, choice, correct, answered_at)`. `credit_entries.kind` migration per ADR 0007. `narration_assets(hash, audio_key, alignment_key, characters, created_at)` and `slide_narration(slide_id, hash, status)`.

## Risks / Trade-offs

- Variant quality: paraphrase can change meaning. Mitigated by judge plus deterministic checks and a cap; cost is more golden cases.
- On-demand variants cost Credits mid-test; fallback to reordered source keeps the test finishable.
- Lazy daily grant means the grant appears at first request; a Learner offline all day gets none, which matches expiry anyway.
- Narration spend scales with characters; cache hit and per-Lesson cap limit it. Single TTS vendor: outage means captions only.
- Existing `lesson_test_failed` and `reward_credits_granted` events are removed; dashboards must be updated.

## Open Questions

- Numeric daily allowance, per-character narration price, spend order of subscription vs purchased Credits (E12).
- Hash computation: API-side with shared config vs Engine-reported voice and model.
- Second TTS provider for fallback instead of captions only.
- Whether a perfect Lesson test (`first_try_correct` = all) still earns bonus XP (kept in `gamification` as is).
