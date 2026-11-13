# ADR 0012: Interim defaults for open human gates and integration flow

- Status: Proposed (orchestrator default, pending owner confirmation)
- Date: 2026-11-13

## Context

Several human-in-the-loop (HITL) tickets block agent work that can be built and tested against fixtures. ADR 0011 ("Defaults chosen during implementation") already lets fixture-backed work go ahead while live activation waits for the owner. This ADR applies the same rule to the remaining gates. None of these defaults turns on a live provider, a real charge, public content or a production deploy.

## Decisions

| Gate | Default until the owner answers | Unblocks |
|---|---|---|
| E12-09 #154 prices | Draft Pricing config values `setup_chat=0`, `course_brief_chat=0`, `outline_chat_change=5` (Daily free credits eligible, charged only for a valid delivered proposal; Apply/Discard and manual edits free). Active only in dev/test until the owner approves. | E12-10 #155 |
| E15-12 #37 sharing policy | v1 defaults stand: learner pays for missing lessons, no flow-back to the owner, no forked updates, public pages `noindex`. Published snapshots exclude narration audio (regenerated per learner) until ElevenLabs redistribution is confirmed. Publishing requires an adult (18+) account, set in config. Moderation queue owner: the project owner. Push notifications fall back to in-app until APNs/FCM credentials exist. Domain and CDN come from config. | E15-02 … E15-11 (fixture-backed) |
| E14-05 #116 voices | One configured default voice plus test fixtures; live voices stay disabled. | E14-06 #117 |
| E7-05 #24 billing accounts | Refunds, disputes and reconciliation use the existing injected signed test provider (as in E7-06 … E7-09); live mode stays rejected. | E7-10 #66, E7-11 #67 |
| E11-02 #27 backup storage | Restore drills run against the local S3-compatible store (RustFS/MinIO) with versioning on; production storage and key custody stay with the owner. | E11-03 #28, E11-05 #52, E11-06 #53 (dev only) |

The HITL tickets stay open: they still need the owner's answer and credentials. Work built on these defaults keeps live activation behind the existing gates.

## Integration flow

Once `impl/mvp` (PR #166, which replaced #156) is green and merged into `main`, epic branches open PRs straight to `main` and run the full pipeline, as `AGENTS.md` describes. `impl/mvp` is then retired.

## Consequences

Changing a default takes a new Pricing config version or a follow-up ADR. When the owner answers, record the answer in the HITL ticket and replace the row here.
