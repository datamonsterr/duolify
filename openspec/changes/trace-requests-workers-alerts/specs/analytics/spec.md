## ADDED Requirements

### Requirement: Independent operational signals
Operational request, RPC and job counts, durations, errors and alert counts SHALL
be exposed as fixed-label metrics independent of PostHog and analytics consent.
No new learner analytics event SHALL be introduced. The existing backup_failed
catalogue event SHALL retain its UUID, common properties and safe numeric outcome
properties. Analytics adapters MUST enqueue without blocking and MUST discard
uncatalogued or private properties. Learner consent checks SHALL remain enforced.

#### Scenario: Analytics outage and opt-out
- **WHEN** PostHog fails or global analytics opt-out is enabled
- **THEN** requests and jobs continue and operational alerts still increment

#### Scenario: Private event delivery
- **WHEN** backup_failed reaches the PostHog test double
- **THEN** it contains the reserved UUID, common properties and outcome fields only

### Requirement: Compatible AI telemetry metadata
The analytics adapter SHALL retain the existing setup-chat provider, model,
input_tokens, output_tokens, latency_ms and task compatibility properties and
SHALL also normalize them to the canonical $ai_generation catalogue fields.
No prompt or completion SHALL be captured. This changes no model behavior.

#### Scenario: Existing setup chat analytics
- **WHEN** the existing setup-chat emitter reports numeric usage metadata
- **THEN** canonical properties and compatibility aliases contain only usage and fixed identifiers
