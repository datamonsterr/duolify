## ADDED Requirements

### Requirement: Design chat pricing keys and interim governance
The versioned Pricing config SHALL support `setup_chat` (0 Credits), `course_brief_chat` (0 Credits), and `outline_chat_change` (5 Credits) with Daily free eligibility for `outline_chat_change`. A version introducing any of these keys SHALL record `design_chat_decision` ("interim_adr_0012" or "owner_approved"). Outside explicit `development` or `test` environments (`DUOLIFY_ENV`), approving, activating, or quoting a version containing these keys SHALL be rejected unless `design_chat_decision` is `owner_approved` AND the version was approved by `PRICING_OWNER_ID`. In `development` and `test`, the interim decision `interim_adr_0012` MAY be approved and activated. Pending or missing decision values SHALL NOT enable these actions in any environment.

#### Scenario: Production rejection of interim decision
- **WHEN** an administrator attempts to approve or activate a version with `design_chat_decision: interim_adr_0012` in production
- **THEN** approval or activation is rejected and the active catalogue remains unchanged

#### Scenario: Production approval with owner decision
- **WHEN** the `PRICING_OWNER_ID` administrator approves a version with `design_chat_decision: owner_approved` in production
- **THEN** approval succeeds and verification evidence is recorded

#### Scenario: Development approval of interim decision
- **WHEN** an administrator approves a version with `design_chat_decision: interim_adr_0012` under `DUOLIFY_ENV=development`
- **THEN** approval succeeds and the version becomes eligible for database-time activation

### Requirement: Action flags and rollback isolation
The actions `setup_chat`, `course_brief_chat`, and `outline_chat_change` SHALL be enabled if and only if the active (activated) pricing version contains the corresponding key and passes the decision gate. When active pricing does not contain the key or when a forward rollback version omitting the key is activated, these actions SHALL be disabled immediately. Manual outline editing, proposal Apply, proposal Discard, and form fallbacks SHALL remain available without error when chat actions are disabled. In-flight reservations created under an earlier active version SHALL settle against their original quoted version.

#### Scenario: Rollback disables chat actions while preserving catalogue
- **WHEN** a forward rollback version without design chat keys is activated
- **THEN** `setup_chat`, `course_brief_chat`, and `outline_chat_change` become disabled while unrelated actions remain active and quotable

#### Scenario: In-flight reservation settlement across rollback
- **WHEN** a proposal reservation is held under an activated version that is subsequently rolled back
- **THEN** validated proposal delivery settles at the original reservation quote version

### Requirement: Validated proposal settlement and free actions
`outline_chat_change` SHALL charge Credits only upon delivery of a valid generated proposal. A failed or timed-out proposal generation SHALL release the held reservation without deducting Credits. Applying a delivered proposal, discarding a proposal, undoing an applied change, and manual outline edits SHALL be free of charge.

#### Scenario: Delivery charges 5 credits
- **WHEN** a valid outline proposal is generated and delivered
- **THEN** exactly 5 Credits are settled against the reservation

#### Scenario: Generation failure releases reservation
- **WHEN** outline proposal generation fails or provider errors occur
- **THEN** the reservation is released and 0 Credits are deducted
