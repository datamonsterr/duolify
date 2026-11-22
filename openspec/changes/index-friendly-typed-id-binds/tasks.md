## 1. Tests first
- [x] 1.1 Malformed ids on courses, outline, proposal, creation, lesson and generation routes return a client error, not a database error.
- [x] 1.2 Poll backoff helper and generation stream statement count.
- [x] 1.3 EXPLAIN test on the hot-path statements.
## 2. Implementation
- [x] 2.1 Courses and lessontest typed binds, boundary validation, shared backoff (PR 1).
- [x] 2.2 Remaining packages and sqlc sources, CI guard, anonymizer and narration purge gaps (PR 2).
## 3. Verification
- [x] 3.1 Courses and lessontest integration suites.
- [ ] 3.2 CI green on both pull requests.
