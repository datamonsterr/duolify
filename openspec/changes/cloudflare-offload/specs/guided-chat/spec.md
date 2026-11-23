## Purpose

Preserve Guided chat's authored scripts, offline behavior and existing consent rules when its network-backed turns can use an Edge transport. Extends the `guided-chat` capability defined by the `conversational-flows-and-mascot` change.

## ADDED Requirements

### Requirement: Guided chat edge transport
When `CHAT_EDGE_ENABLED=true`, network-backed Guided chat turns SHALL use the authenticated per-session Edge WebSocket path, with SSE and existing Go/Engine fallbacks. Local scripted steps, deterministic FAQ and commands SHALL stay available with existing draft and offline behavior. The Edge path MUST protect secure step values, honor existing rate limits and guardrails, and require explicit Action quote consent for paid detours. The Go API and Postgres remain authoritative for server-side drafts, applicable chat history, credits, export, expiry and erasure. Classifier failure MUST retain the deterministic fallback in the Guided chat capability.

#### Scenario: Classifier cannot reply over Edge transports
- **WHEN** the WebSocket and SSE paths fail and the existing classifier is unavailable
- **THEN** the deterministic Guided chat fallback offers the authored choices, preserves the draft and incurs no charge

#### Scenario: Network-backed quoted detour
- **WHEN** the Learner accepts a quote for a paid question and the edge transport reconnects
- **THEN** Go resolves one canonical turn and settlement, and the script returns to the same step afterward
