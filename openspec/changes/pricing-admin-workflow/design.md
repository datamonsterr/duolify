# Design

Keep mutable drafts in `pricing_drafts`; approved catalogues remain in `pricing_versions`, which is immutable. A transaction locks the active catalogue and draft while classifying and approving. Copying an approved draft into `pricing_versions` makes the existing resolver consume it without a second catalogue implementation. `pricing_audit` is append-only and records actor, operation, version, safe summary, reason, request id and database timestamp.

Admin grants are database-backed and never exposed through HTTP. An environment bootstrap list provisions grants for known account UUIDs. The sole launch owner may approve an adverse draft after the configured 72-hour notice, as stated in ADR 0011; other adverse approvals require a distinct pricing administrator. A SKU below the 70% margin floor always blocks submission/approval.

HTTP authorization reads the account UUID and session UUID already verified by `accounts.Protect`, checks the database grant and recent reauthentication timestamp, and writes denied-access audit entries for all failures. CLI operations use the same pricing service and require an explicit actor UUID and reason.

Lifecycle analytics use the existing server analytics wrapper, common properties, and reserved system UUID where there is no learner context. Event properties exclude reason and pricing content.
