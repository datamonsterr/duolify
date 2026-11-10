# Design: Download an Entitled Offline Course Library

## Server-Side Entitlement and Download Index API
1. Entitlement check:
   - Queries `credit_subscription_terms`, `credit_subscriptions`, and `credit_checkout_orders` for the authenticated learner where `t.service_start <= now() AND t.paid_through > now() AND NOT EXISTS (revocations)`.
   - Confirms that the plan benefits stored in the term (or resolved from active pricing configuration for the plan) include `"Offline course library downloads"`. Learn, Plus, and Pro tiers qualify.
   - Refuses unentitled or expired learners with HTTP 403 Forbidden and JSON payload `{"error": "subscription_required"}`.
   - Reuses existing `credits.Service.FundedPlan` / entitlement logic from E7 without duplicating subscription rules or hardcoding plan names/prices.
2. Course library download endpoint:
   - `GET /v1/courses/{course}/download`: returns course ID, entitlement status, and an array of unlocked/generated lessons for the course (e.g. `lesson_id`, `title`, `unit_id`, `position`, `status`, `manifest_key`).
   - Only returns generated lessons with valid manifests for enrolled learners.
   - No AI charge or quote is incurred for downloading existing generated lessons.

## Client-Side Resumable Download Controller and Cache
1. Cache Parameterization & Storage Controls:
   - `cache.ts` bounds are adapted so that bulk-downloaded courses do not evict each other under a strict 5-lesson limit. The account cache limit supports up to 50 lessons or configurable account size (e.g., 80 MiB), while preserving atomic checksum validation per bundle.
   - Storage controls report downloaded lesson counts and approximate byte size.
   - Learners can clear downloaded courses via storage controls, removing cached bundles from device storage.
2. Resumable Queue:
   - Bulk download downloads unlocked lessons sequentially.
   - Manifests and slide documents are fetched and verified with SHA-256 integrity before publication.
   - If interrupted, existing verified lessons remain in cache. Resuming starts from the first un-downloaded lesson.
3. Subscription Expiry Lifecycle:
   - When a subscription expires, already downloaded and cached lessons remain fully playable offline without network access.
   - Attempting to start a new bulk download refuses with a subscription required message and prompt to review options.
   - Expired subscriptions cannot unlock or generate future lessons.
4. Device-Disposable Index:
   - The list of saved courses/lessons is tracked in client storage and updated whenever lessons are saved or removed.
   - Explicit logout or credential expiration invokes `session.clear()` / `clearLessonCache()`, evicting cached bundles and clearing local download state.

## Canvas and Responsive Parity
1. Desktop (`>= 48rem`) and Mobile (`< 48rem`):
   - In course and learn path screens (`S03-Courses.dc.html`, `S08-Path.dc.html`), displays download library action and offline state indicators.
   - Shows badge "Saved offline" on downloaded lessons, matching canvas.
   - Offline warning banner: "Offline. Saved lessons are available. Progress syncs when you reconnect."
   - Download/storage controls open as a panel on desktop and a responsive sheet (`ResponsivePanel`) on mobile.
2. Accessibility:
   - Focus management, keyboard navigation (Escape to close sheet/dialog), ARIA roles (`role="status"`, `role="progressbar"`), screen reader announcements.
   - Dark and light theme contrast conforming to design tokens (`docs/design/tokens.json`).
   - Reduced motion disables transitions and provides non-animated progress meters.
   - 320px reflow without horizontal scrolling.

## Analytics Allowlist and Catalogue
- `ui_control_activated`:
  - `control_id: "download_library"`
  - `surface: "courses"` or `"course_path"`
  - `course_id: <uuid>`
- `ui_recovery_selected`:
  - `surface: "courses"` or `"course_path"`
  - `action: "retry" | "exit"`
  - `error_code: "subscription_required" | "download_failed" | "storage_exceeded"`
- All events use authenticated UUID identity, common properties (`platform`, `app_version`, `source`), respect opt-out, and never emit learner text or slide content.
