# Agent skills for Duolify

How skills reach an agent:

- **OtherSkill routing** (most skills). The owner's global `UserPromptSubmit` hook runs `otherskill find`. It resolves this repo's `otherskill.config.json`, picks matching categories, then matching skills from the project bank (`.otherskill/bank`, committed) and the global bank (`~/.local/share/otherskill/bank`). It adds only the selected descriptions to the prompt. Routing needs a valid OpenRouter key (`otherskill setup`).
- **Native skills** are always visible: OpenSpec skills in `.claude/skills/`, user skills in `~/.claude/skills/`, and plugin skills (Chisle, REA).

Enabled categories: backend, backend/go, backend/rust, frontend/tauri, frontend/react, frontend/ui-ux, analytics, testing, devops, management, product, business-analyst, reverse-engineering, docs.

## Project bank (`.otherskill/bank`)

| Category | Skills | Use for |
|---|---|---|
| backend/go (samber/cc-skills-golang, MIT) | golang-project-layout, code-style, naming, structs-interfaces, error-handling, context, concurrency, database, grpc, lint, testing, observability, performance, safety, security, modernize, continuous-integration | All work in `services/api` |
| frontend/tauri (full-stack-skills/tauri-skills, Apache-2.0) | tauri (index), tauri-concept, tauri-config, tauri-ipc, tauri-security, tauri-framework-security, tauri-mobile, tauri-build, tauri-setup, tauri-app-deep-linking, -stronghold, -store, -updater, -plugin-permissions, -http-client, -notification, -sql | `apps/client/src-tauri`, capabilities, mobile builds, deep links, secure token storage |
| analytics (PostHog/skills, MIT) | integration-javascript_web, omnibus-instrument-product-analytics, omnibus-instrument-llm-analytics, llm-analytics-setup, error-tracking-react, error-tracking-go, feature-flags-react/-go/-rust, logs-go | Event instrumentation required by every spec; LLM cost and latency tracking |
| product (mattpocock/skills, MIT) | to-spec, to-tickets, triage, implement-spec, setup-matt-pocock-skills | Spec to GitHub issues to implementation (see `docs/agents/issue-tracker.md`) |
| reverse-engineering | reverse-engineer-anything (morluto/rea, MIT), reverse-to-spec (Duolify) | Analyze an existing app into a product spec, then OpenSpec, tickets and subagent implementation |

## Global bank (owner machine)

| Category | Skills | Use for |
|---|---|---|
| backend/rust | rust-best-practices, rust-pro, rust-async-patterns, m06-error-handling, m07-concurrency, coding-guidelines, domain-web | `engine/` |
| backend | postgresql-table-design, typescript-expert | Migrations and schema; TS in the client |
| frontend/ui-ux | ui-ux-pro-max, design-system, frontend-design, design-taste-frontend, web-design-guidelines, ui-styling, theme-factory, chatbot-flow-design, stitch | Epic E13 design, tutor chat flows, UI reviews |
| frontend/react | typescript-advanced-types, typescript-expert (expo-* skills there do not apply: the client is Tauri, not Expo) | Client code |
| testing | tdd, test-driven-development, e2e-testing-patterns, javascript-testing-patterns, vitest, systematic-debugging, verification-before-completion, requesting/receiving-code-review | TDD order (e2e, integration, unit) and reviews |
| devops | github-actions, github-actions-templates, docker-compose, docker-expert, use-railway, pnpm, monorepo-management | CI, compose, Railway IaC |
| management | subagent-driven-development, dispatching-parallel-agents, writing-plans, executing-plans, brainstorming, grill-me, grill-with-docs, grilling, domain-modeling, conventional-commits, finishing-a-development-branch, handoff, to-spec/to-tickets (older copies; prefer the project bank versions) | Planning, delegation, glossary and ADR upkeep |
| business-analyst | ask-why-ba, use-case-writer, user-story-ac-writer | Epic E12 requirements, acceptance criteria |

## Native skills

| Where | Skills | Use for |
|---|---|---|
| `.claude/skills` (OpenSpec 1.3) | openspec-propose, openspec-explore, openspec-apply-change, openspec-sync-specs, openspec-archive-change (+ `/opsx` commands) | Every feature starts as an OpenSpec change |
| `~/.claude/skills` | use-superorchestrator-mcp, superorchestrator-cli, superorchestrator-config, use-otherskill, tokenimum | Delegation, skill routing, token facts |
| Plugin `chisle@chisle` | chisle, chisle-audit, chisle-review, chisle-help | Token saving: terse output, YAGNI code, tool-output compression (replaces caveman) |
| Plugin `rea@rea` (install: see README) | REA MCP tools + skill | Binary, APK/IPA, Electron and runtime analysis |

## Hooks on the owner machine

| Hook | Tool | State |
|---|---|---|
| UserPromptSubmit | `otherskill find` (skill routing) | Installed; fails until the OpenRouter key is refreshed (HTTP 401) |
| UserPromptSubmit, SessionStart, PostToolUse, Stop, SessionEnd | `tkm-hook` (tokenimum facts, loop guard, spend) | Working (`tkm doctor`: all Claude events observed) |
| PreToolUse (Bash) | `rtk` rewrite (compact command output) | Working |
| SessionStart, UserPromptSubmit, PostToolUse | Chisle plugin hooks | Installed; active from the next session |
