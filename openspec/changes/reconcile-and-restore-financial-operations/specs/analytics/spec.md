## ADDED Requirements

### Requirement: Payment reconciliation operations analytics
The API SHALL emit payment_reconciliation_failed with channel stripe and a closed
reason_code (provider_unavailable, invalid_evidence, dead_letter, or mismatch).
Operational failures SHALL use system UUID 00000000-0000-4000-8000-000000000011,
source api, platform web and build app_version. Existing restore_drill_completed
SHALL report the financial replay outcome through ok. Existing wrappers MUST
honor global opt-out. No learner text, provider identifiers, payloads or errors
SHALL appear in event properties. Alerts MUST work independently of analytics.

#### Scenario: Analytics disabled
- **WHEN** reconciliation fails with analytics opted out
- **THEN** the operational alert remains available and no PostHog event is delivered
