## Implementation

- [ ] Validate change and consult official dependency documentation.
- [ ] Add failing browser, Postgres integration, and unit tests in order.
- [ ] Implement idempotent configured XP consumer, learner-local goals and authenticated endpoints.
- [ ] Replace static completion XP and show configurable daily progress with recovery states.
- [ ] Extend analytics catalogue, privacy policy, backup fixtures and data inventory.
- [ ] Run focused web/API checks serially for database/browser suites and commit.

## E5-02 streak days

- [x] Failing e2e, integration and unit tests, then streak projection, rollover worker and endpoint.
- [x] S13 streak panel, analytics catalogue, privacy policy, backup fixtures and data inventory.

## E5-03 streak freezes

- [x] Failing e2e, integration and unit tests, then milestone grants, cap overflow and automatic rollover protection.
- [x] S13 inventory, earned and covered-day notices; analytics catalogue, privacy policy, backup fixtures and data inventory.

## E5-04 weekly leagues

- [x] Failing e2e, integration and unit tests, then membership assignment, ranking, Redis rebuild and endpoint.
- [x] S13 Leagues view, `ui_screen_viewed(leagues)` catalogue entry, privacy, backup fixtures and data inventory.


## E5-05 league finalization and restore

- [x] Failing e2e, integration and unit tests, then `league_week_results`, idempotent finalization, early-join tier correction and `last_week` on the league endpoint.
- [x] S13 Last week result, `league_promoted`/`league_demoted` catalogue entries, privacy, backup fixture and data inventory.

## E5-06 paid learning analytics and history exports

- [x] Failing e2e, integration and unit tests, then streaming history export, analytics view and custom allowlist export.
- [x] S13/S23 learning analytics and export UI, analytics catalogue, data inventory, ADR 0011 entry and verified entitlement enforcement.

