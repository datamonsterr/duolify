## ADDED Requirements

### Requirement: Optional per-length lesson price
The Pricing config SHALL accept an optional `lesson_content_by_length` object with `short`, `medium` and `long` Credit prices. A missing object or key SHALL use the `lesson_content` price. Introducing or raising a per-length price is an adverse change and follows the two-person approval rule. Quotes and ledger entries for a Lesson SHALL use the price for the Course lesson length at the quoted version.

#### Scenario: No per-length prices
- **WHEN** the active config has no `lesson_content_by_length`
- **THEN** every length is quoted at the `lesson_content` price

#### Scenario: Raise long price
- **WHEN** an admin drafts a higher `long` price
- **THEN** activation needs the second approval and the adverse notice period
