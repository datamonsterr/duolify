## Why
Turn an approved screened draft (verdict=pass) into a link-visible publication. Only a draft with verdict pass can be published (needs_review stays private until moderation exists). Visitors can inspect safe sample slides before signing in.

## What Changes
- New `publication_links` Postgres table: >=128-bit opaque random token stored only as its SHA-256 hash. Create/revoke/rotate take effect immediately.
- New `course_visibility` column on `courses` (private | link). Defaults private.
- Public preview endpoint (anonymous allowed): title, creator username/display_name/avatar, description, outline, up to 3 sample slides, aggregate enrollment count, version. NO answer key / test explanations / private retry pool / narration audio. X-Robots-Tag: noindex + meta noindex. Per-IP rate limit from config.
- App deep link "launch app" from preview page.
- Analytics events: course_visibility_changed, course_published, share_link_created, share_link_revoked, share_link_opened added to catalog.
- Data: visibility/link hashes in nightly dumps, versioned snapshot refs, no token plaintext in backup logs. docs/data-inventory.md updated.
- Config: SHARING_PUBLIC_DOMAIN, SHARING_PREVIEW_RATE_LIMIT_PER_IP, SHARING_APP_SCHEME (from env, with .env.example comments).

## Out of scope
marketplace (public visibility), republish, enrollment, invites, moderation queue (E15-03+).
