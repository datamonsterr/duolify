## 1. Specification and acceptance contract
- [x] 1.1 Read domain, ADR, all current specs and scaffold; record precedence and scope.
- [x] 1.2 Write prototype e2e acceptance checks before implementation, followed by token integration checks and contrast unit fixtures.

## 2. Design deliverables
- [x] 2.1 Define brand, accessible light/dark tokens, Tailwind v4 snippet and component states.
- [x] 2.2 Document navigation, routes, deep links, every screen/state and analytics.
- [x] 2.3 Add Mermaid flows for creation, lesson/retry, purchase and freeze rollover.
- [x] 2.4 Build the self-contained offline prototype without app wiring.

## 3. Validation and handoff
- [x] 3.1 Run offline browser checks across sizes, modes, keyboard and reduced motion.
- [x] 3.2 Verify token parity and every permitted color pair; document measured ratios.
- [x] 3.3 Pass `openspec validate app-design --strict` and `openspec validate --all --strict` through mise.
- [x] 3.4 Review changed-file scope, commit `docs(design): ...`, confirm clean worktree; do not push.

## 4. Revision 2026-11-09: green theme, guided chat, desktop-first parity
- [x] 4.1 Re-theme tokens, CSS snippet, prototype token values and contrast evidence; add new roles and pairs to `check.mjs`.
- [x] 4.2 Update brand, design-system and screens docs for the theme, guided chat onboarding and creation, and the editable outline.
- [x] 4.3 Design every screen and state desktop first plus mobile in the canvas; add its source to `docs/design/canvas/`.
- [ ] 4.4 Run `mise exec node -- node docs/design/prototype/check.mjs` (token and browser checks) and `openspec validate app-design --strict` through mise.
- [ ] 4.5 Refresh the offline prototype's layout to the new theme and chat flows (its token values are already updated).
- [ ] 4.6 E12 owner decision: free clarifying chat and the price of outline chat changes.
