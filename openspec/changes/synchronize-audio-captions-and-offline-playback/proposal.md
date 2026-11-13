# Synchronize audio captions and offline playback (E14-03)

## Why
E14-01 proved single-slide synthesis and explicit narration play with synchronized captions. E14-02 added asset deduplication and recovery. However, slide animation and audio narration still operate with separate clocks, without unified play/pause/seek/replay, speed control, mute persistence, preloading next audio/alignment, or offline cached playback with graceful caption-only fallback.

## What Changes
- Unified clock: one learner-controlled media clock drives both slide Motion HTML animation and narration audio playback. Play, pause, seek, replay, and playback speeds (0.75x, 1x, 1.25x, 1.5x) affect both clocks together.
- Alignment synchronization: character alignment timestamps synchronize caption highlighting and checkpoint events. The slide holds its final frame until audio playback completes, and narration audio stops immediately when leaving the slide.
- Resilient audio states: pending, failed, disabled, or network-error audio falls back gracefully to caption-only mode while keeping the animation and lesson test playable.
- Captions and mute: captions default to ON and remain visible when muted; transcript remains readable; per-device playback settings (speed, mute, captions) persist.
- Offline preloading and caching: next slide's audio and alignment are preloaded and stored in the disposable lesson cache for offline restart.
- Reduced motion: reduced-motion preference applies static frames without losing narration audio or textual explanation.
- Analytics: add `narration_muted`, `narration_speed_changed`, and `captions_toggled` to API allowlist and client catalogue before emitters; deliver events with UUID distinct id, common properties, opt-out, and no private text.
- Data inventory: document disposable audio cache, authoritative versioned storage, and E1 preference persistence.

## Impact
- `apps/client`: LessonPlayer, NarrationControl, player cache and playback clock, analytics catalogue and emitters, responsive canvas controls.
- `services/api`: analytics allowlist in `analytics.go`, narration & player integration and unit tests.
- `docs/data-inventory.md`: updated for E14-03.
