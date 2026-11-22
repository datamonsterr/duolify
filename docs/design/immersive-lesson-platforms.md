# Immersive lesson: full screen, landscape and wake lock per platform

Spike 1.1 of `openspec/changes/immersive-mobile-lesson-player` (decision 2). Written 2026-11-22.

Evidence labels used on every claim:

- **VERIFIED BY RUNNING**: a command run in this task, result recorded. Source: `scratchpad/spike/probe.cjs` (throwaway, not in the repo) on Playwright 1.65.0 Chromium, headless, Linux. A Playwright "Pixel 7" or "iPhone 14" context is Chromium with a changed user agent, viewport and touch flag. It is not Android Chrome, not WebView and not WebKit.
- **DOCUMENTATION ONLY**: read from an official page or issue on 2026-11-22 (link given). Not run.
- **UNVERIFIED**: needs a real device or emulator.

## What this machine could and could not do

- `which adb emulator sdkmanager` finds nothing and `ANDROID_HOME` is empty. No Android SDK, so no Android emulator and no Tauri Android build. VERIFIED BY RUNNING.
- `apps/client/src-tauri` has no `gen/` directory: no Android or iOS project has been generated yet. The project has no Xcode (Linux host). VERIFIED BY RUNNING (`ls`).
- Playwright WebKit is not installed (`browserType.launch: Executable doesn't exist .../webkit-2388`). Even if it were, it is not iOS Safari. VERIFIED BY RUNNING.
- So every Tauri mobile, Android Chrome, WebView and iOS row below is DOCUMENTATION ONLY or UNVERIFIED.

## Per-platform table

| Platform | Full screen path | Landscape path | Wake lock path | Back / exit path | Status |
|---|---|---|---|---|---|
| Tauri Android | Element `requestFullscreen()` is reported to be cancelled in the Tauri Android WebView (wry's chrome client calls `onCustomViewHidden()` at once; no `onShowCustomView`). So: hide system bars natively (`WindowInsetsControllerCompat.hide(systemBars())`, transient-by-swipe) through a plugin command; CSS fixed stage fills the window | `android:screenOrientation` in the manifest is the maintainer workaround, but it locks the whole app. For per-lesson lock, a plugin command calling `Activity.setRequestedOrientation(SENSOR_LANDSCAPE)` then restore. CSS-rotated stage is the fallback | Web `navigator.wakeLock` (needs secure context and Chromium WebView support; UNVERIFIED in WebView) with fallback to a native `FLAG_KEEP_SCREEN_ON` plugin command; community plugins exist | System back: WebView history by default; `onBackButtonPress` in `@tauri-apps/api` >= 2.9.0 lets the app take over. The client already pushes a history entry on entry and exits on `popstate` | Documentation only. Native bridge needed for bars and orientation. Nothing built |
| Tauri iOS | WKWebView element full screen is not something Tauri documents; do not rely on it. Use a CSS fixed stage plus a plugin command hiding status bar and home indicator (`prefersStatusBarHidden`, `prefersHomeIndicatorAutoHidden`) | Xcode deployment info or `UISupportedInterfaceOrientations` in `Info.plist` (maintainer workaround, app-wide). Per-lesson: plugin command requesting geometry update. CSS-rotated stage is the fallback | Web `navigator.wakeLock` if the WKWebView supports it (Safari 16.4+ engine, UNVERIFIED in WKWebView); else native `UIApplication.isIdleTimerDisabled` plugin command | No hardware back. Edge swipe-back only moves WKWebView history if `allowsBackForwardNavigationGestures` is on (UNVERIFIED for Tauri). Always provide the on-screen exit control | Documentation only. Nothing built |
| Android Chrome (web) | `el.requestFullscreen({navigationUI:'hide'})` inside a tap | `screen.orientation.lock('landscape')` after full screen is entered (MDN: typically only in full screen) | `navigator.wakeLock.request('screen')`, https only, auto-released when the page is hidden; re-acquire on `visibilitychange` | Back gesture exits full screen first (MDN gives no exit text; this is general behaviour, UNVERIFIED). `fullscreenchange` fires either way and the client treats a lost full screen as exit | Mostly documentation only; API shapes VERIFIED BY RUNNING in Chromium emulation |
| iOS Safari (web) | No element full screen on iPhone (only `<video>`); `requestFullscreen` is absent or rejects, so CSS fixed stage only. Add to Home Screen (standalone) removes the browser chrome, UNVERIFIED for this app | Not lockable. CSS-rotated stage when the device is held portrait; follows the device when turned | `navigator.wakeLock` since Safari 16.4 (caniuse, WebKit blog). Standalone Home Screen apps: not stated by WebKit's blog, UNVERIFIED | Edge swipe-back navigates history: the client's pushed history entry makes the swipe exit immersive mode instead of leaving the page. UNVERIFIED on device | Documentation only |
| Desktop, Tauri | `requestFullscreen` works in the system webview in principle (UNVERIFIED per OS); `getCurrentWindow().setFullscreen(true)` is the window-level alternative (no platform restriction noted in the JS reference) | Not applicable: stage is letterboxed, never rotated (`pointer: coarse` is false) | Web `navigator.wakeLock` where the webview has it | Escape (document keydown and `fullscreenchange`) | Documentation only for Tauri; see Chromium row for API behaviour |
| Desktop, browsers | `requestFullscreen` | Not applicable | `navigator.wakeLock` | Escape; `fullscreenchange` | Chromium VERIFIED BY RUNNING (below); Firefox, Safari desktop not run |

## VERIFIED BY RUNNING: Chromium (Playwright 1.65.0, headless, Linux)

Command: `node /home/dat/.cache/tmp/claude-1000/-home-dat-dev-duolify/3c11ae02-db6a-44d9-9754-88e15e8d834c/scratchpad/spike/probe.cjs [Pixel|iPhone|Desktop]` against `page.html` served from localhost (secure context). Same results in all three contexts unless noted.

| Probe | Result |
|---|---|
| `document.fullscreenEnabled`, `el.requestFullscreen`, `webkitRequestFullscreen`, `screen.orientation.lock`, `navigator.wakeLock` | all present (true / function) |
| `requestFullscreen()` on a wrapper div after a real `page.click` | resolves; `document.fullscreenElement` is the wrapper; `fullscreenchange` fired |
| `requestFullscreen()` with no user gesture (`page.evaluate`) | **also resolved**. Headless Playwright does not enforce transient activation. Real browsers do (MDN); do not infer from this |
| `screen.orientation.lock('landscape')` | resolved with no error in every context, even outside full screen. Emulation only; `screen.orientation.type` stayed `portrait-primary` in the Pixel/iPhone context. I could not reproduce the real error names (`SecurityError`, `NotSupportedError`, `InvalidStateError`, `AbortError` per MDN) |
| `navigator.wakeLock.request('screen')` without permission grant | `NotAllowedError: Wake Lock permission request denied` |
| same with context permission `screen-wake-lock` | resolves, sentinel `type=screen`, `released=false` |
| `env(safe-area-inset-top/left)` | `0px` (no notch in headless; page has `viewport-fit=cover`) |
| `matchMedia('(orientation: landscape)')` after `setViewportSize` 390x844 then 844x390 | `false` then `true`; `screen.orientation.type` follows in mobile contexts (`portrait-primary`, `landscape-primary`); the Desktop context kept reporting `landscape-primary` |
| `page.setViewportSize` while fullscreen | rejects: `Browser.setWindowBounds: To resize minimized/maximized/fullscreen window, restore it to normal state first.` (matches the parent's earlier e2e result) |
| Playwright `keyboard.press('Escape')` while fullscreen | did **not** leave full screen (`fullscreenElement` still `wrap`). Browser-level Escape is not simulated; I only verified programmatic `document.exitFullscreen()` (element becomes null, `fullscreenchange` fires with false). Escape is DOCUMENTATION ONLY / UNVERIFIED |
| CSS-rotated stage, viewport 390x844, 1280x720 stage, `rotate(90deg) scale(s)` about the viewport centre, `s = min(844/1280, 390/720) = 0.5417` | iframe `getBoundingClientRect()` = `[0, 75.33, 390, 693.33]`, exactly the expected `720s x 1280s` box centred. Same maths as `layoutStage` in `geometry.ts` |
| Iframe (`sandbox="allow-scripts"`, srcdoc) while the ancestor goes `position:fixed` and is rotated | same node, 0 extra `load` events, no new message from the frame |
| `requestFullscreen()` on the already fixed, rotated ancestor | resolves; 0 extra `load` events; the iframe's `performance.timeOrigin` (reported once by its script) never changed, so the frame was not reloaded or re-created |

Caveats: headless, no real compositor or window manager; "fixed ancestor does not reload" matches the parent's separate e2e. Reparenting the iframe in the DOM would reload it; none of the above moves the node.

## Chosen strategy

1. The CSS stage (fixed, letterboxed, uniform scale, rotated 90 degrees when the screen is a touch device held portrait) is the universal baseline. It works with no platform API, never reloads the Slide (VERIFIED BY RUNNING above) and is what iPhone Safari gets.
2. On top of it, apply whatever the platform offers, best effort, each step failing silently to the next:
   a. Element full screen (browsers where it exists).
   b. Orientation lock, only after a, and only on `screen.orientation.lock` presence.
   c. Native bridge (Tauri mobile only) for system bars and orientation, because element full screen is unreliable in the Tauri Android WebView and not documented for WKWebView.
3. Wake lock: web `navigator.wakeLock` first, native keep-awake only if the web call is missing or rejects in the Tauri webviews.
4. Exit is always possible through the visible control; system back and `fullscreenchange` are accelerators.

## Detection order: what the client does today

Read from `apps/client/src/features/lesson-player/immersive/` (entry.ts, ImmersiveHost.tsx, geometry.ts, presentation.ts, useWakeLock.ts), not run on a device:

1. Entry inside a user tap calls `requestStageFullscreen(el)`: `el.requestFullscreen({navigationUI:'hide'})`, else `el.webkitRequestFullscreen()`, else nothing. Any throw or rejection returns `false` and leaves the CSS-only presentation.
2. `ImmersiveHost` mount: `relayout()` computes `layoutStage(visualViewport size, mode)` where `mode = 'landscape'` only if `matchMedia('(pointer: coarse)')` matches. `rotated = mode==='landscape' && vh > vw`; scale `min(w/1280, h/720)`; sets `--imm-scale`, `--imm-rot`, `data-imm-rotated`. Re-run on `resize` and `visualViewport` resize.
3. Appends `viewport-fit=cover` to the viewport meta while immersive and restores it on exit.
4. Orientation lock: only if `screen.orientation.lock` exists and `document.fullscreenElement` is set: `lock('landscape')`, then `relayout()`; rejection ignored. Re-attempted on every `fullscreenchange` that enters full screen. Exit calls `screen.orientation.unlock()` in a try.
5. Exit wiring: `fullscreenchange` to no element after full screen was seen means exit (`escape`); `keydown` Escape (closes the Slide list first); a pushed history entry and `popstate` means exit (`back`).
6. Wake lock (`useWakeLock`): `navigator.wakeLock?.request('screen')` while active; released on `visibilitychange` hidden, re-acquired when visible; failure ignored.
7. Rotation entry: `matchMedia('(orientation: landscape)')` change plus `(pointer: coarse)` and the setting enters immersive; turning back exits only if rotation entered it.

Gaps against the strategy above (not changed in this task): no Tauri detection and no native bridge; no `visibilitychange`-independent `release` event handling for the wake lock sentinel; no `fullscreenEnabled` pre-check (harmless, the try/catch covers it).

## Tauri native bridge needed (NOT shipped in this task)

Tauri 2 mobile plugins are Rust crates with Kotlin (`@TauriPlugin`, `@Command fun x(invoke: Invoke)`) and Swift (`class P: Plugin`, `@objc func x(_ invoke: Invoke)`) code (DOCUMENTATION ONLY: tauri-docs develop/plugins/develop-mobile). No community plugin for orientation lock or immersive bars was found (web search 2026-11-22; Tauri issue #13408 "App orientation for iOS / Android" is open, workaround is manual manifest/Xcode edits, DOCUMENTATION ONLY). Keep-awake plugins exist (`tauri-plugin-keep-awake` alpha, `tauri-plugin-keep-screen-on` 0.1.4; the iOS path of the first is stated as not checked at runtime). `Window.setFullscreen` has no mobile note in the JS reference, so its effect on Android and iOS is UNVERIFIED; `design.md` decision 2 assumes it hides system bars, which is not documented. Treat as unproven.

Proposed command shapes (names only, one in-house plugin `immersive`):

- `immersive_enter({ orientation: "landscape" | "none", hideSystemUi: boolean }) -> { orientationLocked: boolean, systemUiHidden: boolean }`
- `immersive_exit() -> void` (restore orientation, show bars, clear keep-awake)
- `immersive_keep_awake({ on: boolean }) -> { held: boolean }`
- Capability file permissions `immersive:default`; no network, no file access.
- Android: `WindowInsetsControllerCompat` with `BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE` (Android docs), `setRequestedOrientation`, cutout mode `shortEdges`. iOS: status bar and home-indicator hiding, supported-orientations update.
- The client calls them only when `window.__TAURI_INTERNALS__` exists and the platform is mobile; all results are advisory, the CSS stage still lays out correctly if they fail. Android WebView element full screen is better left unused rather than patched with a custom `WebChromeClient` (one third-party project did that; not endorsed here).

## Safe area, sandbox and CSP

- `apps/client/index.html` line 8: `<meta name="viewport" content="width=device-width, initial-scale=1.0" />`. `viewport-fit=cover` is NOT set (VERIFIED BY RUNNING, `grep`). Without it, iOS (and Android Chrome) letterbox inside the safe area and `env(safe-area-inset-*)` is 0. The immersive host adds `viewport-fit=cover` at runtime while immersive and removes it on exit. Whether iOS Safari and WKWebView re-apply a changed viewport meta live is UNVERIFIED; if not, set it permanently in `index.html` and pad the framed player with `env()`.
- Insets apply to controls only (`--sa-*` in `immersive.css`), never to the Slide.
- In the rotated stage the physical insets (notch) map to different logical sides. The client reads `env()` per physical side, so rotated controls must remap them; UNVERIFIED on device.
- Iframe `sandbox="allow-scripts"` (no `allow-same-origin`) and the Tauri CSP (`frame-src 'self' about:`) stay unchanged. Full screen is requested on the host wrapper, never inside the iframe, so no `allow="fullscreen"` and no `allow-orientation-lock` sandbox flag is needed. If a Slide script itself called `screen.orientation.lock`, the sandbox would block it (MDN: `SecurityError` without `allow-orientation-lock`); slides never do.
- No new `connect-src` or script origins are required by any of this.

## Back, predictive back, swipe-back

- Android Tauri, default: Back goes to WebView history when it can go back, else leaves the app (third-party description, DOCUMENTATION ONLY). `@tauri-apps/api` 2.9.0 added `onBackButtonPress` (DOCUMENTATION ONLY). Tauri PR #16548 (open when searched) changes the back callback to be enabled only while the webview can go back, to support Android 16 predictive back. Issue #13957: `canGoBack()` can be false until the user interacts, so the history entry pushed on entry might not count if there was no prior interaction; entry is always inside a tap or a rotation, rotation entry is the risk. UNVERIFIED.
- Android Chrome: Back exits element full screen first (general behaviour, UNVERIFIED); then `popstate` from the pushed entry exits immersive.
- iOS: no back button; edge swipe pops the pushed history entry, which the client treats as exit. UNVERIFIED on device. Always keep the on-screen exit control.
- Desktop: Escape.

## Remaining UNVERIFIED (needs real devices)

Everything not under "VERIFIED BY RUNNING". Notably: any Tauri Android or iOS behaviour; element full screen in Tauri WebViews; real `lock()` error names and the full-screen requirement on Android Chrome; `lock()` on iOS Safari (caniuse shows the Screen Orientation API as supported on Safari iOS 16.4+ for the whole API, MDN says `lock()` is not available; real answer unknown); wake lock in WKWebView, in the Android WebView and in standalone Home Screen apps; real Escape and system-back exit order; live `viewport-fit` change; rotated safe-area mapping; predictive back on Android 14+; iframe non-reload in WebKit and Android WebView (verified in Chromium only).

## Owner checklist (real devices)

1. Android Chrome, portrait: tap expand. Bars vanish, orientation goes landscape. Back exits full screen; second Back leaves the lesson? Note the order.
2. Android Chrome: keep playing 2 minutes with a 30 s screen timeout. Screen stays on? Lock the phone and unlock: still playing?
3. iPhone Safari (16.4+): tap expand. Stage rotates in portrait, follows when turned, notch not covering controls, Home indicator not overlapping the progress bar. Swipe from the left edge: leaves immersive, not the lesson?
4. iPhone: same with the page added to the Home Screen.
5. Tauri Android build (needs `cargo tauri android init`): tap expand; do the bars hide; is `requestFullscreen` rejected; does Back exit; does the screen stay awake.
6. Tauri iOS build: same; also status bar, Home indicator, rotation lock off and on.
7. In every case the Slide audio and position must not jump on entering or leaving.
