## ADDED Requirements

### Requirement: Analytics SDK loads after first paint and consent
The client SHALL import and initialise the analytics SDK only after first paint and only after consent is resolved to opted in. No analytics network request SHALL be made before that. Events captured while the SDK is loading SHALL be queued and delivered in capture order once it is ready. Opting out SHALL drop the queue, cancel a pending load and never initialise an SDK that arrives afterwards. Autocapture and automatic pageviews SHALL remain off, and a load failure SHALL NOT block the app.

#### Scenario: Events before the SDK is ready
- **WHEN** an opted-in learner triggers an allowlisted event before the SDK has loaded
- **THEN** the event is delivered after initialisation, in order, with its original properties

#### Scenario: Opted out
- **WHEN** a learner is opted out
- **THEN** the SDK is not loaded, no analytics request is made and queued events are discarded

#### Scenario: Opt-out while loading
- **WHEN** a learner opts out after the SDK started loading but before it initialised
- **THEN** the SDK is not initialised and nothing is sent
