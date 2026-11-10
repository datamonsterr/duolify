## ADDED Requirements

### Requirement: Durable generation settlement
The API MUST persist validated generated lessons and settle delivered Credits in the same transaction. A private durable checkpoint MUST permit worker retries after persistence failure without another provider call. Terminal generation failures MUST release customer Credits using the owning learner and MUST retain cleanup intent when financial persistence fails. Checkpoints MUST join full-row backups, owner export and cascading deletion, and MUST be cleared on terminal state.

#### Scenario: Storage fails after provider success
- **WHEN** content persistence fails after a validated generation
- **THEN** no customer charge is committed and a worker retry commits the saved content and settlement atomically without another provider call

#### Scenario: Financial cleanup fails
- **WHEN** a failed generation cannot release its reservation because the ledger is unavailable
- **THEN** the worker returns an error and retries durable cleanup before marking the generation terminal

### Requirement: Preparation requires an unused eligible lesson
Preparation MUST require ready_to_generate, enforce one active generation per lesson in Postgres and enqueue the River job in the same transaction as generation creation and stub state.

#### Scenario: Current lesson preparation
- **WHEN** a learner tries to prepare an already current lesson
- **THEN** the API rejects the request without reserving credits or rewriting content

#### Scenario: Queue unavailable
- **WHEN** River enqueue fails
- **THEN** generation creation and the lesson state change roll back together

### Requirement: Private recap delivery
Recaps MUST filter slides by both lesson and authenticated learner.

#### Scenario: Other learner requests a recap
- **WHEN** a learner requests another learner's lesson topic recap
- **THEN** no personalized slide content is returned

### Requirement: Real bounded paraphrases
The Engine MUST use its real provider path for retry paraphrases, invoke the core variant validators and allow at most one repair within the original quote's remaining attempt, USD and deadline bounds. Missing topic IDs MUST be rejected rather than defaulted. Cosmetic prefixes and identical source prompts MUST NOT be accepted as paraphrases. Approved exhausted pools MAY be reused reordered for free. An unavailable initial pool MUST reject generation; a missing or corrupt stored pool MUST fail safely without consuming the answer, charging, auto-passing or echoing its source prompt.

#### Scenario: Invalid paraphrase twice
- **WHEN** both primary and repair produce invalid paraphrases
- **THEN** generation falls back only to an approved pool or returns unavailable with no delivered customer charge

#### Scenario: Golden reliability gate
- **WHEN** offline golden cases execute the production paraphrase processor with a mock provider
- **THEN** E10 critical checks enforce topic linkage, unchanged correct-answer meaning, different prompt wording and the two-attempt ceiling

#### Scenario: Safe analytics
- **WHEN** generation or retry events are emitted
- **THEN** only allowlisted event metadata is sent and no prompts, learner text or checkpoint content enters analytics
