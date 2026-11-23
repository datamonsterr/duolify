# Cloudflare offload

## Why

Narration dominates projected AI costs. Eligible public reads can be served closer to Learners, and chat streaming spends much of its time waiting for upstream responses. Cloudflare offers free allocations for these workloads while Hetzner remains the Duolify system of record.

The owner chose Workers AI MeloTTS as the intended default on 2026-11-24. Cloudflare retired the model on 2026-11-20. The controller approved an opt-in Workers AI TTS adapter for the available Aura models and kept ElevenLabs as the default pending an owner decision. The original MeloTTS savings estimate does not apply to Aura. [Model retirement](https://developers.cloudflare.com/changelog/post/2026-11-05-planned-model-deprecations/), [Workers AI pricing](https://developers.cloudflare.com/workers-ai/platform/pricing/).

## What Changes

- **W1** Add an opt-in Workers AI Narration provider with an allowlisted configurable model, language gating, audio-derived caption timing and a free-allocation budget. Fall back to budgeted ElevenLabs and then captions. ElevenLabs remains the default and supplies entitled Plus/Pro voice choices. Update only the provider cost basis through Pricing config approval. Preserve existing Learner prices and quote semantics.
- **W2** Route eligible OpenRouter requests through an authenticated AI Gateway with direct OpenRouter fallback. Disable learner-content logging and caching, keep speech-to-text (STT) zero data retention (ZDR), and cache only allowlisted identical non-personal requests.
- **W3** Serve anonymous share previews, public Course pages, the shared demo and flag-gated Marketplace browse via an Edge Worker. Use read-only Hyperdrive over Cloudflare Tunnel if the Worker conforms to Go responses. Fall back to edge-caching eligible Go read responses. Verify Turnstile in Go on email-code request, email sign-up and share-preview access.
- **W4** Route Tutor chat and the network-backed parts of Guided chat through an authenticated Durable Object per Learner session. Use hibernating WebSocket, then server-sent events (SSE), then the existing Engine path. `CHAT_EDGE_ENABLED` defaults to `false`. Go owns credit transactions and Postgres owns chat history.

## Impact

- Capabilities: `accounts`, `ai-reliability`, `analytics`, `course-creation`, `course-sharing`, `guided-chat` (currently in `conversational-flows-and-mascot`), `lesson-player`, `marketplace`, `narration`, `pricing-config`, `tutor-chat`, `voice-input`.
- Implementation affects `engine/`, `services/api/`, `apps/client/`, and a new `edge/` area. The spec and ADR are the CF-00 deliverables. CF-01 through CF-06 cover implementation.
- Backups retain authoritative Postgres rows and versioned lesson objects. Edge caches and session coordination are disposable. PostHog runs through existing wrappers with content-free properties. Fakes, Golden set coverage and the live promotion gate cover the new AI paths.
- Cloudflare use is limited to free allocations. The owner approves any paid plan or new spend. Production cutover, deploys, E18 infrastructure migration tickets, self-hosted TTS, and Learner price changes are outside this change.
