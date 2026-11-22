# ADR 0023: Mobile builds and distribution

- Status: Accepted
- Date: 2026-11-23
- Change: `openspec/changes/mobile-builds-and-releases`

## Context

Tauri's desktop client has no reproducible mobile builds. Android learners need an installable build; there is no Apple Developer account to sign iPhone builds. Native clients use browser-based Clerk SSO and a secure device store. ADR 0022 keeps the application CSP restrictive.

## Decision

Generate Android and iOS projects in CI with Tauri's non-interactive initializers. Android's generated manifest is amended to declare microphone permissions; Tauri’s generated debug build permits HTTP for the disposable emulator test backend, while release builds do not. iOS merges `Info.ios.plist` for microphone usage. The existing deep-link configuration supplies `duolify://`, and the mobile keyring plugin is registered in the native builder. Regeneration on a warm Cargo cache can lose iOS URL schemes, so clean the deep-link plugin after initialization and assert the scheme in the built app's plist.

Mobile builds default to staging API and SSO origins. Android and iOS overlays permit staging API, web, and asset origins without widening the desktop base CSP. Android full-stack tests use a separate debug-only overlay to reach the disposable host backend at 10.0.2.2.

Use Maestro on an Android API 34 x86_64 emulator with KVM and an arm64 iPhone simulator, preserving screenshots. Android tests sign in against the disposable backend; iOS runs launch and landscape checks because the hosted macOS runner has no Linux Docker backend. Both jobs are advisory and excluded from `ci-ok` initially; Android may join the required aggregate after three consecutive green runs.

Versioned GitHub Releases contain a certificate-verified, release-signed APK, an unsigned iOS simulator app, checksums and two locally generated QR codes. The Android QR points to the stable latest APK; the iPhone QR points to the staging web app for Add to Home Screen. A future TestFlight release requires Apple Developer enrollment, signing and provisioning; none are configured here.

## Consequences

Native projects are reproducible build output, not checked-in source. Generated project customizations must be reapplied deterministically after initialization. Signing secrets remain external and a missing or incorrect certificate fails publication. Existing analytics events carry platform and version under their existing privacy rules. There is no new learner data or backup procedure; the signing key requires an owner-managed backup outside the repository. AI behavior does not change.
