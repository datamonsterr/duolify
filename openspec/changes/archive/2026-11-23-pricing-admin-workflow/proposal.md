# Pricing admin workflow

Add an audited draft-to-approval workflow for pricing changes. Pricing administrators can create and edit drafts, validate and diff them, then submit, approve or reject them through protected HTTP endpoints and the API admin CLI. Drafts remain separate from immutable approved catalogue versions. Adverse classification is computed from the stored active config, approvals follow ADR 0011, and every action has an immutable audit row.

## Why

E12-01 and E12-02 provide the launch catalogue and immutable quote snapshots. Operators need a safe path to propose future catalogue changes without editing approved financial history or deploying code.

## Scope

- Draft, edit, validate, diff, submit, approve, reject, list and show operations.
- `pricing_admin` grants, recent-auth enforcement for protected HTTP actions, reason validation, audit records and lifecycle analytics.
- Append-only financial history backed up and retained for seven years.

This change introduces no AI behavior; existing feature evaluation and fallback
gates remain in force.

Runtime activation, schedule, cancellation and rollback remain E12-06/E12-07 work.
