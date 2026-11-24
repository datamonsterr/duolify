# Mobile releases

Pushing a tag `vMAJOR.MINOR.PATCH` (or dispatching the `release` workflow) builds the mobile clients and publishes a GitHub Release. The release notes carry a changelog generated from Conventional Commits since the previous tag.

## Release assets

| Asset | What |
|---|---|
| `duolify.apk` | Signed Android release APK, stable name (always the same URL path per tag) |
| `duolify-<version>.apk` | Same APK with its version in the name |
| `duolify-android-qr.png` | QR code that opens the APK download URL |
| `duolify-ios-simulator.zip` | Unsigned iOS simulator `Duolify.app` (no Apple developer account, no device install) |
| `SHA256SUMS` | Checksums of the assets above |

## Install on Android

1. Open the release page on the phone, or scan `duolify-android-qr.png` from another screen.
2. Download `duolify.apk`, allow "install unknown apps" for the browser when asked, and open the file.
3. Later releases install over the old one: every release is signed with the same key.

To verify the file: `apksigner verify --print-certs duolify.apk`. The single signer SHA-256 must be
`37:83:6C:F7:ED:0F:11:40:20:66:B0:3D:E1:4C:77:14:F3:82:EC:A5:02:C6:ED:9F:0A:8C:40:73:05:82:DD:89`.

## Run on the iOS simulator (macOS with Xcode)

```sh
unzip duolify-ios-simulator.zip
xcrun simctl boot "iPhone 16"        # any available iPhone simulator
open -a Simulator
xcrun simctl install booted Duolify.app
xcrun simctl launch booted app.duolify.client
```

Known issue ([#341](https://github.com/datamonsterr/duolify/issues/341)): the simulator build opens to "Could not open your account. Connect and retry." instead of the welcome page.

The build is for the arm64 simulator only. It is ad-hoc signed, so it never runs on a physical iPhone; TestFlight and App Store distribution are out of scope.

## Cutting a release

1. Set the same version in `apps/client/package.json`, `apps/client/src-tauri/tauri.conf.json` and `apps/client/src-tauri/Cargo.toml`, and merge to `main`.
2. Tag `main` and push: `git tag v0.2.0 && git push origin v0.2.0`.
3. The workflow fails before publishing if the tag and versions differ, signing secrets are missing, or the APK certificate is not the expected one.

`workflow_dispatch` with `dry_run` (default on) builds and verifies everything and uploads the files as a workflow artifact without creating a release.

Publishing (tag push, or dispatch with `dry_run=false`) fails unless the commit is an ancestor of `origin/main`. A publishing dispatch must also use the `main` ref. Dispatch with `test_missing_signing=true` blanks the signing secrets to prove the missing-secrets path fails closed.

## Signing

Secrets (repository secrets, never committed): `ANDROID_KEYSTORE_BASE64`, `ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS`, `ANDROID_KEY_PASSWORD`. The keystore is decoded to the runner temp directory, used only by the release build type, and the APK is checked with `apksigner` against `scripts/release/android-cert.sha256` (exactly one signer, exact match, otherwise the job fails and nothing is published). Debug builds in CI are never signed with this key.

Validation scripts live in `scripts/release/` and run with `mise run release:test`.
