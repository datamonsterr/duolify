# Project status

Updated 2026-11-13 by the master orchestrator session. Ticket data: `docs/epics/tickets.json`; issue numbers: `docs/epics/issues.json`; labels `status:implemented` / `status:in-progress`.

## Integration

- `main` holds bootstrap (#1), design (#50), ticket breakdown (#149), status and ADR 0012 (#167) and the MVP integration branch `impl/mvp` (#166, merge `c08e4b9`, full CI green: run 43378636155). `impl/mvp` is retired; epic branches open PRs straight to `main`.
- CI fixes in #166: Tauri crate missing `serde_json`; nextest archive lost the `duolify-eval` binary path; engine image missing the character schema files; `engine-eval-live` now reports `missing_config` without Hugging Face secrets instead of failing.
- Dependabot #2 (TypeScript 7) closed: typescript-eslint does not support TS 7 yet.
- Interim defaults for open human gates: ADR 0012 (pending owner confirmation).
- Open PRs: #168 E6, #169 E5, #170 E8 (child sessions).

## Epics

Done = closed, or `status:implemented` and still open for the listed reason. DoD per ticket: acceptance criteria in `tickets.json`, the named e2e → integration → unit tests present on `main`, full CI green. On 2026-11-13, 49 tickets and review issues #159–#163 were closed with evidence; epics E2, E4 and E9 are closed.

| Epic | Done | Remaining (issue) | Blocked by human gate |
|---|---|---|---|
| E1 Accounts | 11/12 | E1-12 #157 open: Rust keychain/keystore adapter tests missing | E1-10 #48 live auth credentials |
| E2 Course creation | 8/8, epic closed | — | — |
| E3 Lesson player | 6/6 implemented, kept open | review fixes #164 (E3 items) | — |
| E4 Lesson tests | 7/7, epic closed | — | — |
| E5 Gamification | 5/6 | E5-06 #89 (PR #169) | — |
| E6 Social | 2/7 | E6-02 #91 … E6-07 #95 (PR #168) | — |
| E7 Credits | 8/14 | E7-10 #66, E7-11 #67 (fixture work per ADR 0012) | E7-05 #24, E7-12 #143, E7-13 #144, E7-14 #145 |
| E8 Tutor | 3/5 | E8-04 #99, E8-05 #146 (PR #170) | — |
| E9 Notes | 5/5, epic closed | — | — |
| E10 Evals | 4/6 | E10-06 #109 (code on main; live part waits for E10-01) | E10-01 #25 HF dataset + eval secrets |
| E11 Ops | 2/7 | E11-03 #28, E11-05 #52, E11-06 #53 | E11-02 #27, E11-07 #54 |
| E12 Monetization | 7/10 (6 kept open for #165) | review fixes #165 (E12 items), E12-10 #155 | E12-08 #111, E12-09 #154 |
| E13 App design | 4/6 | E13-06 #36 | E13-05 #35 brand assets |
| E14 Narration | 0/6 | E14-01 #112 … E14-04 #115, E14-06 #117 | E14-05 #116 voices |
| E15 Sharing | 0/13 | E15-01 #118 … E15-11 #128 (after E6-04) | E15-12 #37, E15-13 #147 |
| E16 Steering | 2/8 | E16-01 #129, E16-02 #130, E16-03 #131, E16-07 #133; E16-05 #132 and E16-08 #134 after E15 | — |
| E17 Characters | 5/9 | review fixes #165 (E17 part fixed), E17-07 #148 (after E8-05), E17-08 #140 (after E14), E17-09 #141 (after E15/E16) | E17-02 #40 art and price |

## Plan

1. Done: #166 green and merged; implemented tickets closed with evidence.
2. Child sessions, at most three at a time: E5+E8 (then E17-07); E6 (then E15); review fixes + E12-10. Next: E14, E16, E11/E7 fixture work, E15, E17-08/09.

## Human-required

See the HITL issues above and the final report of the master session for exact steps: GitHub secrets, Hugging Face dataset `datamonsterr/duolify-golden`, branch protection on `main` requiring `checks / ci-ok`, stale `OPENROUTER_API_KEY` in `~/.config/environment.d/60-opencode-providers.conf`, REA plugin install.
