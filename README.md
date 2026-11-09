# Duolify

Turn any topic you want to learn long-term into a Duolingo-style learning app.
Take short lessons, build streaks with friends and use credits for AI assistance.
Lessons are Motion HTML slides played live in the app, never rendered to video.

```text
Tauri 2 + React client ── HTTP / SSE ── Go API (public)
                                        ├── gRPC ── Rust engine (private)
                                        │              └── Claude → OpenRouter fallback
                                        ├── Postgres 18: product data + River jobs
                                        ├── Redis 8: reconstructible caches
                                        └── S3: lesson assets + separate backup bucket
Go API worker ── River jobs / encrypted nightly backups ── Postgres + S3
```

## Repository

| Path | Purpose |
| --- | --- |
| `apps/client` | Tauri desktop/mobile shell, React UI, Motion HTML player |
| `services/api` | Go API, migrations, auth/progress/credits and workers |
| `engine` | Rust gRPC AI service, provider routing and eval binary |
| `proto` | Shared protobuf contract and Buf code generation |
| `evals` | Eval fixtures and thresholds; full golden set on Hugging Face |
| `infra`, `.railway` | Local containers, backup tooling and Railway IaC |
| `openspec` | Product specs and change proposals |
| `docs/adr`, `docs/epics` | Architecture decisions and implementation roadmap |
| `.mise/tasks` | Tasks grouped by area |

Read [CONTEXT.md](CONTEXT.md) for product terms and [AGENTS.md](AGENTS.md) for
contribution rules. This is a bootstrap scaffold; real auth, generation, billing
and scheduled backups arrive in later OpenSpec changes.

## Prerequisites

Install **git**, **Docker with Compose**, and **[mise](https://mise.jdx.dev/)**.
Mise installs the pinned language runtimes, package managers and project CLIs.
The Docker daemon must be running and accessible to your user.

Tauri also needs OS development packages. The following lists follow the current
[Tauri 2 prerequisites](https://tauri.app/start/prerequisites/):

Debian / Ubuntu:

```sh
sudo apt update
sudo apt install libwebkit2gtk-4.1-dev build-essential curl wget file \
  libxdo-dev libssl-dev libayatana-appindicator3-dev librsvg2-dev
```

Arch Linux:

```sh
sudo pacman -Syu
sudo pacman -S --needed webkit2gtk-4.1 base-devel curl wget file openssl \
  appmenu-gtk-module libappindicator-gtk3 librsvg xdotool
```

macOS desktop: install Xcode Command Line Tools with `xcode-select --install`.
For iOS, install and launch full Xcode; follow the Tauri guide for CocoaPods and
Rust targets. Android needs Android Studio's SDK, NDK, platform/build tools and
Rust targets. Mobile setup is optional until working on mobile.

## Quick start

```sh
git clone https://github.com/datamonsterr/duolify.git
cd duolify
mise trust && mise install
cp .env.example .env
mise run setup
mise run client:install
mise run infra:up
mise run client:dev
```

The API listens on `http://localhost:8080`; `/healthz` checks liveness and
`/readyz` checks Postgres, Redis and the engine. The private engine uses gRPC on
port 50051. Local S3 is on port 9000 and its console on 9001. Keep the default
`ENGINE_PROVIDER_MODE=mock` to run without provider credentials or API charges.
Local S3 uses [RustFS](https://docs.rustfs.com/en/installation/container/docker)
and a pinned AWS CLI bucket initializer: MinIO's Docker Hub images were removed
and its Quay registry denies anonymous pulls. The existing `S3_*` settings work
with RustFS; Railway continues to use managed buckets.
Bootstrap client screens currently use mock data; see [client notes](apps/client/README.md).

Compose loads `.env` with working defaults and rewrites localhost dependency
URLs to container DNS. Postgres uses the local `duolify` database/user/password;
these development credentials must never be reused in cloud environments.
Published ports bind only to loopback. Bucket initialization enables asset
versioning; `migrate` applies embedded Goose and River migrations before API or
worker startup. The current binary's command is `migrate`, without `up`.

`infra:up` starts dependencies, API and mock engine. Add the `workers` profile
with `mise exec -- docker compose --profile workers up -d --wait` to run the
worker too. Compose profiles are additive; `infra:deps` explicitly selects only
the three data services and bucket initialization for host development.

```sh
mise run dev              # dependencies up; prints commands for three terminals
# Then, in separate terminals:
mise run api:migrate && mise run api:run
mise run engine:run
mise run client:dev
```

Use `infra:down` before switching from the full stack to host processes to free
ports. `infra:down` retains named volumes; deleting volumes is a separate,
destructive action.

## Common tasks and CI

```sh
mise tasks
mise run infra:deps
mise run infra:logs
mise run infra:lint
mise run infra:down
mise run api:test
mise run engine:test
mise run client:test
mise run proto:lint
mise run infra:restore-drill -- /path/to/anonymized.dump
```

Run only the focused area's checks locally. Every area exposes lint, test and
build tasks; CI invokes mise tasks. Draft PRs run fast path-filtered lint, unit
tests and mock evals. Ready PRs/main add integration, browser, container and Tauri
builds plus live sample evals when secrets exist. Namespace profiles provide all
CI runners; `ci-ok` is the aggregate required check. See [CI details](docs/ci.md).

`mise run setup` installs Lefthook hooks. Pre-commit checks Go, Rust and client
formatting in parallel for affected areas. Commit messages use
[Conventional Commits](https://www.conventionalcommits.org/), e.g.
`chore(infra): add local stack`.

## AI and evals

The engine routes Claude API requests with OpenRouter fallback and has a mock
provider for local use. Model IDs and task routing live in
`engine/config/models.toml`. The versioned golden set belongs in Hugging Face
[`datamonsterr/duolify-golden`](https://huggingface.co/datasets/datamonsterr/duolify-golden).

Run `mise run engine:eval` for offline mock checks. See [evals/README.md](evals/README.md)
for live checks, dataset revisions and thresholds. AI changes require golden-set
cases and passing evals. Provider keys and learner text never belong in commits.

## Railway and data

Railway configuration lives in [.railway/railway.ts](.railway/railway.ts), using
the TypeScript IaC SDK with its own package and lockfile. No legacy
`railway.json` or `railway.toml` is used.

```sh
mise run infra:plan        # links duolify/dev, installs SDK, previews changes
# Human-authorized only, after reviewing the plan (creates billable resources):
mise run infra:deploy-dev  # interactive config apply, guarded to duolify/dev
```

The engine has no public domain; the API resolves its Railway private domain.
API deployment runs migrations before starting and checks `/readyz`. Worker
migrations also run independently before startup. Provider/JWT/PostHog secrets
are preserved, never embedded in source. Production is outside bootstrap scope.
See [.railway/README.md](.railway/README.md) for target guards and SDK setup.

The worker implements nightly encrypted Postgres dumps and synthetic recovery
checks locally. Cloud keys, retention policies and Railway snapshots still need
owner provisioning before real data is enabled. See the
[backup setup and restore drill](infra/backup/README.md),
[data inventory](docs/data-inventory.md) and [backup spec](openspec/specs/data-backup/spec.md).
Redis is reconstructible; lesson assets need object versioning. Real data must
wait for cloud backup configuration, access controls and versioning verification.

## Agentic workflow

Start feature work with an OpenSpec change. Use `/opsx:new`, `/opsx:continue` or
`/opsx:ff` to prepare it, `/opsx:apply` to implement, `/opsx:verify` to check and
`/opsx:archive` after completion. Write e2e, integration and unit tests before
implementation; specs cover analytics, data backup and AI reliability.

OtherSkill routes skills using `otherskill.config.json` and the project bank
`.otherskill/bank` (run `otherskill setup` once to install the global routing
hook). Keep routing credentials outside the repo. The skill list, the ticket
flow and the reverse-engineering flow are in `docs/agents/` (`skills.md`,
`issue-tracker.md`, and the `reverse-to-spec` skill). Superorchestrator
coordinates work packages in separate git worktrees; merge reviewed commits
through the bootstrap branch. Branch from `main`, open a draft PR early and use
Conventional Commits. A task is complete after its PR merges into `main`; remove
its worktree with `git worktree remove <path>` afterwards.

## Human-required steps

- [ ] Fill `.env`: Anthropic and OpenRouter keys, `HF_TOKEN`, PostHog keys
  (`POSTHOG_API_KEY` and `VITE_POSTHOG_KEY`) and a random 32+ byte `JWT_SECRET`.
- [ ] Create HF dataset `datamonsterr/duolify-golden`, upload
  `evals/fixtures/golden.sample.jsonl` and pin its revision for live evals.
- [ ] Add GitHub secrets: `RAILWAY_TOKEN`, `ANTHROPIC_API_KEY`,
  `OPENROUTER_API_KEY`, `HF_TOKEN`, `POSTHOG_API_KEY`.
- [ ] Connect Namespace runner profiles `namespace-profile-datamonster01` and
  `namespace-profile-linux-xl` to this repository.
- [ ] Configure branch protection requiring `ci-ok`.
- [ ] Issue a Railway project token scoped to `duolify` / `dev`, populate cloud
  secrets and explicitly approve a reviewed plan before applying it.
- [ ] Install the Tauri OS packages above (or macOS Xcode tools).
- [ ] Refresh the OpenRouter key used by OtherSkill routing; the current key
  returns HTTP 401. Keep the replacement outside source control.
- [ ] Add `ELEVENLABS_API_KEY` to `.env` and GitHub secrets (slide narration).
- [ ] Install the REA plugin for reverse engineering:
  `claude plugin marketplace add morluto/rea` then
  `claude plugin install rea@rea --scope project`.
- [ ] Install Android SDK/NDK or full Xcode and mobile targets when needed.
- [ ] Finish and verify backup scheduling, 14-day retention, restricted bucket
  access, cloud asset versioning and scheduled restore drills before real data.
