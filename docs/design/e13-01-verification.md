# E13-01 verification

Ticket: E13-01, GitHub #31. Design baseline: app-design Revision 2026-11-09.

## Source and scope

The client imports `docs/design/theme.css` directly, including its Tailwind 4
`@theme inline` aliases, explicit light/dark groups and reduced-motion overrides.
No client copy of the palette is maintained. Integration checks compare every
color, dimension, typography, duration, easing, elevation and breakpoint mapping
against `tokens.json`; unit checks measure every approved text/control contrast
pair without rounding. Browser checks verify computed component colors.

Nunito Latin WOFF2 weights 600/800/900 are vendored from `@fontsource/nunito`
5.3.0, with the package's SIL OFL-1.1 license in
`apps/client/src/assets/fonts/OFL.txt`. Vite emits three hashed local WOFF2 assets.
The font stack retains Unicode/system fallbacks. Browser checks load all three
weights and assert local font origins with no font-service requests.

Inspected the HTML and rendered Foundations, Components and S03-Courses boards,
desktop first and then the mobile states. Inspected client light/dark screenshots
at 1280px and 390px. Canonical tokens override older canvas typography/colors.
Geometry exceptions are the documented 1px borders, percentage-based progress,
viewport sizing and safe-area inset; the original two-tile placeholder mark uses
shared spacing/radius tokens. Lime contains no text. Path nodes use 72px tokens.

Courses retains API-backed titles/descriptions and derives completion from actual
lesson states. Loading is static, unavailable cards have no destination, failed
refresh retains loaded courses, Retry preserves search, and Clear restores input
focus. Offline lists show the last response's local as-of time and make no claim
that lessons are cached. Tests cover keyboard and touch, pending duplicate
protection, 320px reflow, 200% text, long Unicode content, both themes and system
or explicit reduced motion. Mobile navigation reserves its own grid row rather
than covering content.

Create and Discover are visible unavailable controls with explanations. The
parent explicitly confirmed that creation/marketplace implementations belong to
later tickets. This change leaves `src/features/pricing` untouched. It adds no
AI/provider behavior, learner persistence or new environment variables.

The existing app-design event catalog precedes these emitters. Stable control
IDs are checked with a PostHog test double, UUID/common properties, revoked
consent, disabled/missing context and transport failure. Search/title/description
text is never passed to analytics. E1-06 may install authenticated consent context
with `configureUiAnalytics`; the scaffold supplies none and emits no UI events.

## Commands and results

All commands ran from this run worktree through mise. Final checks:

| Exact command | Result |
| --- | --- |
| `mise run client:install` | PASS, frozen lockfile |
| `mise run client:lint-web` | PASS, ESLint + Prettier + TypeScript |
| `mise run client:test-web` | PASS, 16 tests / 4 files |
| `mise run client:build` | PASS, three local WOFF2 assets; existing bundle-size advisory remains |
| `mise run client:e2e` | PASS, 9 tests including the existing lesson completion flow |
| `mise run ci:openspec` | PASS, 22 strictly validated items |
| `git diff --check` | PASS |

TDD and intermediate checks (failures were retained and fixed, never skipped or
weakened):

| Exact command | Result / follow-up |
| --- | --- |
| `mise run client:e2e -- design-e13-01.spec.ts --workers=1 --max-failures=1` | RED before integration/unit tests: missing accessible course article. Corrected fixture interception and JSON import attributes during harness setup. |
| `mise run client:test` | RED for missing Courses integration, then RED for missing Button unit imports, before implementation. Later all web tests passed and the native phase failed for missing WebKitGTK. |
| `mise run client:e2e -- design-e13-01.spec.ts --workers=2` | Initially failed fixture interception; later focused run passed all seven then-existing tests. |
| `mise run client:e2e -- design-e13-01.spec.ts --workers=2 --max-failures=2` | Initially failed CSS minifier short-hex serialization; normalized equivalent color notation, then seven tests passed. |
| `mise run client:e2e -- design-e13-01.spec.ts --grep 'pending refresh' --max-failures=1` | RED for overlapping mobile navigation, then PASS after reserving a navigation grid row. |
| `mise run client:lint` | Initially failed generated Playwright artifact formatting; generated artifacts now excluded from source lint/format checks. |
| `mise run client:lint-web` | Intermediate artifact cleanup race and missing TypeScript 6 Node ambient types fixed; final command passes. |
| `mise run ci:tauri-deps` | Could not install missing native prerequisites because sudo requires a password. |

The user explicitly instructed skipping native checks locally because Namespace
CI supplies Rust/WebKit prerequisites, and no `apps/client/src-tauri` files
changed. Local native `cargo fmt`, `cargo clippy`, `cargo test` and Tauri binary
build are therefore deferred to CI. The full `client:lint` / `client:test` tasks
still run the same web checks as dependencies followed by their native phases;
CI coverage is preserved.

## Official API references consulted

- [Tailwind 4 theme variables](https://tailwindcss.com/docs/theme)
- [Vite static assets and CSS font URLs](https://vite.dev/guide/assets)
- [Playwright assertions](https://playwright.dev/docs/test-assertions) and [Page routing](https://playwright.dev/docs/api/class-page#page-route)
- [Fontsource installation and self hosting](https://fontsource.org/docs/getting-started/install)
- [React external-store subscriptions](https://react.dev/reference/react/useSyncExternalStore)
- [TanStack Query queries](https://tanstack.com/query/latest/docs/framework/react/guides/queries)
- [Testing Library render/cleanup](https://testing-library.com/docs/react-testing-library/api/)
- [Vitest mocks](https://vitest.dev/api/vi.html)
- [PostHog capture and opt-out](https://posthog.com/docs/libraries/js/usage)
- [TypeScript 6 explicit ambient types](https://www.typescriptlang.org/tsconfig/types.html)
