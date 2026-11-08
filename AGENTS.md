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

## Workflow

- Branch from `main`, open a draft PR early. Draft PRs run the fast CI pipeline; ready PRs run the full pipeline.
- Conventional Commits (`feat:`, `fix:`, `chore:`, `docs:`, `test:`, `refactor:`, `ci:`).
- Tests run in CI. Run only the focused task for the area you change locally.
- A task is done when its PR is merged into `main`. Remove its worktree afterwards (`git worktree remove <path>`, never `--force`).
