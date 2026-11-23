# Localized copy and appearance preferences (E13-04, #34)

## Why
Shared components need stable translated messages, bidirectional layout and motion preferences that preserve learner state.

## What Changes
- Add typed Git-backed English and Arabic messages, an expanded-copy test locale, and native Intl number, plural, date and currency formatting. Native Intl avoids a new dependency for this bounded catalogue.
- Add shared system/light/dark, language and reduced-motion controls with an E1 preference adapter boundary; no new durable storage.
- Localize guided chat, summary, outline and diff controls and shell navigation without changing route IDs. Isolate names and user text direction.
- Preserve static instructional explanations and prevent nonessential motion and automatic scrolling.

## Data backup
Messages and tokens are restored together from Git. Preferences are disposable host memory until E1 supplies durable preferences. Clear/reset and host recreation restore defaults. No learner text is persisted or emitted.

## Analytics
Catalogue before emitters: ui_appearance_changed (mode: system|light|dark), ui_control_activated (surface: settings; control_id: locale|motion|reset). Common UUID, platform, app_version and source properties and both opt-outs apply. No locale strings, draft text or private content.

## AI reliability
No new AI behavior. Existing evaluation, timeout and fallback gates remain unchanged.
