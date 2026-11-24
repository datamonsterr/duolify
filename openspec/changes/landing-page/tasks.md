## 1. Tests first

- [x] 1.1 Playwright: signed-out `/` shows the landing, CTA opens the sign-in chat, header Sign in, no duplicate account button, video poster and no autoplay, footer links, signed-in `/` skips the landing.
- [x] 1.2 Unit per section, analytics opt-out test.

## 2. Implementation

- [x] 2.1 Landing route and sections, lazy catalogue (EN, AR), media config constant.
- [x] 2.2 Welcome block (single column, Google, Apple, Get started), remove duplicate account button, hero link for the shared example.
- [x] 2.3 Page title, description and Open Graph image.

## 3. Data and analytics

- [x] 3.1 Backup: not applicable.
- [x] 3.2 Anonymous landing events and `docs/analytics/coverage.md`.

## 4. Owner steps

- [ ] 4.1 Upload with `mise run infra:landing-media-upload <dir> v1` (owner), then set `VITE_LANDING_MEDIA_BASE` for the web build.
