## ADDED Requirements

### Requirement: Daily free spend cap by allocation
The free-learner USD daily cap SHALL count the Daily free allocation of a hold or settlement and SHALL exclude the check-in allocation. Mixed funding SHALL count only its Daily free proportion.

#### Scenario: Check-in funds an outline replan
- **WHEN** a Learner has enough check-in Credits for a replan whose hold exceeds the free daily USD cap
- **THEN** the cap does not reject the check-in-funded hold
