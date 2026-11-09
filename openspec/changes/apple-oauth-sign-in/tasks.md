# Tasks

## Tests (TDD order)

- [x] Add Playwright Apple completion/cancellation coverage at desktop and mobile widths, with returning identity claims omitted, focus recovery, keyboard/touch, offline and retry behavior.
- [x] Add Postgres integration coverage for Apple issuer/audience/nonce checks, signing-key rotation, replay, first-login private-relay email/name persistence, returning identity without email/name, linking collisions, backup restore and deletion.
- [x] Add API and client unit tests for Apple claim boundaries, fixture behavior, deep-link bridge, and analytics privacy/opt-out.

## Implementation

- [x] Add a strict OpenSpec rule set for Apple identity, callback, client bridge, analytics privacy, backup/deletion, and no-AI boundary.
- [x] Reuse the existing provider interface, OAuth transaction and session issuer; add Apple fixture/provider verification behind an injectable key/client-secret boundary.
- [x] Persist optional first-login name and private-relay email while returning sign-ins resolve solely by `(apple, subject)`.
- [x] Add Apple entry point to the responsive Welcome sign-in surface using existing design tokens and recovery states.
- [x] Extend the existing OAuth analytics catalog before the Apple emitter; use existing PostHog wrappers and assert UUID/common properties, opt-out, and no private data.
- [x] Document Apple provider bindings in the data inventory and add Apple subject to backup fixture assertions. Do not add credentials; list E1-10 as the live provisioning gate.
- [x] Run `mise run ci:openspec`, then the focused API and client mise gates. AI behavior is unchanged.
