# Project status

Updated 2026-11-21 by the master session. Everything needed from the owner is in [`OWNER-NEEDS.md`](OWNER-NEEDS.md); this file only tracks engineering state. Ticket data: `docs/epics/tickets.json`. Definition of done per ticket: its acceptance criteria, the named e2e, integration and unit tests on `main`, and a green `checks / ci-ok`.

## Summary

| State | Epics |
|---|---|
| Complete (epic closed) | E2 #4, E3 #5, E4 #6, E5 #7, E6 #8, E8 #10, E9 #11, E13 #15, E16 #18 |
| Agent work done, waiting only on the owner | E1 #3, E7 #9, E10 #12, E11 #13, E12 #14, E14 #16, E15 #17, E17 #19 |
| In progress | E18 #231 infrastructure cost migration (ADR 0014) |

CI has been unavailable since 2026-11-21 08:04 UTC: no job is picked up by the Namespace runners (OWNER-NEEDS X1). Every PR below is verified only by local mise tasks.

## Open tickets per epic

| Epic | Open tickets |
|---|---|
| E1 Accounts | #48 live mail and OAuth credentials (owner) |
| E7 Credits | #24 Stripe account and keys (owner); #145 Stripe pilot (owner); #143, #144 Apple and Google billing (parked by ADR 0013) |
| E10 Evals | #25 Hugging Face dataset and eval secrets (owner); #109 live-sample gate (agent, blocked by #25) |
| E11 Ops | #27 backup keys and storage (owner); #54 environment secrets and rollout (owner) |
| E12 Monetization | #111 real product mapping and catalogue activation (owner) |
| E14 Narration | #116 live voices (owner, blocked by #109) |
| E15 Sharing | #37 sharing release approvals (owner); #147 marketplace pilot (deferred by ADR 0013) |
| E17 Characters | #40 default-cast art and names (owner; review sheet in PR #262) |
| E18 Infrastructure | see below |

## E18 state

| Ticket | State |
|---|---|
| Self-hosted Docker stack | Draft PR #255, reviewed, running on the owner's machine |
| #233 load baseline and replication check | Draft PR #261, rework in progress (depends on #264) |
| #234 SSE heartbeats and cache policy | Draft PR #264, reviewed and fixed |
| #236 S3 conformance suite | Draft PR #260, reviewed and fixed |
| #237 object copy tool and read-fallback | In progress |
| #239 prod overlay, WAL archiving, DR copy, alerts, deploy job | Draft PR #263 (stacked on #255), reviewed and fixed |
| #240 switchover gate | In progress |
| #241 staging rehearsal and runbook | Blocked: needs Helsinki staging (#238, owner) |
| #243 decommission Railway | Blocked: after the production migration (#242, owner) |
| #232, #235, #238, #242 | Owner tasks |

Open question that changes this epic: the Railway project has no services, so there may be nothing to migrate (OWNER-NEEDS D1).

## Defaults in force until the owner answers

ADR 0011 "Defaults chosen during implementation", ADR 0012 and ADR 0013. Nothing goes live, charges real money or publishes publicly: live providers, Stripe live mode, the marketplace (`MARKETPLACE_ENABLED`), public character export, live voices and push notifications all default off.

## Engineering notes

- CI: API integration runs as three parallel shards and Playwright as three shards; `ci-ok` requires every shard. A PR that conflicts with `main` gets no CI run.
- Migrations: goose rejects versions older than the newest on `main`; rename new migrations to sort after it at merge time.
- One shared Rust target directory for agents: `CARGO_TARGET_DIR=/home/dat/.cache/duolify-target`.
- The full `api:test-integration` task takes more than ten minutes; agents run it in package groups with `API_TEST_PACKAGES`.
- Merge order for the open PRs: #264 before #261; #255 before #263; #260 and #262 are independent.
