# Duolify — agent guide

Duolify turns any topic a person wants to learn long-term into a Duolingo-style app: courses, short lessons, streaks, friend streaks and credits. Lessons are **Motion HTML** slides played in the app, never rendered to video. Project terms live in `CONTEXT.md`. Decisions live in `docs/adr/`. Product specs live in `openspec/`.

## Layout

| Path | What | Stack |
|---|---|---|
| `apps/client` | Desktop + mobile app | Tauri 2, React, TypeScript, Vite |
| `services/api` | Public API router, auth, progress, streaks, credits, workers | Go |
| `engine` | AI agent engine: course and lesson generation, chat, notes, evals | Rust (Cargo workspace, gRPC via tonic) |
| `proto` | gRPC contract between `services/api` and `engine` | Protobuf + buf |
| `evals` | Golden-set config (dataset lives on Hugging Face) | — |
| `infra` | Docker, Railway config, backups | Docker compose, Railway |
| `openspec` | Specs and change proposals | OpenSpec |
| `.mise/tasks` | One task file per area | mise |

## Rules

- Use `mise` for every tool and task. Do not add ad-hoc scripts when a mise task works. Run `mise tasks` to list tasks.
- Every area exposes `<area>:lint`, `<area>:test`, `<area>:build`. CI calls only mise tasks.
- Check current docs (Context7 or official site) before coding against any library, framework, SDK or CLI. Do not write APIs from memory.
- TDD order for features: e2e test, then integration test, then unit test, then code until all pass.
- New feature work starts with an OpenSpec change (`openspec/changes/<id>`). Every spec includes: data backup, analytics events (PostHog), and model testing and reliability (evals, fallbacks) when AI is involved.
- AI features need golden-set cases in the Hugging Face dataset (see `evals/README.md`) and must pass `mise run engine:eval`.
- No secrets in the repo. Add new variables to `.env.example` with a comment.
- Keep services stateless. State lives in Postgres, Redis and object storage.
- The `proto` contract changes only with a matching change in both `services/api` and `engine`.

## Agent skills

Skills are routed by OtherSkill (`otherskill.config.json`, project bank `.otherskill/bank`, plus the owner's global bank). The routing hook adds the matching skill descriptions to each prompt; load a skill before acting on it. See `docs/agents/skills.md` for the full list.

### Issue tracker

GitHub Issues on `datamonsterr/duolify`, epics as `epic` issues with sub-issue tickets. Flow: OpenSpec change, then `/to-tickets`, then one PR per ticket. See `docs/agents/issue-tracker.md`.

### Triage labels

Default five roles (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`) plus `epic` and `area:*`. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `CONTEXT.md` (glossary) and `docs/adr/`. See `docs/agents/domain.md`.

### Reverse engineering

To learn from an existing app, use the `reverse-to-spec` skill: evidence, product spec, fit decision, OpenSpec change, tickets, implementation by subagents. Binary and packaged targets need the REA plugin (`rea@rea`).

## Workflow

- Branch from `main`, open a draft PR early. Draft PRs and main pushes run the fast CI tier. Ready PRs run the full tier with path filters. The nightly workflow runs the full tier on main.
- Conventional Commits (`feat:`, `fix:`, `chore:`, `docs:`, `test:`, `refactor:`, `ci:`).
- Tests run in CI. Run only the focused task for the area you change locally.
- A task is done when its PR is merged into `main`. Remove its worktree afterwards (`git worktree remove <path>`, never `--force`).

## CI/CD Resource & Push Policy
This account shares 40 concurrent GitHub Actions slots across all repos and a 3,000 min/month private-repo budget. Treat CI as a final verification gate, NOT a scratchpad.

1. **Verify locally before pushing:** Run the repo's local check task (`mise run check`, `cargo clippy`, `pnpm lint && pnpm typecheck && pnpm test`) and confirm it passes before `git push`.
2. **Batch commits:** Do not push after every small edit. Commit locally and push only when a logical milestone passes local checks.
3. **Draft PRs first:** Always open new PRs as `--draft` (`gh pr create --draft`). Only mark ready (`gh pr ready`) once local checks pass and implementation is complete.
4. **No unnecessary heavy triggers:** Do not add `e2e` or `bench` labels unless explicitly requested.
5. **Workflow hygiene:** Every new GitHub Actions workflow MUST include `concurrency` (`cancel-in-progress: true`), `timeout-minutes` on every job, and path filters. Never add `macos-*` or `windows-*` jobs to private PR triggers.
