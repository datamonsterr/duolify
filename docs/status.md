# Project status

Updated 2026-11-14 11:20 UTC by the master orchestrator session. Ticket data: `docs/epics/tickets.json`; issue numbers: `docs/epics/issues.json`; labels `status:implemented` / `status:in-progress`. DoD per ticket: acceptance criteria in `tickets.json`, the named e2e → integration → unit tests on `main`, full CI (`checks / ci-ok`) green. Issues are closed only with an evidence comment.

## Summary

| State | Epics |
|---|---|
| Complete (epic closed) | E2 #4, E3 #5, E4 #6, E5 #7, E6 #8, E8 #10, E9 #11 |
| Agent work done, waiting only on owner (HITL) | E1 #3, E7 #9, E10 #12, E11 #13, E12 #14, E13 #15, E14 #16, E15 #17 |
| In progress | E16 #18, E17 #19 (E17-09) |

## Per epic

| Epic | Done | In progress / remaining (issue, PR) | Blocked by human gate |
|---|---|---|---|
| E1 Accounts | 11/12 | — (E1-12 merged in #189; iOS/Android device-only paths untestable on Linux CI) | E1-10 #48 live auth credentials |
| E7 Credits | 10/14 | — (E7-10/11 merged in #191) | E7-05 #24, E7-12 #143, E7-13 #144, E7-14 #145 |
| E10 Evals | 4/6 | E10-06 #109: code on main, live part needs E10-01 | E10-01 #25 HF dataset + eval secrets |
| E11 Ops | 5/7 | — (E11-03/05/06 merged in #188) | E11-02 #27 backup storage + keys, E11-07 #54 env secrets + rollout |
| E12 Monetization | 8/10 | — (E12-10 merged in #172; Railway dev runs `DUOLIFY_ENV=dev` so interim chat prices stay off there) | E12-08 #111 real product mapping, E12-09 #154 chat prices |
| E13 App design | 4/6 | E13-06 #36 after E13-05 | E13-05 #35 final brand assets |
| E14 Narration | 5/6 | — (E14-01/02/03/04/06 merged in #186; live ElevenLabs off unless `NARRATION_LIVE_ENABLED=true` + key + voice) | E14-05 #116 live voices |
| E15 Sharing | 11/13 | — (E15-01…11 merged in #176, #192, #203, #208; marketplace off by default) | E15-12 #37 sharing policy, E15-13 #147 pilot |
| E16 Steering | 2/8 | E16-01…03, 05, 07 in progress on PR #201; E16-08 #134 next | — |
| E17 Characters | 7/9 | E17-08 merged in #193; E17-09 #141 after E16-05 (E15-11 merged) | E17-02 #40 art and price |

All AI tickets close with one known gap: golden rows live in `evals/fixtures` until the Hugging Face dataset exists (E10-01 #25).

## Children (at most three)

- `duolify E16` → E16 (PR #201), then E17-09.
- `duolify followups` → #194 (PR #209 merged), #199 (PR #211), #206, #207, #205 (PR #210), #212.

## Engineering notes

- One shared Rust target dir for all agents: `CARGO_TARGET_DIR=/home/dat/.cache/duolify-target`.
- Migrations: goose rejects versions older than main's newest; new migrations must sort after the newest file on `main` at merge time.
- CI: API integration split into three parallel shards (#200); Playwright step limit 10 min (#204), sharding in #210; compiled `pricingserver` binary no longer tracked (#197).
- Interim defaults for open human gates: ADR 0012 (pending owner confirmation).

## Human-required

GitHub secrets (`gh secret set <NAME> -R datamonsterr/duolify`): OPENROUTER_API_KEY, ANTHROPIC_API_KEY, ELEVENLABS_API_KEY, HF_TOKEN, RAILWAY_TOKEN, POSTHOG_API_KEY, BACKUP_* (restore drill). Hugging Face dataset `datamonsterr/duolify-golden` + pinned revision. Branch protection on `main` requiring `checks / ci-ok`. Stale `OPENROUTER_API_KEY` in `~/.config/environment.d/60-opencode-providers.conf`. REA plugin install. Confirmation of ADR 0012 and the HITL issues listed above.
