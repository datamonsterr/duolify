# ADR 0005: Tauri 2 + React client, Motion HTML slides in sandboxed iframes

- Status: Proposed (confirm)
- Date: 2026-11-08

## Decision

- **Client**: Tauri 2 (desktop and mobile from one codebase) with React, TypeScript, Vite, Tailwind CSS, TanStack Router and TanStack Query, pnpm. UI and gamification patterns follow Duolingo-style learning apps (reference: sanidhyy/duolingo-clone), with Duolify's own branding.
- **Lesson player**: each slide is a self-contained Motion HTML document stored in object storage. The player loads one slide at a time into a sandboxed `<iframe sandbox="allow-scripts">` with a strict CSP (no network except the asset origin). A small `postMessage` protocol (`play`, `pause`, `seek`, `ended`, `checkpoint`) controls playback. The next slide preloads while the current one plays.
- **Slide authoring format**: plain HTML + CSS animations + Web Animations API; the engine may include the `motion` JS library from a pinned, self-hosted asset. No rendering to MP4.
- **Tests**: Vitest + Testing Library for units; Playwright against the Vite web build for e2e; Tauri-specific checks with `tauri build --debug` in CI.

## Consequences

- Generated HTML is untrusted: sandbox and CSP are mandatory; the engine also validates output.
- Slides work offline once cached.
