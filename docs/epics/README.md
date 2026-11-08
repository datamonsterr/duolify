# Epics

Each epic becomes one or more OpenSpec changes (`openspec/changes/<id>`). Order reflects dependencies. Every change includes: e2e, integration and unit tests (written in that order), PostHog events, data backup notes, and model reliability (golden-set cases, fallback) when AI is involved.

| # | Epic | Areas | Depends on |
|---|---|---|---|
| E0 | Bootstrap: repo, tooling, scaffolds, CI/CD, infra | all | — |
| E1 | Accounts and learner profile (auth per ADR 0004, profile, settings) | api, client | E0 |
| E2 | Course creation agent: preview, outline-only generation, enrollment | engine, api, client | E1 |
| E3 | Lesson player: Motion HTML slides, playback controls, preload, offline cache | client, api | E2 |
| E4 | Gated lesson generation and lesson tests | engine, api, client | E2, E3 |
| E5 | Gamification: XP, streaks, streak freezes, daily goals, leagues | api, client | E4 |
| E6 | Social: friends, friend streaks | api, client | E5 |
| E7 | Credits: ledger, reservation and settlement, Stripe top-ups, reward credits | api, client | E1 |
| E8 | Tutor chat with course, lesson, profile and history context | engine, api, client | E4, E7 |
| E9 | Quick AI notes and bookmarks | engine, api, client | E3, E7 |
| E10 | Evals and model reliability: golden set, judges, fallbacks, dashboards | engine, evals, ci | E2 |
| E11 | Ops: backups and restore drill, observability, Railway environments | infra, api | E0 |

## E0 bootstrap work packages

| WP | Scope | Owner directory |
|---|---|---|
| WP-API | Go service scaffold, health, engine gRPC client, migrations, tests, Dockerfile | `services/api`, `.mise/tasks/api.toml` |
| WP-ENGINE | Rust workspace, tonic server, provider trait (Claude, OpenRouter, mock), eval binary, tests, Dockerfile | `engine`, `evals`, `.mise/tasks/engine.toml` |
| WP-CLIENT | Tauri 2 + React app, lesson player stub, tests | `apps/client`, `.mise/tasks/client.toml` |
| WP-PROTO | buf config, codegen, breaking-change check | `proto`, `.mise/tasks/proto.toml` |
| WP-SPECS | OpenSpec init and first product specs | `openspec` |
| WP-INFRA | docker compose, Railway config as code, backups stub | `infra`, `docker-compose.yml`, `.mise/tasks/infra.toml` |
| WP-CI | Draft and ready pipelines, caching, actionlint, `ci-ok` | `.github` |
| WP-DOCS | README setup guide, skills config | `README.md`, `otherskill.config.json` |
