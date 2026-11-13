# Project status

Updated 2026-11-13 by the master orchestrator session. Ticket data: `docs/epics/tickets.json`; issue numbers: `docs/epics/issues.json`; labels `status:implemented` / `status:in-progress`.

## Integration

- `main`: bootstrap (#1), design (#50), ticket breakdown and owner decisions (#149) merged.
- `impl/mvp` → `main`: PR #166 (replaces #156). Holds E1–E4, E7, E9–E13, E17 work. CI red on 2026-11-12: `client` (Tauri crate missing `serde_json` for `generate_context!`) and `engine-test` (`duolify-eval` binary missing from the nextest archive). Fix in progress.
- Unmerged epic work: `epic/e5` (+21 commits, E5-04/05), `epic/e8` (+26 commits, E8-03/04).
- Dependabot #2 (TypeScript 7): not mergeable, typescript-eslint does not support TS 7 yet; told dependabot to ignore this major.
- Interim defaults for open human gates: ADR 0012 (pending owner confirmation).

## Epics

Done = closed or `status:implemented`. DoD for every ticket: acceptance criteria in `tickets.json`, tests e2e → integration → unit, green CI, merged to `main`. No ticket is closed yet because nothing past #149 is on `main`.

| Epic | Done | Remaining (issue) | Blocked by human gate |
|---|---|---|---|
| E1 Accounts | 11/12 | review fixes #163 (fixed on branch, verify) | E1-10 #48 live auth credentials |
| E2 Course creation | 8/8 | review fixes #164 (E2 part fixed) | — |
| E3 Lesson player | 6/6 | review fixes #164 (E3 items 2, 3, 6) | — |
| E4 Lesson tests | 7/7 | review fixes #161 (fixed on branch, verify) | — |
| E5 Gamification | 4/6 | E5-05 #88 (in progress on `epic/e5`), E5-06 #89 | — |
| E6 Social | 1/7 | E6-01 #90 … E6-07 #95 | — |
| E7 Credits | 8/14 | review fixes #159 (verify), E7-10 #66, E7-11 #67 | E7-05 #24, E7-12 #143, E7-13 #144, E7-14 #145 |
| E8 Tutor | 3/5 | E8-04 #99 (in progress on `epic/e8`), E8-05 #146 | — |
| E9 Notes | 5/5 | review fixes #162 (fixed on branch, verify) | — |
| E10 Evals | 4/6 | review fixes #160 (verify), E10-06 #109 code present, CI red | E10-01 #25 HF dataset + eval secrets |
| E11 Ops | 2/7 | E11-03 #28, E11-05 #52, E11-06 #53 | E11-02 #27, E11-07 #54 |
| E12 Monetization | 7/10 | review fixes #165 (E12 items), E12-10 #155 | E12-08 #111, E12-09 #154 |
| E13 App design | 4/6 | E13-06 #36 | E13-05 #35 brand assets |
| E14 Narration | 0/6 | E14-01 #112 … E14-04 #115, E14-06 #117 | E14-05 #116 voices |
| E15 Sharing | 0/13 | E15-01 #118 … E15-11 #128 (after E6-04) | E15-12 #37, E15-13 #147 |
| E16 Steering | 2/8 | E16-01 #129, E16-02 #130, E16-03 #131, E16-07 #133; E16-05 #132 and E16-08 #134 after E15 | — |
| E17 Characters | 5/9 | review fixes #165 (E17 part fixed), E17-07 #148 (after E8-05), E17-08 #140 (after E14), E17-09 #141 (after E15/E16) | E17-02 #40 art and price |

## Plan

1. Green CI on #166, merge to `main`, close implemented tickets with evidence.
2. Child sessions, at most three at a time: E5+E8 (then E17-07); E6 (then E15); review fixes + E12-10. Next: E14, E16, E11/E7 fixture work, E15, E17-08/09.

## Human-required

See the HITL issues above and the final report of the master session for exact steps: GitHub secrets, Hugging Face dataset `datamonsterr/duolify-golden`, branch protection on `main` requiring `checks / ci-ok`, stale `OPENROUTER_API_KEY` in `~/.config/environment.d/60-opencode-providers.conf`, REA plugin install.
