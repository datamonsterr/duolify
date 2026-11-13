# Activate approved setup and outline-chat prices

Add the approved setup, course-brief, and sample-outline-chat keys through a new audited Pricing config version and activate only these gated actions while preserving the approved launch seed and the separate E16 outline_replan price.

## Key Changes
- Audited Pricing config version introducing `setup_chat=0`, `course_brief_chat=0`, and `outline_chat_change=5` (with Daily free eligibility for outline_chat_change and settlement only on valid delivered proposal) drafted, validated, submitted, and approved through the standard admin workflow.
- Decision gate: version records `design_chat_decision` ("interim_adr_0012" | "owner_approved"). Outside explicit development/test environments, approving, activating, or quoting a version with these keys is rejected unless the decision is `owner_approved` AND approved by `PRICING_OWNER_ID` (fail closed). In dev/test, the interim ADR 0012 decision may be approved and activated.
- Action flags: `setup_chat`, `course_brief_chat`, and `outline_chat_change` actions are enabled only when the active (activated) version contains the key and passes the decision gate. Rollback disables only these actions; manual outline editing, Apply/Discard, and form fallback stay available. In-flight reservations settle at their original quote version.
- Dev-only path resolution: `Quotes.CreateDevelopmentOutline` and `pricing.LoadDevelopment` resolve through the activated version once present, keeping dev fallback only for environments without it.
- Analytics: `pricing_version_approved`, `pricing_version_activated`, and `pricing_version_rolled_back` emitted from canonical API owners only with common UUID properties and no reason/chat text.
- Data backup: immutable pricing versions, audit rows, and verification evidence retained in nightly financial backups for at least 7 years; restore resolves the same active version and action flags.
- Model reliability: no new prompts or models; reuse E1-11/E2-06/E2-08 golden cases and mock-provider activation evidence; require E10-06 for live promotion independently of pricing approval.
