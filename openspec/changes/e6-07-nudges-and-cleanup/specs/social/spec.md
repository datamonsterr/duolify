## ADDED Requirements

### Requirement: Social cleanup on deletion and restore
Deleting a Learner account SHALL remove their friendships, requests, friend links, nudges and shared streaks in the same operation, and the surviving friend MUST see no trace of the erased identity. The released Username MUST stay held for the configured hold. Replaying deletion tombstones after a restore MUST give the same result without breaking Username uniqueness or other active holds.

#### Scenario: Survivor view
- **WHEN** a connected Learner's account is deleted
- **THEN** the other Learner's Friend list, inbox and streak show nothing of them

#### Scenario: Hold survives restore
- **WHEN** a restored database replays a deletion tombstone
- **THEN** the Username is held and no other Learner can claim it before the hold ends
