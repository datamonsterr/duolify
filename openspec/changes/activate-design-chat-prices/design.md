# Activate approved setup and outline-chat prices design

## Context
Ticket E12-10 activates the setup and outline chat prices on top of the active pricing catalogue. Owner decision E12-09 is pending; interim default ADR 0012 applies:
`setup_chat=0`, `course_brief_chat=0`, and `outline_chat_change=5` (Daily free eligible, charged only for a valid delivered proposal; Apply/Discard/manual edits free), active only in dev/test until owner approves.

## Architecture

### 1. Audited Pricing Config Version
- Built on top of the active catalogue (`usd-quotes-2026-11-09`).
- Introduces actions:
  - `setup_chat`: `{credits: 0}`
  - `course_brief_chat`: `{credits: 0}`
  - `outline_chat_change`: `{credits: 5}`
- Updates `daily_free.eligible_actions` to include `outline_chat_change`.
- Includes usage envelopes/limits for all three actions matching development specifications.
- Quote policy inherits model mappings (`haiku` for setup_chat, course_brief_chat, outline_chat_change) when approved.
- Goes through normal admin workflow: draft → validate → submit → approve → activation, writing `pricing_audit` rows and verification evidence.

### 2. Decision Gate
- Pricing versions introducing any of these three keys record `design_chat_decision`:
  - `"interim_adr_0012"`: interim decision under ADR 0012.
  - `"owner_approved"`: owner-approved decision under E12-09.
- Environment check fails closed: only `DUOLIFY_ENV` equal to `"development"` or `"test"` is treated as non-production; empty, unset, or other values are treated as production.
- Production rules:
  - Approving or scheduling a version with these keys requires `design_chat_decision == "owner_approved"` AND the approving admin must be `PRICING_OWNER_ID`.
  - `ActivateDue` refuses activation if a version contains these keys unless `owner_approved` and approved by `PRICING_OWNER_ID`.
  - Quoting any of these actions requires `owner_approved` and `PRICING_OWNER_ID` approval in production.
- Non-production rules:
  - In `development` and `test`, `design_chat_decision == "interim_adr_0012"` or `"owner_approved"` may be approved and activated.
  - A draft with missing or `"pending"` decision is rejected.

### 3. Action Flags and Rollback Isolation
- The three actions (`setup_chat`, `course_brief_chat`, `outline_chat_change`) are enabled only when the active (activated) version contains the respective action key AND passes the decision gate.
- Rollback (forward rollback draft omitting these keys) removes these keys, disabling only these actions while keeping all unrelated catalogue actions intact.
- When an action is disabled, form fallbacks, manual editing, and Apply/Discard remain functional without server error.
- In-flight reservations settle at their original quote version, preserving historical terms.

### 4. Dev-only Path Resolution
- `Quotes.CreateDevelopmentOutline` and `pricing.LoadDevelopment` check if an activated version carrying the respective keys is active.
- If an activated version exists, dev-only paths resolve through the active version.
- If no activated version carries the keys (e.g., initial unmigrated dev environment), they fall back to the development migrations, still env-gated.

### 5. Charging and Proposal Settlement
- `outline_chat_change` settles credits only upon valid delivered proposal.
- Failed or cancelled proposal generation releases held reservations without credit deduction.
- Proposal Apply, Discard, and manual adjustments incur 0 credits.

### 6. Analytics and Data Backup
- Standard lifecycle events (`pricing_version_approved`, `pricing_version_activated`, `pricing_version_rolled_back`) are emitted with canonical properties, strictly omitting reason text or chat content.
- Pinned pricing versions, audit records, and verification evidence join 7-year nightly financial backup retention.
- Backup restore drill asserts identical resolver output and action flags before and after restore.

### 7. AI Reliability & Model Safety
- No new prompts or models are added in this ticket; model bracket is Claude 3.5 Haiku.
- Bounded operational envelopes (tokens, latency, retry clamps) are enforced via config limits.
- Feature promotional requirements remain pinned to E10-06 live evaluations independently of pricing approvals.
