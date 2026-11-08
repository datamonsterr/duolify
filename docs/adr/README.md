# Architecture decision records

| ADR | Title | Status |
|---|---|---|
| [0001](0001-monorepo-layout.md) | Single monorepo, one directory per deployable | Proposed |
| [0002](0002-service-architecture.md) | Go API in front, Rust engine behind gRPC | Proposed |
| [0003](0003-data-stores.md) | Postgres, Redis, S3-compatible storage, Postgres job queue | Proposed |
| [0004](0004-auth.md) | First-party auth in the API | Proposed |
| [0005](0005-client-and-lesson-player.md) | Tauri 2 + React client, Motion HTML slides in sandboxed iframes | Proposed |
| [0006](0006-llm-providers-and-evals.md) | LLM providers, gated generation, golden set on Hugging Face | Proposed |
| [0007](0007-credits-and-payments.md) | Credits ledger, Stripe for top-ups, effort rewards | Proposed |
| [0008](0008-ci-and-deploy.md) | Two-tier CI on Namespace runners, Railway deploy | Accepted |

New ADR: copy the newest file, increase the number, keep it under one page.
