# Authorized lesson manifest (E3-01)

## Why
The scaffold imports slides and grants completion without a lesson test. Learners need authorized, object-backed content with an isolated rendering boundary.

## What Changes
- Add an enrollment-checked manifest and slide document API backed by Postgres references and versioned objects; fixture content replaces imports until E4.
- Validate ordered stable slide IDs, bounded durations, assets and checkpoints, projecting no answer keys.
- Fetch documents through authenticated parent requests and render in opaque-origin allow-scripts iframes with an injected asset-origin CSP and validated messages.
- Preserve E13 responsive shell, captions, panels and audio controls; final slide links to the test without granting progress.
- Register content-free lesson_started and lesson_player_error schemas before emitters.
- Cover metadata export/deletion, encrypted backups and protected object fixture restoration. No new AI behavior; existing eval/fallback gates remain unchanged.

## Impact
API/player, client/player, privacy policy, backup fixtures and inventory. Playback control/resume semantics remain E3-02; durable offline caching remains E3-03.
