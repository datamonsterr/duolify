## Why
Owners need a reviewable, immutable snapshot and a safety verdict before any Course can become public (#118, E15-01). This tracer builds the snapshot, the real `ScreenCourse` classifier contract and the owner-only preview. It exposes no public link, marketplace listing or Share link.

## What Changes
- New `services/api/internal/sharing` package: allowlist snapshot builder, deterministic scanner (rejects out-of-allowlist fields; flags emails, phone numbers, API keys), screening service and owner-only HTTP.
- Postgres `publication_drafts` (immutable numbered versions, content hash, snapshot, verdict) and `publication_screenings` (bounded verdict and cost rows, retained one year).
- Additive proto: `ScreenCourse` RPC, `ScreenCourseRequest`, `ScreenCourseResponse`. Go consumer and Rust producer ship in this change.
- Platform-funded sample: at most 12 Lessons and 60000 input tokens, 120s deadline, one evaluated fallback, USD cap from config. No Learner Credits are reserved or charged; undelivered usage is never billed and no reservation exists to leak.
- Verdict `pass | needs_review | block`; any failure, timeout, schema violation or cap breach yields `pending_screening` (fail closed). Private learning is never touched.
- Client: owner "Preview publication draft" panel (desktop dialog, mobile sheet) with verdict, findings, outline and recovery actions.
- Config (ADR 0012, E15-12 open): publishing requires an 18+ account (`SHARING_MIN_PUBLISHER_AGE`), published snapshots exclude narration audio, in-app notifications only.

## Out of scope
Share link, visibility changes, marketplace, enrolment, republish, update flow, moderation queue, invites (E15-02 to E15-12). Live provider promotion (E10-06).
