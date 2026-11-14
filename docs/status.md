# Project status

Updated 2026-11-14 14:30 UTC by the master orchestrator session. Ticket data: `docs/epics/tickets.json`; issue numbers: `docs/epics/issues.json`. DoD per ticket: acceptance criteria in `tickets.json`, the named e2e → integration → unit tests on `main`, full CI (`checks / ci-ok`) green. Issues are closed only with an evidence comment.

**All agent-buildable work is merged.** Every open ticket now waits on an owner decision, credential or approval (HITL), or on a ticket that does.

## Summary

| State | Epics |
|---|---|
| Complete (epic closed) | E2 #4, E3 #5, E4 #6, E5 #7, E6 #8, E8 #10, E9 #11, E16 #18 |
| Agent work done, waiting only on owner (HITL) | E1 #3, E7 #9, E10 #12, E11 #13, E12 #14, E13 #15, E14 #16, E15 #17, E17 #19 |

## Open tickets per epic

| Epic | Done | Open (all HITL or blocked by HITL) |
|---|---|---|
| E1 Accounts | 11/12 | E1-10 #48 live mail/OAuth credentials |
| E7 Credits | 10/14 | E7-05 #24 billing policy + Stripe/Apple/Google accounts; E7-12 #143, E7-13 #144, E7-14 #145 store/Stripe pilots |
| E10 Evals | 4/6 | E10-01 #25 HF dataset + eval secrets; E10-06 #109 (agent ticket, needs E10-01; code and missing-config path on main) |
| E11 Ops | 5/7 | E11-02 #27 backup storage, key custody, IAM grants; E11-07 #54 env secrets + rollout approval |
| E12 Monetization | 8/10 | E12-08 #111 real product mapping; E12-09 #154 chat prices (interim 0/0/5, dev/test only) |
| E13 App design | 4/6 | E13-05 #35 brand assets + usability evidence; E13-06 #36 (agent ticket, needs E13-05) |
| E14 Narration | 5/6 | E14-05 #116 approve live voices |
| E15 Sharing | 11/13 | E15-12 #37 sharing rights/policy; E15-13 #147 marketplace pilot |
| E17 Characters | 8/9 | E17-02 #40 character_create price + default-cast art |

All AI tickets closed with one known gap: golden rows live in `evals/fixtures` until the Hugging Face dataset exists (E10-01 #25).

## Defaults in force until the owner answers

ADR 0011 "Defaults chosen during implementation" and ADR 0012 (interim defaults for human gates). Nothing goes live, charges real money or publishes publicly: live providers, Stripe live mode, marketplace (`MARKETPLACE_ENABLED`), public character export, live voices and push notifications all default off.

## Engineering notes

- CI: API integration runs as three parallel shards (#200); Playwright runs as three shards (#210); `ci-ok` requires every shard.
- Migrations: goose rejects versions older than main's newest; new migrations must sort after the newest file on `main` at merge time.
- One shared Rust target dir for agents: `CARGO_TARGET_DIR=/home/dat/.cache/duolify-target`.
- Generated lessons publish player manifests at settlement (#221); a worker backfill and the #219 read-time bridge cover older lessons.

## Human-required

GitHub secrets (`gh secret set <NAME> -R datamonsterr/duolify`): OPENROUTER_API_KEY, ANTHROPIC_API_KEY, ELEVENLABS_API_KEY, HF_TOKEN, RAILWAY_TOKEN, POSTHOG_API_KEY, BACKUP_S3_ENDPOINT, BACKUP_S3_REGION, BACKUP_S3_BUCKET, BACKUP_RESTORE_S3_ACCESS_KEY_ID, BACKUP_RESTORE_S3_SECRET_ACCESS_KEY, BACKUP_AGE_IDENTITY, BACKUP_FINANCIAL_FIXTURES, BACKUP_FINANCIAL_FIXTURE_SIGNATURE, BACKUP_FINANCIAL_FIXTURE_KEY. Hugging Face dataset `datamonsterr/duolify-golden` + pinned revision in `evals/pinned-datasets.toml`. Branch protection on `main` requiring `checks / ci-ok`. Cloud IAM: backup writer read-only on the app bucket `lessons/*`. Stale `OPENROUTER_API_KEY` in `~/.config/environment.d/60-opencode-providers.conf`. REA plugin install. Confirmation of ADR 0012 and the HITL issues above.
