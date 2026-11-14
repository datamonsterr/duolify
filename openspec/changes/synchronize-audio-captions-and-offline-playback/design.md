## Decisions

- **Single Master Clock**: The parent `PlaybackClock` in `LessonPlayer` / `SlidePlayback` owns the playback time for both the slide iframe and the narration audio element. Audio `currentTime` is kept in sync with the clock during ticks, seeks, and replay. When audio is ready, the total duration for the slide is `Math.max(slide.duration_ms, asset.duration_ms)` so the slide holds its final frame until audio finishes.
- **Speeds**: Speeds `0.75`, `1`, `1.25`, and `1.5` are supported. Changing the speed updates both `PlaybackClock.rate` (driving the slide animation message protocol) and `audio.playbackRate`.
- **Exit & Cleanup**: Leaving the slide (via Next, Previous, Exit lesson, or unmount) stops the audio immediately (`audio.pause(); audio.currentTime = 0;`) and cleans up object URLs.
- **Preload & Offline Cache**: When a slide is loaded or while playing slide N, slide N+1's audio blob and alignment JSON are preloaded and stored in the disposable cache alongside slide HTML and assets. When offline, cached audio and alignment are materialized and played. If audio fails or is uncached offline, the player falls back to caption-only playback without breaking the slide animation.
- **Preference Persistence**: Per-device playback settings (speed, mute, captions visibility) are saved to local storage so they persist across sessions and offline restarts, with captions defaulting to true.
- **Analytics Catalogue Precedence**: `narration_muted`, `narration_speed_changed`, and `captions_toggled` are added to the API allowlist in `services/api/internal/analytics/analytics.go` and client catalogue in `apps/client/src/lib/analytics.ts` before emitting.
- **Design Board & Canvas Fidelity**: Desktop controls and mobile sheets match `S09-Player.dc.html` and `D2-Player.dc.html`, supporting touch and keyboard shortcuts (`Space`, `ArrowLeft`, `ArrowRight`).

## Risks

- **Audio Drift / Desync**: Small drift between requestAnimationFrame and HTMLAudioElement is prevented by anchoring the clock to audio when audio is playing, or syncing `currentTime` on seek and rate changes.
- **Quota Exceeded in Cache**: Audio blobs are bounded and stored in the disposable cache; cache write errors fall back gracefully to streaming or caption-only mode without blocking lesson progression.
