## Why

Learners want familiar Characters that reflect their interests and accompany them across Lessons and Tutor chat. A prompt-based creator with a consistent, animatable appearance makes those Characters usable in Motion HTML without the cost and inconsistency of generating new images for every Slide.

## What Changes

- New `custom-characters` capability (E17): create and regenerate from a prompt describing personality, appearance and info; manually edit structured fields; manage private Characters and pick a Cast per Course.
- An LLM returns a validated `CharacterSpec` for an original parametric SVG Character rig, with five expressions. A short bounded persona accompanies generation and Tutor chat; voice keys map to the ElevenLabs allowlist.
- Ship a few original built-in Characters as the Default cast. Never copy or reference another game's characters, names or art.
- Input/output screening, per-Learner limits, prompt-injection boundaries, immutable versions and Default cast fallback protect learning. Test reactions never change grading.
- Creation and prompt-based regeneration cost Credits through the proposed `character_create` Pricing config key; manual edits are free. Owner approval of the new key and price is required before enabling paid creation.
- Published courses snapshot their attached Characters under E15; all other Characters remain private. Add PostHog event definitions, backup/retention and Golden set reliability requirements to each affected capability.

## Capabilities

### New Capabilities

- `custom-characters`: prompt creation, structured editing, original rig and Default cast, safety, Cast selection/versioning, Credits, analytics, backup and reliability.

### Modified Capabilities

- `lesson-player`: sandboxed Character rendering, expressions, dialogue and playback/offline behavior.
- `course-creation`: versioned Cast input for preview and Lesson generation, preserving gating and Course instructions.
- `tutor-chat`: selected main guide persona with bounded context and safe fallback.
- `narration`: per-speaker allowlisted voices with existing cache, alignment and billing rules (extends the pending E14 `product-rules-v1` change).
- `lesson-test`: presentation-only answer reactions; grading and Retry variants remain authoritative.
- `course-sharing`: allowlisted Character snapshots, reuse, privacy and lifecycle (extends the pending E15 `course-sharing-marketplace` change).
- `analytics`: catalog for creation, Casts, rendering, reactions and persona fallback.

## Impact

- Client/design: original SVG rig and built-in art; creator/editor, Cast picker, quote and rejection states; Slide speakers, reactions and Tutor chat identity. Design review covers all expressions and accessibility.
- API: private Character/version storage, screening orchestration, quotes and idempotent jobs, Cast selection and snapshots, ownership checks, limits, backup and analytics.
- Engine: prompt-to-`CharacterSpec`, validators and screening, bounded persona context in generation/chat, speaker annotations and voice resolution; Golden set cases.
- Proto: future implementation may carry validated Character specs, Cast snapshots and speaker keys in matching API/Engine changes. This proposal changes no proto or implementation code.
- Infra: Postgres records in nightly dumps; versioned S3 assets and backup manifests in nightly backup/restore; no new services.
- ADRs: proposes [0010](../../../docs/adr/0010-custom-characters.md); relies on [0002](../../../docs/adr/0002-service-architecture.md), [0003](../../../docs/adr/0003-data-stores.md), [0005](../../../docs/adr/0005-client-and-lesson-player.md), [0006](../../../docs/adr/0006-llm-providers-and-evals.md), [0007](../../../docs/adr/0007-credits-and-payments.md) and [0009](../../../docs/adr/0009-narration-elevenlabs.md).

## Dependencies and scope

- E17 depends on E3, E4, E7 and E14; Tutor chat behavior applies with E8 and publishing behavior with E15. Persona handling follows E16 Course steering's data-only boundary without changing its precedence or scope.
- Out of scope: implementation code, proto edits, issues, other epic changes, free-form image generation, AI portraits, voice cloning, public Character catalogs, standalone Character sharing and retroactive Lesson regeneration.
