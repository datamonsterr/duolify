## ADDED Requirements

### Requirement: Canonical lifecycle reconciliation
The API SHALL append verified canonical provider status and periods, never trust webhook snapshots, and derive active, cancel-scheduled, past-due, expired, refunded and disputed views from this history and funded terms. Dunning SHALL preserve access only until verified paid-through, never issue unfunded allowances, and cancellation resumption SHALL replace the derived cancellation view without rewriting history. Refunded/disputed terms SHALL stop entitlements and future grants while preserving issued balances pending E7-10 allocation rules.

#### Scenario: Reordered failure and cancellation
- **WHEN** stale failure/cancellation notifications arrive after a paid renewal
- **THEN** canonical active state wins, the renewal grants once and no paid term is removed

### Requirement: Provider transition boundary
A shared transition interface SHALL define provider-specific effective timing. Stripe SHALL schedule a new configured plan at next renewal with both update and future phase proration disabled. A schedule SHALL grant nothing; only a matching verified paid invoice SHALL activate its pinned terms. Native adapters remain gated by E7-12/E7-13. Live Stripe remains gated by E7-05.

#### Scenario: Scheduled downgrade
- **WHEN** a funded subscriber requests a configured downgrade
- **THEN** the current benefits and balance remain until renewal, the effective date is shown and only paid renewal activates the new allowance

### Requirement: Funded generation scheduling
Claims SHALL prefer reserved generation work with the configured priority benefit, using verified non-revoked funded periods at claim time. Concurrency SHALL use the funded term's versioned limit and fall back to the quote's ordinary configured limit on expiry. Priority SHALL never bypass provider or USD limits.

#### Scenario: Premium job at cap
- **WHEN** a premium job exceeds the reserved operational budget
- **THEN** it is refused without provider execution or customer charge

### Requirement: Lifecycle audit and privacy
Lifecycle and transition history SHALL enter encrypted nightly financial dumps with 35-day rotation and seven-year retention, account export and order-linked identity erasure. Restore SHALL preserve effective periods, dedupe and cancellation resumption. The analytics catalogue SHALL define subscription_renewed, subscription_cancelled, subscription_payment_failed and entitlement_changed before emitters; financial outbox occurrences SHALL be deduplicated by business change without learner text and inherit UUID identity and opt-out.

#### Scenario: Restored history
- **WHEN** financial history is restored and duplicate events replay
- **THEN** status and periods remain reconstructible and grants/events are not duplicated
