## ADDED Requirements

### Requirement: Shared demo public read at the edge
The Edge Worker MAY serve and cache the shared demo using the same authored, versioned fixture, object references and checksum-based restore process as the Go shared demo endpoint. It SHALL preserve existing public reads, analytics consent and transitions into personalized creation. The route MUST use the existing configured freshness bound and fall back to Go when the edge path fails. The authored shared demo MUST stay free and require no inference.

#### Scenario: Read shared demo at the edge
- **WHEN** a Learner opens the shared demo through the Edge Worker or the Go fallback
- **THEN** the same version and preview assets load with zero generative work, reservation or charge
