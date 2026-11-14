## 1. Specification and tests
- [x] 1.1 Strictly validate this OpenSpec change.
- [ ] 1.2 Write failing e2e test `apps/client/e2e/narration-e14-03.spec.ts`.
- [ ] 1.3 Write failing integration test `services/api/internal/narration/synchronize_audio_captions_and_offline_playback_integration_test.go`.
- [ ] 1.4 Write failing unit tests `services/api/internal/narration/synchronize_audio_captions_and_offline_playback_test.go` and `apps/client/src/features/narration/synchronize_audio_captions_and_offline_playback.test.tsx`.

## 2. Implementation
- [ ] 2.1 Add analytics events to API allowlist and client catalogue.
- [ ] 2.2 Synchronize slide animation clock with narration audio playback, seek, speed, and replay.
- [ ] 2.3 Implement next slide audio/alignment preloading and offline playback cache with fallback.
- [ ] 2.4 Support per-device preferences persistence, captions default ON, mute, and reduced-motion static frame.
- [ ] 2.5 Update data inventory and ensure clean verification checks across api and client.
