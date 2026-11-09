## ADDED Requirements

### Requirement: Audited pricing draft workflow
Pricing administrators SHALL create, edit, validate, diff, submit, approve, reject, list and show pricing changes through protected HTTP endpoints and the API admin CLI. Every operation SHALL require a non-empty reason and append an audit record. Denied HTTP access SHALL also be audited. Approved pricing versions and audit rows SHALL be immutable at the database boundary.

#### Scenario: Draft and approve an adverse change
- **WHEN** an authorized administrator drafts and submits a valid price increase and an eligible approver approves it
- **THEN** a new immutable approved version is created and the audit log identifies the actor, reason, diff summary and timestamps

#### Scenario: Invalid or unauthorized operation
- **WHEN** a draft is invalid or an actor lacks `pricing_admin`
- **THEN** the operation is refused, no approved version changes, and the failure is auditable

### Requirement: Adverse classification and approval
The API SHALL compute adverse status against the currently approved pricing content. An adverse change SHALL require at least 72 hours between approval and `effective_from`, and shall be rejected if any SKU is below 70% margin. The sole launch owner MAY approve an adverse draft after the notice window under ADR 0011; otherwise the author SHALL NOT approve their own adverse draft. Non-adverse drafts MAY be approved by their author.

#### Scenario: Margin floor
- **WHEN** any submitted SKU has margin below 70%
- **THEN** validation or approval is refused regardless of actor or notice

### Requirement: Pricing lifecycle analytics
The API SHALL emit `pricing_version_created`, `pricing_version_approved` and `pricing_version_rejected` with UUID identity and common properties. Events MUST NOT include reason text or private pricing content, and opt-out SHALL suppress delivery.

#### Scenario: Lifecycle event privacy
- **WHEN** a pricing draft is created, approved or rejected
- **THEN** the corresponding catalogued event is delivered through the existing analytics wrapper without reason text

### Requirement: Pricing draft financial retention
Pricing drafts, approved versions and audit entries SHALL be included in nightly financial backups and retained for at least seven years. Deleting an administrator account SHALL pseudonymize its actor references without deleting audit history.

#### Scenario: Restore and actor deletion
- **WHEN** a financial backup is restored or an admin account is deleted
- **THEN** pricing history remains intact and deleted actor references are pseudonymized
