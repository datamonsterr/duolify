# Project status

Updated 2026-11-14 03:10 UTC by the master orchestrator session. Ticket data: `docs/epics/tickets.json`; issue numbers: `docs/epics/issues.json`; labels `status:implemented` / `status:in-progress`. DoD per ticket: acceptance criteria in `tickets.json`, the named e2e → integration → unit tests on `main`, full CI (`checks / ci-ok`) green. Issues are closed only with an evidence comment.

## Summary

| State | Epics |
|---|---|
| Complete (epic closed) | E2 #4, E3 #5, E4 #6, E5 #7, E6 #8, E8 #10, E9 #11 |
| In progress | E7 #9, E14 #16, E15 #17, E16 #18, E17 #19, E1 #3 (test gap) |
| Agent work done, waiting only on owner (HITL) | E10 #12, E11 #13, E12 #14, E13 #15 |

## Per epic

| Epic | Done | In progress / remaining (issue, PR) | Blocked by human gate |
|---|---|---|---|
| E1 Accounts | 11/12 | E1-12 #157 adapter tests — PR #189 (full pipeline running) | E1-10 #48 live auth credentials |
| E7 Credits | 8/14 | E7-10 #66 (reviewing agent commit on `epic/e7`), E7-11 #67 (after E7-10) — fixture provider per ADR 0012 | E7-05 #24, E7-12 #143, E7-13 #144, E7-14 #145 |
| E10 Evals | 4/6 | E10-06 #109: code on main, live part needs E10-01 | E10-01 #25 HF dataset + eval secrets |
| E11 Ops | 5/7 | — (E11-03/05/06 merged in #188) | E11-02 #27 backup storage + keys, E11-07 #54 env secrets + rollout |
| E12 Monetization | 8/10 | — (E12-10 and review fixes merged in #172) | E12-08 #111 real product mapping, E12-09 #154 chat prices |
| E13 App design | 4/6 | E13-06 #36 after E13-05 | E13-05 #35 final brand assets |
| E14 Narration | 3/6 | E14-03 #114, E14-04 #115, E14-06 #117 — PR #186 (draft, conflicts with main being resolved) | E14-05 #116 live voices |
| E15 Sharing | 4/13 | E15-01…04 PR #176 (green, merging); E15-05 #122, E15-07 #124 running; E15-06 #123, E15-08…11 #125–#128 next | E15-12 #37 sharing policy, E15-13 #147 pilot |
| E16 Steering | 2/8 | E16-01 #129, E16-02 #130, E16-03 #131, E16-05 #132, E16-07 #133, E16-08 #134 — not started | — |
| E17 Characters | 6/9 | E17-08 #140 (after E14), E17-09 #141 (after E15-05/11, E16-05) | E17-02 #40 art and price |

## Children (at most three)

- `duolify E6` → E15 (PR #176, then part 2).
- `duolify E14-E16` → E14 (PR #186), then E17-08.
- `duolify E11-E7-E1` → E1-12 (PR #189), E7-10/11; then its slot goes to a dedicated E16 child.

## Engineering notes

- All children share one Rust target dir `CARGO_TARGET_DIR=/home/dat/.cache/duolify-target` (disk reached 90% with per-epic target dirs).
- Coding-agent quota is the bottleneck: codex-1/2 return ~06:10 UTC; several agy accounts are capped.
- Interim defaults for open human gates: ADR 0012 (pending owner confirmation).

## Human-required

GitHub secrets, Hugging Face dataset `datamonsterr/duolify-golden`, branch protection on `main` requiring `checks / ci-ok`, stale `OPENROUTER_API_KEY` in `~/.config/environment.d/60-opencode-providers.conf`, REA plugin install, confirmation of ADR 0012, and the HITL issues listed above.
