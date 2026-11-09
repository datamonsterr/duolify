# Design

The client requests an API authorization transaction, opens its authorization URL in the system browser, and accepts a callback only through an allowlisted redirect/deep-link bridge. The transaction binds a cryptographically random state, nonce, and PKCE verifier/challenge. The API consumes each state once, checks expiry and the exact configured redirect, exchanges the code through a provider interface, and verifies issuer, audience, nonce, signature, subject, and verified email before binding the provider subject.

Provider identity is `(provider, subject)`, unique across Learners. If the verified email already belongs to a different Learner and that provider subject is not already bound, sign-in fails with a generic linking-required response; it does not silently merge accounts. Existing email-code auth remains available to complete a future explicit linking flow.

The provider interface has Google and Apple-neutral inputs and verified claims. No live provider implementation or credential is included; E1-10 gates provisioning. The application defaults to an unavailable provider, and the deterministic Google fixture is injected only by tests. The client exposes a native browser/deep-link bridge with web and test doubles. Cancellation restores focus to the initiating control and emits only the catalogued `ui_recovery_selected` event. Successful sign-in continues through existing secure credential storage and PostHog wrappers.

Styles use the existing design tokens and match the Welcome canvas. Desktop keeps the split welcome panel; narrow screens keep the same controls as a single step. Loading, offline, provider error, cancel, keyboard focus, dark mode, and reduced-motion states are explicit.
