## ADDED Requirements

### Requirement: Partial streaming usage and tutor failure cases
A chat stream that ends or fails before its final usage frame MUST still yield one usage record: provider-reported when present, otherwise estimated from delivered bytes, bounded by the quote limits and flagged `estimated`. Golden-set `tutor_chat` cases SHALL cover first-token timeout with fallback, first-token failure with no fallback, idle timeout, cancel and mid-stream failure with critical checks (no second provider after a delta, usage within the quote, typed error code).

#### Scenario: Stream ends without usage frame
- **WHEN** the provider closes after deltas without a usage frame
- **THEN** the Engine emits one estimated usage frame no larger than the quote bounds
