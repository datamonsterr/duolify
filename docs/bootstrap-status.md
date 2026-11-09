# Bootstrap status (E0)

Checked against the owner's original init request. Updated 2026-11-09, branch `chore/bootstrap`, draft PR #1.

Legend: **Done**, **Partial** (works, with a gap listed), **Blocked** (needs the owner), **Not started**.

## Init tasks

| # | Requirement | Status | Evidence / gap |
|---|---|---|---|
| 1 | Initialize repo requirements and infra | Done | Empty root commit on `main`; all work on `chore/bootstrap`; `.gitignore`, `.editorconfig`, `.env.example`, lefthook hooks |
| 2 | README setup guide; automate all but human steps | Done | `README.md` quick start (`mise install`, `mise run setup`, `infra:up`, `client:dev`) and a "Human-required steps" checklist |
| 3 | Find and configure agentic skills (OtherSkill) | Partial | 51 project skills in `.otherskill/bank` (Go, Tauri, PostHog, tickets, reverse engineering), global bank, list in `docs/agents/skills.md`. Gap: routing hook gets HTTP 401 because a stale `OPENROUTER_API_KEY` in `~/.config/environment.d/60-opencode-providers.conf` overrides the new key (works when that variable is unset) |
| 4 | OpenSpec, OtherSkill config, AGENTS.md + CLAUDE.md, mise.toml | Done | `openspec/` (17 capability specs, 8 changes, strict validation passes), `otherskill.config.json`, `AGENTS.md`, `CLAUDE.md` → `@AGENTS.md`, `mise.toml` + `.mise/tasks/*.toml` |
| 5 | Scaffold Go service, Rust engine, Tauri app, infra | Done | `services/api`, `engine` (5 crates), `apps/client` (Tauri 2 + React), `proto`, `infra`, `.railway/railway.ts` |
| 6 | Dummy tests per part, full CI/CD, Docker images, compose, Railway deploy config | Partial | Tests: api (unit, e2e, Postgres integration), engine (9 tests + offline eval), client (Vitest, Playwright, Rust). Compose stack healthy locally. Draft CI green. Gap: the ready pipeline (integration, Playwright, Docker images, Tauri build, live evals, Railway plan) has not run yet; it runs when the PR is marked ready |
| 7 | Install CLI tools via mise | Done | `mise install` installs all 16 pinned tools; `cargo:tauri-cli` dropped in favour of `@tauri-apps/cli` from pnpm |

## Principles

| Requirement | Status | Evidence / gap |
|---|---|---|
| Investigate, break into epics, subagent-driven development via superorchestrator | Done | `docs/epics/README.md` (E0–E16); all scaffolds built by superorchestrator runs with review gates |
| TDD order e2e → integration → unit | Done (rule) | `AGENTS.md`, ticket template in `docs/agents/issue-tracker.md`, each OpenSpec `tasks.md` |
| CI on Namespace runners, draft vs ready pipelines, parallel, cached | Done | `pr-draft.yml`, `pr-ready.yml`, `_checks.yml`, `deploy-dev.yml`; only `namespace-profile-datamonster01` / `namespace-profile-linux-xl`; `ci-ok` aggregate |
| Golden set on Hugging Face with automatic evals in CI | Partial | `duolify-eval` runner, offline fixtures, thresholds, `EVAL_SAMPLE_LIMIT`; draft CI runs offline evals. Gap: dataset `datamonsterr/duolify-golden` not created; live evals need `HF_TOKEN` + API secrets |
| PostHog analytics; every spec covers backup and model reliability | Done | Client and API wrappers; every OpenSpec spec has analytics, backup and reliability requirements; `docs/data-inventory.md` |
| Architecture for multi-user speed, easy to extend | Done (design) | ADR 0002 (Go API + stateless Rust engine over gRPC), ADR 0003 (Postgres, Redis, S3, River) |
| Check current docs before coding | Done | Each brief required current docs; Context7 MCP failed to connect in this session |

## Rules

| Requirement | Status | Evidence / gap |
|---|---|---|
| Commit on bootstrap branch; draft PR when scaffold and CI are complete; no merge | Done | PR #1 is draft, not merged |
| No Railway production deploy; dev project only | Done | Railway project `duolify` + environment `dev` created; `railway config plan` shows 7 additions; nothing applied |
| No secrets in repo | Done | Keys only in git-ignored `.env` and local config files |
| Ambiguous decisions as ADRs | Done | ADRs 0001–0009 in `docs/adr/` |

## Added after the first report

| Requirement | Status | Evidence / gap |
|---|---|---|
| Lesson test pass-all with paraphrased retry variants; daily free credits; auto streak freeze, no credit rewards | Done (spec) | OpenSpec change `product-rules-v1` |
| ElevenLabs narration | Done (spec) | ADR 0009, capability `narration`; key in `.env` |
| E12 monetization (approved), prices in config | Done (spec) | `docs/business/`, changes `monetization-model` and `pricing-config` |
| E13 app design | Done | `docs/design/` incl. offline prototype, change `app-design` |
| E15 sharing and marketplace, E16 course steering | Done (spec) | Changes `course-sharing-marketplace`, `course-steering` |
| to-tickets and GitHub issues skills, well instructed | Done | Skills in bank; `docs/agents/issue-tracker.md`; labels created on GitHub. No issues created yet |
| Chisle replaces caveman | Done | Plugin `chisle@chisle` active; caveman disabled |
| Hooks: tokenimum, rtk, OtherSkill | Partial | tokenimum and rtk work; OtherSkill blocked by the stale env key (row 3 above) |
| REA reverse engineering ported to OtherSkill + subagent flow | Partial | `reverse-engineer-anything` and `reverse-to-spec` skills in bank. Gap: plugin `rea@rea` not installed (`EALLOWREMOTE` from npm 12) |
| OpenRouter MCP with management key; keys for tools, dev, CI | Done | MCP connected with the management key; keys `duolify-tools`, `duolify-dev`, `duolify-ci` created |
| Claude API key | Done | Verified; in `.env` |

## Owner actions still open

1. Replace `OPENROUTER_API_KEY` in `~/.config/environment.d/60-opencode-providers.conf` with the `duolify-tools` key, then log out and in.
2. `npm_config_allow_remote=all claude plugin install rea@rea --scope project`
3. GitHub secrets: `OPENROUTER_API_KEY` (the `duolify-ci` key), `ANTHROPIC_API_KEY`, `ELEVENLABS_API_KEY`, `HF_TOKEN`, `RAILWAY_TOKEN`, `POSTHOG_API_KEY`.
4. Create the Hugging Face dataset `datamonsterr/duolify-golden` and upload `evals/fixtures/golden.sample.jsonl`.
5. Branch protection on `main` requiring `ci-ok`.
6. Mark PR #1 ready to run the full pipeline; then merge.
7. Rotate the keys pasted into chat once setup is stable.
