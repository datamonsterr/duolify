# Design

Postgres remains the pricing source of truth. Public pricing reads resolve the greatest approved, non-cancelled `effective_from <= now()` and the nearest future approved, non-cancelled version. The query is evaluated against database time at each read; Redis is disposable and can be bypassed when unavailable. Runtime parse/query failures retain the last validated catalogue and log an alert.

Approved `pricing_versions` remain immutable. A cancellation row hides a not-yet-effective schedule while retaining its original version. An activation marker keyed by version prevents duplicate activation event emission across concurrent readers. Scheduling is a reviewed approval action and uses a unique effective instant; adverse versions still require the ADR 0011 notice and approver rules. Rollback inserts a new mutable draft with the active version as parent and the historical source as explicit ancestry, then uses the existing submit/approve workflow.

Quotes continue to reference their original version. Version, cancellation, activation, draft ancestry and audit records stay in full financial backups and are retained at least seven years.
