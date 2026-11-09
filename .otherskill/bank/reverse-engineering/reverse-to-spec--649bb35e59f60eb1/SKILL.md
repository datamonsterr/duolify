---
name: reverse-to-spec
description: Analyze an existing application (repo, live web app, desktop/mobile build or binary) into a black-box product spec, turn the parts worth adopting into an OpenSpec change for Duolify, ticket it, and implement it with subagents. Use when the user says "reverse engineer", "analyze this app", "learn from <app>", "copy how <app> does X", or wants a spec from an existing product.
---

# Reverse to spec

Pipeline: **evidence, then product spec, then fit decision, then OpenSpec change, then tickets, then implementation.** The main session is the controller. Each phase except the fit decision runs in a subagent (superorchestrator `spawn_agent`, or native subagents as the fallback). Every subagent gets a self-contained brief: it has no conversation context.

Work products live in `docs/research/<target-slug>/` and `openspec/changes/<change-id>/`.

## Rules

- Analyze only targets the user may inspect: their own apps, open-source code, public web behavior, or artifacts they own a license for. Do not bypass DRM, licensing, or authentication.
- Extract **behavior and requirements**, never assets. No copied names, characters, art, sounds, copy text or level data. Duolify builds its own design (see `docs/adr/0005-client-and-lesson-player.md`).
- Every claim carries an Evidence ID and is labeled **observed**, **inferred** or **unknown**.
- Specs describe what users see and what the system guarantees. No implementation leakage (library names, class names, internal services) unless it is part of an external contract.

## Phase 1: Evidence (subagent "analyzer")

Pick the route by target type:

| Target | Route |
|---|---|
| Source repository | Read the code and docs directly. REA is not needed. |
| Live web app | Browser tools: walk the user flows, record screens, network calls and states. |
| Shipped artifact (APK/IPA/DMG, Electron/ASAR, native binary, installer) | REA MCP tools. Load the `reverse-engineer-anything` skill first and follow its routing (for example `analyze_javascript_application`, `inventory_artifact`, `open_binary`). Requires the REA plugin (`rea@rea`). |

Brief template:

```
Goal: collect evidence about <target> for the features <list>. Do not write a spec.
Target: <path | URL | artifact>. Route: <source | browser | REA>.
Output: docs/research/<slug>/evidence.md with a table: Evidence ID, feature, observation, source (file:line, URL, screen, tool call), confidence (observed|inferred|unknown).
Also list: user flows (step lists), states and transitions, error and empty states, limits and quotas, timings, data the user can see.
Out of scope: copying assets or text; anything behind auth you were not given.
Finish with: evidence count per feature and the open unknowns.
```

## Phase 2: Product spec (subagent "spec-writer")

Brief template:

```
Goal: turn docs/research/<slug>/evidence.md into a black-box product spec.
Read: the evidence file, CONTEXT.md (use its terms), openspec/specs/ (current Duolify capabilities).
Output: docs/research/<slug>/product-spec.md with sections:
  1. Problem statement  2. Actors and goals  3. Core capabilities
  4. Observable behaviors (Given/When/Then, each citing Evidence IDs)
  5. Edge cases and limits  6. Data the product keeps
  7. Leakage review: list and remove any implementation detail.
Each behavior must be testable from outside the system and survive a rewrite in another stack.
```

## Phase 3: Fit decision (controller, with the user)

For each capability in the product spec, decide **adopt**, **adapt** or **skip** against Duolify's specs, ADRs and epics (`docs/epics/README.md`). Write the table to `docs/research/<slug>/fit.md`. If a decision changes architecture, add an ADR. Ask the user when a choice is a product call (pricing, gamification rules, UX tone).

## Phase 4: OpenSpec change (subagent "spec-author")

Brief template:

```
Goal: create OpenSpec change openspec/changes/<change-id>/ for the adopted/adapted rows in docs/research/<slug>/fit.md.
Use the `openspec` CLI (`openspec --help`) and the /opsx commands' format. Follow openspec/config.yaml rules.
Each requirement needs scenarios, plus requirements for: PostHog analytics events, data backup and retention, and model testing and reliability when AI is involved.
Acceptance: `openspec validate <change-id> --strict` passes.
```

## Phase 5: Tickets

Run `/to-tickets` on the change. Follow `docs/agents/issue-tracker.md`: one `epic` issue, vertical-slice sub-issues with blocked-by edges, area labels, `ready-for-agent` when no human input is needed. Confirm with the user before creating issues.

## Phase 6: Implementation

One superorchestrator run per unblocked ticket, `review: true`. Brief = the ticket body (spec path, TDD test list in e2e, integration, unit order, acceptance `mise run` tasks). Review with `get_run(diff: true)`, accept or reject with feedback. Close the ticket through the PR (`Closes #<n>`).

## Done when

- `docs/research/<slug>/` holds evidence, product spec and fit table.
- The OpenSpec change validates in strict mode.
- Tickets exist (or the user declined), and implemented tickets have merged PRs with green `ci-ok`.
