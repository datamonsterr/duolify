# Duolify client

Run `mise run client:dev` for the Tauri app or `pnpm dev` in this directory for the web client. Development uses mock API data. The client reads `VITE_API_URL`, `VITE_POSTHOG_KEY`, and `VITE_POSTHOG_HOST` at build time.

On Debian/Ubuntu, Tauri requires `libwebkit2gtk-4.1-dev`, `build-essential`, `curl`, `wget`, `file`, `libxdo-dev`, `libssl-dev`, `libayatana-appindicator3-dev`, and `librsvg2-dev`. See [Tauri prerequisites](https://tauri.app/start/prerequisites/) for other platforms.

After installing the Android SDK, NDK and Rust targets described in the prerequisites, run `pnpm tauri android init` in this directory. On macOS, install Xcode, CocoaPods and the iOS Rust targets, then run `pnpm tauri ios init`. Generated mobile projects are local build artifacts and are not committed.
