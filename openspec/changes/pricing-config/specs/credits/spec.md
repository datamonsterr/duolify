## ADDED Requirements

### Requirement: Credits read the Pricing config
Quotes, reservations, settlements, Daily free credits grants, pack purchases and Subscription grants SHALL take every amount from the active Pricing config version defined in the `pricing-config` capability, SHALL store that version id on each resulting ledger entry, and MUST NOT use any other source for prices. The catalogue named in `monetization-model` is realized by the Pricing config.

#### Scenario: Daily grant after price change
- **WHEN** a new version lowers Daily free credits and activates at midnight UTC
- **THEN** grants made after activation use the new amount, the grant entry names the new version, and earlier grants are unchanged

#### Scenario: Reservation keeps its version
- **WHEN** a version changes between reservation and settlement
- **THEN** settlement uses the version stored on the reservation
