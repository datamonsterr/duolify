# Project status

Updated 2026-11-13 (evening) by the master orchestrator session. Ticket data: `docs/epics/tickets.json`; issue numbers: `docs/epics/issues.json`; labels `status:implemented` / `status:in-progress`.

## Integration

- `main` holds bootstrap (#1), design (#50), ticket breakdown (#149), ADR 0012 (#167), the MVP integration branch (#166, merge `c08e4b9`), E8 (#170), E6-01…04 (#168), E17-07 (#177), E5 (#169), E6-06/07 (#185) and flaky-test fixes (#178, #180, #181, #182, #184). Every merge had a green full pipeline (`checks / ci-ok`).
- `impl/mvp` is retired; epic branches open PRs straight to `main`.
- Flaky tests found on the way were fixed at the root, never quarantined: #173 (E17-06 effect timing), #174 (notes refetch race), #175 (enrichment marked failed on a busy reservation slot — product bug), #179 (unscoped status locator), #183 (Go map order in a social test).
- Interim defaults for open human gates: ADR 0012 (pending owner confirmation).
- Open PRs: #172 review fixes E3/E12 + E12-10, #176 E15 (draft), E14 (child session, PR pending).

## Epics

Done = closed, or `status:implemented` and kept open for the listed reason. DoD per ticket: acceptance criteria in `tickets.json`, the named e2e → integration → unit tests on `main`, full CI green. Epics closed: E2, E4, E5, E6, E8, E9.

| Epic | Done | Remaining (issue) | Blocked by human gate |
|---|---|---|---|
| E1 Accounts | 11/12 | E1-12 #157 open: Rust keychain/keystore adapter tests missing | E1-10 #48 live auth credentials |
| E2 Course creation | closed | — | — |
| E3 Lesson player | 6/6 implemented, kept open | review fixes #164 (E3 items, PR #172) | — |
| E4 Lesson tests | closed | — | — |
| E5 Gamification | closed | gaps noted on #89: no analytics canvas screen; client parses the full export before saving | — |
| E6 Social | closed | — | — |
| E7 Credits | 8/14 | E7-10 #66, E7-11 #67 (fixture work per ADR 0012, needs E11-03/05) | E7-05 #24, E7-12 #143, E7-13 #144, E7-14 #145 |
| E8 Tutor | closed | — | — |
| E9 Notes | closed | — | — |
| E10 Evals | 4/6 | E10-06 #109 (code on main; live part waits for E10-01) | E10-01 #25 HF dataset + eval secrets |
| E11 Ops | 2/7 | E11-03 #28, E11-05 #52, E11-06 #53 (dev-only per ADR 0012) | E11-02 #27, E11-07 #54 |
| E12 Monetization | 7/10 (6 kept open for #165) | review fixes #165, E12-10 #155 (PR #172) | E12-08 #111, E12-09 #154 |
| E13 App design | 4/6 | E13-06 #36 | E13-05 #35 brand assets |
| E14 Narration | 2/6 in progress | E14-03 #114, E14-04 #115, E14-06 #117 | E14-05 #116 voices |
| E15 Sharing | 1/13 in progress | E15-02 #119 … E15-11 #128 (PR #176) | E15-12 #37, E15-13 #147 |
| E16 Steering | 2/8 | E16-01 #129, E16-02 #130, E16-03 #131, E16-07 #133 (after E14); E16-05 #132, E16-08 #134 after E15 | — |
| E17 Characters | 6/9 | E17-08 #140 (after E14), E17-09 #141 (after E15/E16/E11-05) | E17-02 #40 art and price |

## Children (at most three)

- `duolify E6` → now E15 (PR #176).
- `duolify review-fixes E3-E12` → #164, #165, E12-10 (PR #172).
- `duolify E14-E16` → E14, then E16-01/02/03/07.
- Next when a slot frees: E11-03/05/06 + E7-10/11 fixture work; E1-12 adapter tests; E17-08 after E14.

## Capacity

Coding-agent quota is the bottleneck: codex-1/2 return ~2026-11-14T06:10Z; codex-acc1/2 and several agy accounts hit limits on 2026-11-13.

## Human-required

See the HITL issues above and the master session's final report: GitHub secrets, Hugging Face dataset `datamonsterr/duolify-golden`, branch protection on `main` requiring `checks / ci-ok`, stale `OPENROUTER_API_KEY` in `~/.config/environment.d/60-opencode-providers.conf`, REA plugin install, confirmation of ADR 0012.
