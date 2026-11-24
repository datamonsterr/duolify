# Design

- Route `/` keeps its component. A thin wrapper checks the session: signed-out on web (not Tauri) renders `features/landing/Landing` (lazy); otherwise the current index.
- Sections in order: sticky 56 px top bar, hero (5/7 columns on desktop, stacked on phone), pillars, how it works, welcome block, footer.
- The welcome block is the same component as the sign-in welcome step (`WelcomeActions`), so the age check, SSO buttons (Apple only when SSO is enabled, same gate as today) and "Get started" behave identically on `/sign-in`.
- Copy lives in `messages.landing.ts` (EN, AR) registered with `registerMessages`.
- Video: 16:9, rounded, poster, centred play button, native controls after play, EN captions track, `preload="none"`.
- Page head: title, meta description and Open Graph image are set in `index.html` defaults and by the landing route.
- Privacy note only restates what the privacy text and consent code already do: analytics is anonymous, can be turned off, codes and tokens never reach analytics.
