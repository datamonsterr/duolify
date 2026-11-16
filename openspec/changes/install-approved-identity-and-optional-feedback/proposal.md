# Install approved identity and optional feedback (E13-06, #36)

## Why
Owner decision (ADR 0013, 2026-11-16) accepted the original geometric placeholder mark, green theme, and design tokens as the approved v1 Duolify brand (following HITL ticket E13-05). It confirmed that no UI sound assets ship for v1 (sound cues remain silent no-ops without remote fetching), while optional feedback settings for UI sounds and haptics stay. The client application needs dedicated accessible brand components (mark and wordmark), production app icons for Tauri and the web platform (favicon and manifest), and synchronized feedback settings per S23 with desktop and mobile parity, keyboard and touch support, and reduced-motion compliance.

## What Changes
- Add dedicated `BrandMark`, `BrandWordmark`, and `Brand` components implementing the approved original two-tile mark and lowercase wordmark with accessible names and token-based contrast across 16px to app-icon sizes.
- Commit canonical SVG icon sources to the repository, generate Tauri app icons for `apps/client/src-tauri/icons`, and provide a web manifest and favicon in `apps/client/public/`.
- Provide an optional feedback cue engine and platform bridge double supporting independent UI sounds and haptic toggles with conservative defaults (UI sounds off; haptics matching system preferences). Sound cues remain silent no-ops without audio assets or network requests; haptics trigger only on supported devices and respect reduced motion and OS settings.
- Align Settings (S23) across desktop panels and mobile sheets, providing accessible loading, saved, and recoverable error states.
- Emit `ui_control_activated` only for settings interaction without personal text or repeated sound triggers.

## Area impact
- **Client**: Install brand components, web favicon/manifest, Tauri app icons, feedback controller, and accessible S23 feedback toggles.
- **API**: No changes; existing authenticated `learner_profiles` settings persistence and backup dumps are reused.
- **Engine**: No changes.
- **Proto**: No changes.
- **Infra**: No changes; Git tracks icon assets and web manifest.

Relies on:
- [ADR 0013: Owner decisions for the remaining human gates](../../../docs/adr/0013-owner-decisions-2026-11-16.md)
- [ADR 0011: Owner decisions 2026-11-09](../../../docs/adr/0011-owner-decisions-2026-11-09.md)
- [ADR 0012: Interim defaults 2026-11-13](../../../docs/adr/0012-interim-defaults-2026-11-13.md)

## Data backup and retention
Approved vector assets, web manifest, and application icons are versioned and restored directly from Git history. Optional feedback settings (UI sounds and haptics) are stored in PostgreSQL table `learner_profiles` under the existing E1 persistence contract, included in nightly encrypted general backups (14-day retention), and erased during account deletion.

## Analytics
Catalogue precedes emitters. UI interactions emit allowlisted `ui_control_activated` with surface `settings` and control IDs `ui_sounds` and `haptics`. Events are gated by learner analytics consent, include UUID and common properties (platform, app_version, source), and contain no private learner text or repeated cues.

## Model reliability
No AI model calls, prompts, generation routines, or engine evaluations are modified. Existing AI golden-set and fallback gates remain unchanged.
