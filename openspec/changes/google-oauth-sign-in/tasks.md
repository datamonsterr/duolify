## Tests (TDD order)

- [ ] Add Playwright complete/cancel Google browser sign-in coverage for desktop and mobile, including keyboard/touch, focus restoration, offline/error recovery, and safe redirects.
- [ ] Add Postgres integration coverage for transaction replay/expiry, PKCE and nonce mismatch, exact redirect enforcement, verified-email collision, unique subject binding, session creation, dump/restore, and deletion.
- [ ] Add API and client unit tests for provider claim validation, boundary rejection, native bridge doubles, and privacy-safe analytics.

## Implementation

- [ ] Add a strict OpenSpec rule set and provider-neutral API adapter interface with a Google fixture and a provisioned provider boundary left to E1-10.
- [ ] Add short-lived OAuth transaction state and provider-subject binding persistence; use a migration after `20261109131401` if required.
- [ ] Integrate the existing auth session issuer without changing E1-03 account route dispatch.
- [ ] Add the Google entry point to the responsive Welcome sign-in surface using design tokens and recovery states.
- [ ] Add catalogued analytics events before emitters, routed through existing PostHog wrappers; assert UUID/common properties, opt-out, and no private data.
- [ ] Update `.env.example`, backup fixtures, and `docs/data-inventory.md`; no secrets or learner content in OAuth events.
- [ ] Run `mise run ci:openspec`, then the focused API/client mise gates. AI behavior is unchanged.
