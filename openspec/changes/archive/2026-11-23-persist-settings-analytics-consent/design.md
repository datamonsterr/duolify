# Settings persistence and consent

Reuse `learner_profiles` for settings and consent. Keep current profile fields and the `analytics_opt_out` bit; add validated appearance, reduced-motion, narration, narration-speed, captions, text-size, UI-sounds and haptics fields with conservative defaults. The settings route reads and patches this record through the authenticated profile API. Updates are confirmed before persistence, and drafts remain local on errors.

On opt-out, update the local analytics boundary before the network request, opt out of PostHog capture and clear queued events. If persistence fails, keep capture off and explain the saved state could not be confirmed. On opt-in, persist first and enable subsequent events only after success. API analytics remain gated by the persisted opt-out value. Settings events carry only stable enum/control identifiers and common UUID properties.

Use desktop sections and narrow-screen stacked settings panels consistent with S23. Preserve the Profile form and its Update by chat entry point.
