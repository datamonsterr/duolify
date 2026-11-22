## 1. Tests first
- [x] 1.1 Integration test with a counting object store: cold manifest read at most one GET, warm manifest, position save and sync zero, slide read one; tamper detection, cross-learner isolation and malformed ids.
- [x] 1.2 Unit tests for the bounded, versioned, expiring manifest cache and UUID validation.
## 2. Implementation
- [x] 2.1 Cache decoded manifests; use stored digests; skip the manifest for narration routes.
- [x] 2.2 Typed uuid binds and boundary validation in player and narration.
- [x] 2.3 Client position heartbeat 10 s.
## 3. Verification
- [x] 3.1 Player, narration and sharing integration suites; client player tests.
- [ ] 3.2 CI green on the pull request.
