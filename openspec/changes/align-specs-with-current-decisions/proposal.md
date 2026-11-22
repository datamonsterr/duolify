# Align specs with current decisions

## Why

The 113-change sync preserved historical requirements that later decisions superseded. This docs-only change reconciles those requirements with the accepted decisions and current implementation, and applies the deltas to the main specs in the same change. It does not implement the separately proposed features below.

## Decisions and sources

- ADR 0014, amendment 2026-11-22: **“No migration from Railway”**; the Hetzner stack is a fresh deploy. The same amendment says **“everything stays on R2 for this phase, backups included, and the missing versioning is accepted.”** R2 cannot retain overwritten versions; bucket locks and expiry protect WAL and base backups. `infra/deploy/compose.prod.yml` and `services/api/internal/backup/character_assets.go` document encrypted checksum-pinned protected asset copies, including the `unversioned` marker. Version-dependent recovery and deletion-ledger behavior remain an explicit limitation. ADR 0003 and 0008 retain historical Railway text; their status notes below identify the superseding decisions.
- ADR 0015: **“OpenRouter is the only provider”**; `tencent/hy4-flash` is primary and `z-ai/glm-5.3-flash` fallback. ADR 0018's structured output and screen reasoning and ADR 0019's promotion evidence gate remain in force.
- ADR 0016: **“All social SSO (Google, Apple, later others) goes through Clerk.”** First-party email codes remain; Clerk identity verification and linking requirements protect production sessions.
- ADR 0017: **“The lesson test is a core feature and is on in every environment by default.”** Flags are kill switches. ADR 0011 / E4-06 rules out on-demand retry variant generation at launch; exhausted pools are reused reordered for free. Pool requirements retain complementary validation and behavior, cross-referenced.
- `openspec/changes/optimize-api-hot-paths/design.md` specifies the Course list summary; `services/api/internal/courses/course_selection.go` implements it.
- Owner decisions 2026-11-22, recorded in ADR 0015: published Courses outlive their author's account with anonymized attribution; staging is Hetzner Falkenstein `fsn1` on `cx23` (Helsinki unavailable within budget), with production location open; Clerk handles Google/Apple and email codes stay first-party. Daily check-in credits (150/day, 250 first time, up to seven days banked, automatic on app open), example Courses and first-run tour have their own in-progress changes and are not specified here.
- `docs/infra/staging.md`, `docs/infra/hardening.md`, `infra/deploy` and `.github/workflows/deploy-vm.yml` describe the current host and deploy. Existing Railway migration baselines and switchover gates do not apply.
- `course-steering` had two ADDED headings; the duplicate heading is removed without changing its requirements. The historical `handle-exhausted-retry-pools` MODIFIED classification remains untouched if validation passes.

## Overlap review

The social, Quick notes, Tutor chat, credits and Course sharing pairs retain distinct detailed behavior; cross-references make their relationship explicit. The lesson retry pool pair retains separate validation and exhausted-pool requirements. The temporary blanket publishing refusal in `Owner-only preview and publishing guard` is removed because later sharing requirements govern publication.

## Needs owner decision

- Production Hetzner location remains open (`docs/infra/staging.md`).
- Decide version-aware asset recovery and deletion-ledger versioning before real assets or the deletion ledger depend on object versions (ADR 0014 amendment). R2 does not guarantee recovery of overwritten versions.
- The migration-specific `Analytics continuity` rollback thresholds remain in the spec pending a decision on their replacement; there was no Railway cutover to which they can be applied.

## Impact

Documentation and specs only. No new analytics, backup mechanism, model behavior or code is introduced. Existing backup, deletion, analytics and AI reliability obligations remain in force except for the explicitly accepted R2 limitations and cancelled migration requirements.
