# E13-01: Apply the green theme

## Why
The client still uses scaffold violet styles and fabricated heart/stat counters. E13-01 (#31) makes the revised 2026-11-09 design tokens the shared client foundation.

## What Changes
- Import the canonical CSS mapping directly; bundle licensed Nunito 600/800/900 WOFF2.
- Provide reusable tactile buttons, semantic badges and accessible course cards, with a real API-backed course list and lesson-path destination.
- Cover loading, empty, search, unavailable, offline and retry states at desktop/mobile widths in both themes and reduced motion.
- Keep Create and Discover visible with unavailable explanations until their owning capabilities ship. No new creation, marketplace, shell feature, AI or pricing behavior.

## Data and backup
Sources and font license/assets are restored from Git. Course/search state and analytics context are ephemeral; no new learner persistence or backup/deletion requirements. Update the data inventory. Existing API backup rules remain unchanged.

## Analytics
The app-design catalog already defines `ui_control_activated(surface, control_id)`. This slice emits only surface `courses` and stable controls `open_course`, `refresh_courses`, `retry_courses`, `clear_search`. It uses the shared analytics adapter only with an enabled, authenticated UUID context; E1-06 supplies that context when available. Send common platform/app_version/source properties and course_id only when known. Never send titles, descriptions, search strings, error text or URLs. Opt-out and transport failure must not block controls.

## Model reliability
No AI behavior, prompts, provider calls or generated fixtures change; existing evaluation and fallback gates remain untouched.
