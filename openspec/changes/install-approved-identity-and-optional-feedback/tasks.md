## 1. Acceptance first
- [x] 1.1 Author failing Playwright e2e test in `apps/client/e2e/design-e13-06.spec.ts`.
- [x] 1.2 Author failing integration test in `apps/client/src/test/install_approved_identity_and_optional_feedback.integration.test.tsx`.
- [x] 1.3 Author failing unit test in `apps/client/src/features/design/install_approved_identity_and_optional_feedback.test.tsx`.

## 2. Implementation
- [x] 2.1 Implement `BrandMark`, `BrandWordmark`, and `Brand` components with 16px legibility.
- [x] 2.2 Commit canonical SVG icon, generate Tauri app icons, and configure web manifest and favicon.
- [x] 2.3 Implement optional feedback controller and platform bridge double for sound (silent no-op) and haptics.
- [x] 2.4 Update Settings (S23) with responsive desktop and mobile feedback controls, keyboard and focus states.
- [x] 2.5 Emit `ui_control_activated` for settings toggles and update data inventory.

## 3. Verification
- [x] 3.1 Pass `client:lint-web`, `client:test-web`, `client:build`, and `client:e2e`.
- [x] 3.2 Validate OpenSpec change with `mise run ci:openspec`.
