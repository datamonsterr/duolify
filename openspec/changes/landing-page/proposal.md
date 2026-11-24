# Landing page

Owner request (2026-11-23): signed-out visitors on the web see a landing page with a one-minute video and short instructions, privacy notes in the footer, and a cleaner sign-in block (same PR as the sign-in screen fix).

## Impact

- **Client (web only):** Signed-out `/` renders a lazy landing route: top bar, hero with the video, three pillars, three-step "how it works", the welcome view (age check, Google, Apple, Get started) and a footer with Privacy, Terms, Analytics preference and one privacy note. Signed-in `/` is unchanged. Tauri desktop and mobile builds skip the landing and open the welcome view directly.
- **Welcome view:** One centred column, max 360 px. The body "I already have an account" button is removed (the header has "Sign in"); "Explore shared example" moves to the hero as a text link.
- **Media:** The video, its poster and captions are not in git and not in the Tauri bundle. They load from a media base URL set in one config constant (`VITE_LANDING_MEDIA_BASE`). The video uses `preload="none"` and never autoplays.
- **Analytics:** `landing_viewed`, `landing_video_played`, `landing_video_completed`, `landing_cta_clicked{target}`. Anonymous (no identity, no learner text), sent only when the visitor has not opted out of analytics.
- **AI / Engine / proto / API / data:** none. No evals or fallbacks apply. Backup: not applicable, no data is stored.
- **Budget:** The landing and its copy catalogue are lazy chunks. `bundle-budget.json` is not raised.
