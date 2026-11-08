## MODIFIED Requirements

### Requirement: Streak freeze interaction
A Friend streak SHALL be preserved on a missed day by the Streak freeze of the friend who missed. Each Learner's own freeze protects their own side only: a freeze MUST NOT be spent from, or counted against, the other friend. The freeze MUST be applied automatically at rollover, as for a Streak. A freeze used for a Friend streak MUST count as that friend's freeze consumption and MUST also preserve that friend's own Streak. If both friends miss the same day, each side MUST be evaluated with its own freeze, and the Friend streak breaks unless both sides are protected.

#### Scenario: Freeze saves Friend streak
- **WHEN** one friend misses a day and owns a freeze
- **THEN** that friend's freeze is consumed and the Friend streak is unchanged

#### Scenario: Friend without freeze misses
- **WHEN** one friend misses a day and has no freeze, while the other friend owns one
- **THEN** the Friend streak breaks and the other friend's freeze is not consumed

#### Scenario: Both miss
- **WHEN** both friends miss the same day and only one owns a freeze
- **THEN** the Friend streak breaks

## ADDED Requirements

### Requirement: Friend streak milestone freezes
Reaching a Friend streak milestone in config SHALL grant each Learner one Streak freeze, once per milestone, when below the freeze cap. The grant MUST NOT include Credits.

#### Scenario: Milestone reached
- **WHEN** a Friend streak reaches a milestone
- **THEN** each Learner below the cap receives one freeze once and `streak_freeze_earned` is emitted with reason `friend_streak`

## REMOVED Requirements

### Requirement: Friend streak rewards
**Reason**: Credit rewards for effort are removed (ADR 0007); freezes replace them.
**Migration**: Remove the `reward` ledger writes and the `reward_credits_granted` event from friend streak milestones; keep the freeze grant.
