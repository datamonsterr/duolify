## 1. Harness
- [x] 1.1 `infra/e2e/compose.yml` overlay and `infra/e2e/stack.sh` (up, down, logs, code, url)
- [x] 1.2 `playwright.stack.config.ts`, `e2e-stack/stack.ts` flow fixture, `flow-reporter.ts`
- [x] 1.3 mise tasks `client:e2e-stack-up`, `client:e2e-stack`, `client:e2e-stack-down`

## 2. First flows
- [x] 2.1 F1 email-code sign-up and sign-in
- [x] 2.2 F2 create a course: topic, outline, confirm
- [x] 2.3 F3 play the first lesson to the end
- [x] 2.4 F4 lesson test pass path

## 3. CI and docs
- [x] 3.1 Full-tier `playwright-stack` job uploading screenshots and `flows.json`
- [x] 3.2 docs/ci.md and apps/client/README.md

## 4. Core loop and more flows
- [x] 4.1 Pricing approval stays with the owner: no harness approval; preview failures report `pricing not approved on target`
- [x] 4.2 F2-F8 written: F2-F4 wait on owner-approved pricing (PR #304); F3-F8, `fixme` on the engine mock-mode lesson defect
- [x] 4.3 F5 retry variant, F6 tutor, F7 notes and bookmarks, F8 progress, F9 credits, F10 settings, F11 account data, F12 friends
- [x] 4.4 Mocked e2e: offline Learn home with no active course shows the error state
- [x] 4.5 Add `playwright-stack` to `ci-ok` after two consecutive green CI runs

## 5. Round 3
- [x] 5.1 Worker keeps the application object-store key (generated lessons were never published); deploy-plan test
- [x] 5.2 F2-F8 un-fixme'd: preview and lesson generation failures are hard failures
- [x] 5.3 F13 free example course for a new learner
- [x] 5.4 Shell right rail reads the real wallet and streak; F8 and F9 assert it
- [x] 5.5 F1d daily check-in (PR #300) and F14 welcome and tour (PR #311); course flows funded by the real check-in
