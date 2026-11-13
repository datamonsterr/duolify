# E2-01: Preview a confirmed course brief

## Why
The existing brief confirmation and paid execution boundaries need a connected preview flow without enrollment.

## What Changes
- Bind immutable preview quotes to an owned, confirmed brief revision; reject mismatched or expired consent before reservations or engine work.
- Send the exact brief and current optional profile to PreviewCourse, validate sample outline and slides, and reuse bounded paid execution and settlement.
- Add an accessible Preview step with sandboxed samples, sample outline review, Back/discard/regenerate and fresh consent. Outline editing and creation remain E2-07/E2-02.
- Catalogue private analytics and offline golden fixtures. Preview output is ephemeral; existing brief and financial retention/privacy policies apply.

## Impact
API, engine, proto and client change together; no new learner tables or provider credentials.
