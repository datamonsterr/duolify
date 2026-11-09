# ADR 0010: Custom characters with a parametric SVG rig and bounded personas

- Status: Proposed
- Date: 2026-11-09

## Context

Learners want to describe original mascot-style Characters and see them narrate Slides, speak dialogue, react to Lesson test answers and act as the Tutor chat persona. Appearance must stay consistent across Lessons and animate in Motion HTML. Learner-authored persona text is a prompt-injection surface, as in Course steering.

## Decision

- Appearance uses an original parametric SVG Character rig: body shape, palette, eyes, mouth, accessories; expressions `idle`, `happy`, `sad`, `thinking`, `celebrate`. An LLM maps the prompt to a versioned `CharacterSpec` validated against a closed JSON Schema and safety checks. Trusted templates render it and Motion animates it in Motion HTML Slides (ADR 0005). V1 has no free-form image generation; an optional AI portrait is a later extension outside E17.
- Personality and info are structured fields (name, bio, backstory, traits, speaking style) plus a short persona prompt. The Engine receives them as a bounded, escaped, data-only section beneath system policy, following `course-steering`. They cannot change safety, output format, grading, tools, pricing or reveal prompts. Input and output screening plus schema validation are required; delimiters alone are insufficient protection.
- Voice keys map to an ElevenLabs voice allowlist in config (ADR 0009). No cloning, uploads or arbitrary provider voice IDs.
- A few original built-in Characters form the Default cast. Names, designs and art must not copy or reference Duolingo's or any other game's characters. Each Course has one main guide and optional supporting Characters; the Default cast applies when none is chosen. Jobs pin versions so edits affect future generation only.
- Screen prompts, generated specs and manual edits: reject real-person likeness, third-party IP/trademarked characters, sexual content and content inappropriate for the supported age range. Enforce per-Learner count, length and rate limits. Fail closed on screening failure; use the Default cast for unavailable or invalid runtime Characters.
- Creation and prompt-based regeneration reserve and settle Credits using the proposed Pricing config key `character_create` (ADR 0007). Manual field edits are free and still validated and screened. This key is not owner-approved; paid creation remains disabled until approval in a new Pricing config version.
- Characters stay private except for an explicit publish confirmation: Characters attached to a Published course are immutable snapshots with it under E15. Never publish raw creation prompts, ownership identifiers or screening records. Postgres stores versions, Casts and verdicts; nightly dumps and versioned S3 asset backups must restore them together (ADR 0003).
- Golden set cases on Hugging Face cover prompt-to-spec validity, persona adherence, injection resistance and screening. Bounded calls use ADR 0006 provider fallback, timeouts and cost caps. Exhausted creation attempts release Credits; runtime use falls back to the Default cast. PostHog receives identifiers and reason codes, never persona or prompt text.

## Consequences

An authored rig constrains appearance but keeps cost predictable, identity consistent and animation reusable. Versioned Casts add snapshot and deletion bookkeeping. Reactions are presentation only and never change grading. See the [E17 design](../../openspec/changes/custom-characters/design.md) for boundaries and proposed limits.

## Open questions

1. Owner approval and price of `character_create`, including Daily free credits eligibility and provider cost ceiling; no launch price is assumed.
2. Final original Default cast names/art, rig options, accessibility review and supported learner age range.
3. Initial count/rate/text limits and multilingual persona/voice coverage; proposed defaults live in the design.
4. Confirm ElevenLabs rights and availability for allowlisted voices and redistribution with E15 snapshots before launch.
