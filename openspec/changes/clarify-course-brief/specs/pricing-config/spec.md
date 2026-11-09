## ADDED Requirements

### Requirement: Development course brief chat funding
The Pricing config SHALL define course_brief_chat as platform-paid with zero learner Credits in immutable unapproved development version usd-dev-course-brief-unapproved, succeeding usd-dev-unapproved without mutation. Its numeric envelope SHALL contain token, attempt, deadline, USD and rate ceilings. The API MUST enforce learner and global platform throttles shared across replicas, bounded by the platform USD/day cap. Development MUST remain independent of launch sales approval. Production MUST require an approved Pricing version and explicit live promotion; form confirmation remains usable while chat is disabled. No wallet reservation or deduction SHALL occur during clarification.

#### Scenario: Zero-balance development learner
- **WHEN** a Learner with no Credits clarifies a brief under the development config
- **THEN** the model may be called within platform limits and no ledger entry is created

#### Scenario: Unapproved production config
- **WHEN** production selects the unapproved development version
- **THEN** assistant calls are refused and the form stays available
