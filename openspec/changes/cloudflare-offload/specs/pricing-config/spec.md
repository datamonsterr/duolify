## ADDED Requirements

### Requirement: Approved narration provider cost basis
OpenRouter and ElevenLabs model cost assumptions and worst-case bounded fallback cost MUST be validated through the existing versioned Pricing config approval workflow. The change SHALL be limited to provider cost basis and leave Learner-facing credit prices, quote settlement rates, packs, plans and voice entitlements intact. An accepted quote SHALL retain its original Pricing version and cover the actual authorized route and attempt envelope. A cache hit MUST remain free.

#### Scenario: Cost-only approval
- **WHEN** a pricing administrator approves and activates a valid narration provider cost basis
- **THEN** new quotes use the approved cost envelope, existing quotes retain their version and Learner Credits for the same delivered narration are unchanged
