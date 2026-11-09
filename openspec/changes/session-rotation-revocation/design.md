# Design

An auth_sessions row is a family of SHA256 refresh hashes. Postgres row locking serializes consumption; only one concurrent refresh succeeds and reuse revokes that family, including its access JWTs. Protect consults Postgres for every request; there is no Redis authorization cache to invalidate. Listing returns only active owned session IDs, creation/expiry timestamps and a current-device flag, never tokens, email, IP or user-agent text. DELETE one owned UUID or all owned sessions revokes immediately. Missing/foreign IDs return 404 identically.

The client shares a single refresh promise across protected requests and retries each request at most once. A generation counter prevents an outstanding read/refresh from restoring credentials after logout or a different sign-in. Offline errors keep credentials and drafts for Retry. Revocation success removes credentials from the existing SecureStore. Revalidation on navigation and window focus detects remote revocation.

Expired-session recovery keeps the feature tree mounted but hidden and inert, showing S01 email sign-in in place. Same-learner reauthentication preserves drafts; different-learner reauthentication remounts them. Return paths use the E1-02 allowlist, extended with /settings/sessions. No draft content is persisted to browser storage or sent to analytics. Recovery selection uses the last verified UUID and its existing consent, and explicit logout clears identity.

The sessions screen is a separate settings step at /settings/sessions on mobile and a bounded account panel on desktop, with date-based device labels, current-device indicator, keyboard controls, loading/empty/error feedback and retry. No untrusted device text is collected. Nightly dumps already cover session families and hashes; extend restore checks to prove consumed/revoked families remain unusable and account deletion cascades. No new AI behavior; existing eval/fallback gates remain unchanged.

Dependencies checked against official PostgreSQL locking docs, React external-store docs, MDN Fetch docs and Playwright assertions before implementation.
