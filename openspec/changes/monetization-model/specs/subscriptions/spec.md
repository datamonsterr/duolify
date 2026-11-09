## ADDED Requirements

### Requirement: Versioned subscription catalogue
The API SHALL expose versioned server-owned plan configuration with currency, channel/territory, external product/price IDs, monthly and yearly minor-unit prices, monthly Credit allowance, entitlement version and effective dates. Initial individual plans SHALL be Learn at USD 9.99/month or USD 96/year with 600 Credits/service month, Plus at USD 19.99/month or USD 192/year with 1,600, and Pro at USD 39.99/month or USD 384/year with 3,600. Store-localized products MUST display their verified native prices and terms. Catalogue changes MUST NOT rewrite paid service terms, prior grants or ledger entries; future renewal changes MUST follow provider and legal notice/consent rules.

#### Scenario: Annual offer shown
- **WHEN** a Learner views an annual Plus offer
- **THEN** the client discloses the full annual price, monthly equivalent, 1,600 monthly Credits, non-expiring issued balance, billing channel, taxes and renewal/cancellation terms

#### Scenario: Retired plan version
- **WHEN** a catalogue version is retired for new purchases during an existing paid term
- **THEN** the Learner's funded entitlements and scheduled allowance remain explainable and honored under the preserved version

### Requirement: Plan entitlements
Learn SHALL include offline library downloads, no upsell banners and learning-history export. Plus SHALL include those benefits plus priority generation, advanced learning analytics and curated voices without a separate voice-license surcharge. Pro SHALL include Plus benefits plus custom analytics export and a higher configured generation concurrency limit. Entitlements MUST be derived from verified paid state in Postgres, versioned, synchronized across authorized devices and cached only as derived data. Universal replay of already generated Lessons MUST remain unmetered. Plans MUST NOT issue Credits for effort or grant paid Streak freezes; Streak freezes SHALL remain effort-earned and auto-applied. Curated voice synthesis MUST still consume the disclosed narration Credits.

#### Scenario: Paid access on another device
- **WHEN** an authenticated Learner signs in on another supported device during a funded Plus term
- **THEN** the API exposes the same verified entitlement version and existing balance without issuing another allowance

#### Scenario: Voice selection
- **WHEN** a Plus Learner selects a different curated voice for existing narration
- **THEN** the client discloses any new synthesis quote before generation, and replaying the already generated voice remains free of AI charges

### Requirement: Verified subscription activation
Subscription activation MUST require a verified provider-funded service period. Stripe Checkout MAY link the provider subscription to the authenticated Learner, but the API MUST activate and append `subscription_grant` entries only after verifying a paid invoice and expected plan, amount, currency, environment and period. Native activation MUST verify the store transaction/purchase token and account binding server-side. The event inbox, subscription state, entitlement update, first ledger grant and financial outbox MUST commit atomically using unique provider event and business grant keys. Unpaid checkouts, trials and client receipts alone MUST NOT authorize a grant. Launch SHALL have no automatic free trial.

#### Scenario: Checkout and invoice events both arrive
- **WHEN** valid subscription checkout and paid invoice events describe the same service month
- **THEN** one funded activation and one allowance grant occur regardless of arrival order

#### Scenario: Receipt reused by another account
- **WHEN** a transaction already bound to one Learner is submitted by another
- **THEN** the API rejects the attempted binding and does not duplicate entitlements or Credits

### Requirement: Monthly allowance and annual scheduling
Every funded subscription SHALL grant its configured monthly allowance once per service month. Annual subscriptions MUST grant monthly rather than issuing the whole annual balance at purchase. A durable schedule MUST use a unique key comprising provider, subscription, funded term and service-month index; month-end anniversaries MUST clamp to the last valid date. Grant jobs MUST verify the funded term, remain within paid-through bounds and catch up missed funded months idempotently. Cancelling auto-renew MUST NOT cancel remaining grants within an already paid annual term. A provider-confirmed refund or chargeback MUST cancel affected future grants. Already issued paid Credits MUST roll over without expiry after cancellation, downgrade or term end.

#### Scenario: Worker restarts after an anniversary
- **WHEN** an annual subscription grant worker restarts after missing a funded monthly anniversary
- **THEN** it issues that month's allowance once and replaying the job issues no duplicate

#### Scenario: Annual cancellation
- **WHEN** an annual Learner turns off auto-renew midterm without a refund
- **THEN** funded monthly grants and benefits continue through the paid-through date and issued Credits remain afterward

#### Scenario: Annual term refunded
- **WHEN** the provider confirms a refund revoking an annual funded term
- **THEN** future grants for the revoked period stop and attributable issued Credits follow the signed reversal policy

### Requirement: Renewal and plan lifecycle
The API MUST represent active, cancellation-scheduled, past-due, expired, refunded and disputed state with authoritative provider periods. It MUST preserve confirmed paid-through access during payment retries but MUST NOT grant unfunded Credits. Stripe plan changes SHALL initially take effect at renewal without interim proration. Native store changes MUST honor provider-mandated upgrade/proration timing and grant only the verified net newly purchased allowance using versioned mapping. Downgrades and cancellation MUST NOT remove unrelated paid balances or previously delivered content. The client MUST provide channel-correct management/cancellation links, display effective dates and prevent a new overlapping subscription when a verified active subscription exists.

#### Scenario: Failed renewal
- **WHEN** renewal payment fails after the previous paid-through date
- **THEN** no new allowance is granted, paid benefits expire according to verified state and previously issued Credits remain usable

#### Scenario: Stale cancellation event
- **WHEN** a cancellation notification arrives after a later verified renewal
- **THEN** canonical provider state is reconciled before changing entitlements and the stale event cannot erase the paid term

#### Scenario: Store immediate upgrade
- **WHEN** a store verifies an immediate prorated upgrade
- **THEN** entitlement timing follows that verified period and the grant uses only the reconciled net allowance without duplicate grants for overlapping periods

### Requirement: Store billing and purchase restore policy
The system MUST maintain an effective-dated platform, storefront-region and program eligibility matrix for digital subscription and Credit purchases. Store-distributed builds MUST default to native billing and MUST NOT display an external purchase link unless the configured exception's enrollment, disclosures and eligibility are satisfied. Fee classes MUST distinguish subscription/pack, merchant eligibility and regional/install cohorts. Purchases on all channels MUST resolve to one Learner ledger and verified entitlement view. Restore MUST reconcile existing provider transactions and local business keys without reissuing consumed Credits or treating client state as authoritative. Paid Credits MUST NOT expire, including Apple-purchased currency. Accidental cross-channel duplicate purchases MUST be reconciled without double allowances and with a channel-correct refund path.

#### Scenario: Unknown region policy
- **WHEN** the platform/region/program combination has no verified external-purchase policy
- **THEN** the app offers only permitted native billing or disables new purchases until configured

#### Scenario: Reinstall and restore
- **WHEN** a Learner restores an existing subscription after reinstalling
- **THEN** verified funded access and current ledger balance are recovered without replaying already issued grants

### Requirement: Subscription refunds and disputes
The client MUST disclose refund request and channel ownership before purchase. Web policy SHALL allow requests for the unused paid service/credit portion, subject to mandatory law and recorded operator decisions; stores SHALL use their provider refund process. Verified full or partial refunds and chargebacks MUST record monetary allocation, attributable signed ledger reversals and entitlement/future-grant changes atomically with a unique reversal key. They MUST preserve original financial entries and unrelated paid Credits. Forced reversals exceeding attributable unspent Credits MUST record a source deficit and block new paid AI pending resolution rather than deleting past consumption. Repeated provider notifications MUST NOT apply another reversal or entitlement reduction. Legally required refunds MUST override discretionary commercial policy.

#### Scenario: Partial refund preserves other purchase
- **WHEN** a partial subscription refund is confirmed while the Learner also holds pack Credits
- **THEN** only the attributable subscription balance/term is adjusted and the pack remains intact

#### Scenario: Chargeback after consumption
- **WHEN** a charged-back subscription grant was already consumed
- **THEN** consumption remains in the ledger, source debt and revoked future grants are recorded once and unrelated issued balances are retained

### Requirement: Subscription analytics events
The system SHALL emit `plan_purchased` with plan_id, interval, channel, amount_minor, currency and billing_period_start/end; `subscription_renewed` and `subscription_cancelled` with plan_id, interval, channel and effective period; `subscription_payment_failed` with plan_id, channel and reason_code; `subscription_grant_issued` with plan_id, amount and period_key; and `entitlement_changed` with plan_id and entitlement_version. It MUST use the Credits monetization paywall/checkout/refund/reconciliation events for the corresponding funnel stages. Events MUST extend the canonical analytics catalogue before implementation and inherit its UUID identity, common properties, privacy, opt-out and non-blocking delivery rules. Financial event creation MUST be deduplicated in the same transaction as the underlying change; events MUST NOT contain card data or learner content.

#### Scenario: Renewed once
- **WHEN** duplicate provider notifications reconcile the same funded renewal
- **THEN** one grant and one financial outbox occurrence of each renewal/grant event are recorded

#### Scenario: Opted-out subscriber
- **WHEN** an opted-out Learner purchases a plan
- **THEN** verified fulfillment succeeds without sending PostHog events and the financial audit remains available in Postgres

### Requirement: Subscription data backup and retention
Postgres MUST store subscription and entitlement history, funded monthly grant schedules, invoice/payment references, refund allocations, dedupe keys and price/entitlement versions. Financial records MUST be retained at least seven years or longer where required by law, encrypted in nightly backups with 35-day snapshot rotation and PITR where available. Raw redacted webhook payloads SHALL be retained for 30 days and derived analytics for 90 days. Account deletion MUST remove learner content and pseudonymize financial identity except required fields. Quarterly restore drills MUST reconcile provider-funded periods, ledger-derived balances, outstanding-credit liability, entitlements and grant uniqueness; deletion tombstones MUST be applied before serving restored data.

#### Scenario: Annual schedule restored
- **WHEN** a backup containing an annual plan schedule is restored
- **THEN** missed funded grants can be caught up once, refunded terms cannot grant again and entitlements match verified paid-through state

### Requirement: Entitlement and AI cost reliability
Billing workers MUST use bounded retries, durable dead-letter replay and daily provider reconciliation. Payment, analytics or cache outages MUST NOT fabricate entitlement extensions or duplicate grants. Plan benefits MUST NOT bypass configured provider budgets, quotes or evaluated model/voice selection. Changes affecting AI allowances or routes MUST use pinned Golden set usage/context/fallback/narration cases, pass the configured evaluation thresholds and focused mock/live evaluation tasks, and preserve the Credits cost limits. Catalogue activation MUST verify current provider model IDs and rates, including valid OpenRouter fallback aliases. Previously issued balance and paid service terms MUST remain reconstructible through catalogue rollbacks.

#### Scenario: Catalogue rollback
- **WHEN** operators roll back a bad new price table
- **THEN** new purchases use the restored active version while accepted reservations, paid terms and prior grants retain their original versions

#### Scenario: Budget-limited premium generation
- **WHEN** a Pro priority job would exceed the Learner or platform provider-spend cap
- **THEN** priority does not bypass the cap, work is queued or refused transparently and undelivered work is not charged
