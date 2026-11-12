# Epic controller playbook

An epic controller is a Claude session that owns one epic (`E<n>`) end to end. It plans, delegates each ticket to a coding agent through superorchestrator, reviews, merges into its epic branch and verifies. It does not write feature code itself.

## Sources of truth

- Tickets: `docs/epics/tickets.json` (scope, acceptance, TDD test locations, `blocked_by`), graph and waves in `docs/epics/tickets.md`, issue numbers in `docs/epics/issues.json`.
- Specs: `openspec/` (the ticket's `spec` paths). Decisions: `docs/adr/` (0011 holds the owner decisions). Terms: `CONTEXT.md`.
- Design: `docs/design/` (`design-system.md`, `tokens.json`, `theme.css`, `screens.md`) and the canvas boards in `docs/design/canvas/*.dc.html`. Desktop first, same functions on mobile. Tokens win over a conflicting canvas value.
- Expected API: the OpenSpec requirements and scenarios, `proto/` for the API ↔ engine contract, and the existing handlers in `services/api/internal/httpapi`.

## Branches

- Integration branch: `impl/mvp` (draft PR #156). Only the parent orchestrator merges into it.
- Your branch: `epic/e<n>`, created from `origin/impl/mvp`, pushed to origin after every merged ticket.
- Before starting each ticket: `git fetch origin && git merge origin/impl/mvp` so you build on other epics' work. Resolve conflicts keeping both sides; rerun the affected checks.

## Ticket status (GitHub labels)

- `status:in-progress`: someone is implementing it. Never start a ticket with this label unless you own it.
- `status:implemented`: merged into an epic branch or `impl/mvp` with all checks green.
- A blocker counts as satisfied when its issue is closed (owner decision) or labelled `status:implemented` **and** its code is in your branch after merging `origin/impl/mvp`. HITL tickets that are open block their dependents; skip those dependents.

## Loop

1. Read the epic's tickets and compute the ready set (AFK, not implemented, not in progress, all blockers satisfied). Independent ready tickets may run in parallel (at most 2 at a time) when they touch different files.
2. Label the issue `status:in-progress`.
3. Spawn one coding agent per ticket with `spawn_agent` (`cwd` = your worktree, `review: true`, `wait: false`). Brief: the ticket id (the agent reads `docs/epics/tickets.json`), the rules below, what already exists on the branch, and parallel work to avoid. Accounts and models: `codex-1`/`codex-2` with `codex/gpt-6.1-sol/high` (strongest; `codex-acc*` accounts cannot run gpt-6.1), `codex-acc1..3` with `codex/gpt-6-luna/high` for smaller tickets or when codex-1/2 are out of quota, `agy-3`/`agy-4` with `agy/gemini-3.8-flash-high/high` as a last resort. Check `list_harnesses` for quota before pinning. On a quota failure, resume later with `send_message` or respawn on another account; the unfinished work stays in the run worktree.
4. Answer `ask_parent` questions from the specs/ADRs; if it is a real product decision not covered, choose the conservative option that keeps prices in config and record it in the ticket's OpenSpec change and as one table row (`| <ticket> | <default> |`) in the "Defaults chosen during implementation" table of ADR 0011 — no bullets or paragraphs. Never add rows to the owner-decisions table.
   Port isolation: parallel epics share the host. Have agents set the client e2e/API ports through the env vars in `apps/client/e2e/servers.ts` to the epic's range (`4<epic number, two digits>0`–`…9`, e.g. E7 → 4070–4079, E12 → 4120–4129).
5. At the review gate: `get_run(diff: true)`; check acceptance criteria, TDD order (tests exist for e2e, integration, unit), design-board fidelity for UI, no hard-coded prices, no secrets, no learner text in analytics, new tables covered by backup fixtures and `docs/data-inventory.md`. `reject_run` with precise feedback until it is right, then `accept_run(merge: true)`. On merge conflicts, reject with "merge your base branch and resolve".
   Gemini or other fallback-model output gets a line-by-line review of every changed file, especially authorization, idempotency, money paths and the product rules. Reviews of earlier epics found that this output violated core rules while all the tests passed.
6. After merging: run the touched areas' mise tasks yourself (`api:lint|test|build|test-e2e|test-integration`, `client:lint-web|test-web|build|e2e`, `engine:*`, `ci:openspec`), fix-forward through another agent if anything fails, push `epic/e<n>`, label the issue `status:implemented` (remove `status:in-progress`), and comment the commit SHA on the issue. Do not close issues; PR merge closes them.
7. Repeat until no ready tickets remain. Before your final report, `git fetch origin && git merge origin/impl/mvp` once more, resolve conflicts, rerun the touched checks and push, so the parent can merge `epic/e<n>` into `impl/mvp` without conflicts. Then finish with a report: tickets implemented (ids, issue numbers, SHAs), tickets still blocked and by what, decisions taken, follow-ups.

## Rules for coding agents (include in every brief)

- TDD: e2e → integration → unit, each failing first, then code until green. Never weaken or skip tests.
- Only mise tasks. Native Tauri (Rust/WebKit) checks cannot run on this host; run web variants and list skipped native checks (CI runs them).
- Go migrations: goose versions `YYYYMMDDHH` + two-digit epic number + two-digit sequence (e.g. E7 → `2026111014` `07` `01`), so parallel epics never collide; sqlc via `mise run api:generate`. Add new tables to backup fixtures and `docs/data-inventory.md`.
- `proto/` changes only with matching api and engine changes in the same ticket.
- AI features: mock provider in tests, golden-set cases in `evals/fixtures/`, `mise run engine:eval` green, spec fallback implemented. Live Hugging Face evals are not available yet (ADR 0011).
- Prices and credits only from the Pricing config. External services (Resend, Stripe, ElevenLabs, OAuth, HF) behind interfaces with fakes; no secrets; new env vars in `.env.example`.
- PostHog through existing wrappers; never send learner text. The API analytics client drops events not in its allowlist (`services/api/internal/analytics/analytics.go`): add every new event there and to the analytics catalogue in the same ticket.
- Every new table linked to a learner needs an export/deletion policy in `services/api/internal/accounts/privacy.go` (the E1-07 coverage test fails otherwise).
- No PRs, no report files in the repo; report in the final message.

## Host resources

Parallel epics share one disk and a small `/tmp` tmpfs. Rust agents use `CARGO_TARGET_DIR=/home/dat/.cache/<epic>-target` and `RUSTC_WRAPPER=` (empty); nothing large goes in `/tmp`. Delete your target dirs and extra Go caches when the epic finishes. On "no space left on device", stop and tell the parent rather than retrying. If you see "no field `deadline_unix_ms`" (stale generated proto), run `cargo clean -p engine-proto`.

## Never

Merge into `main` or `impl/mvp`, force-push, open PRs, deploy, create paid resources, or commit secrets.
