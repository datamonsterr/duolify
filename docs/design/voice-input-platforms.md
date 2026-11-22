# Voice input: microphone capture per platform

Spike for OpenSpec change `immersive-mobile-lesson-player`, task 1.2. Date: 2026-11-22.
Scope: push-to-talk capture with `getUserMedia` and `MediaRecorder` in the Tauri 2 client
(`apps/client`, Tauri 2.12.5, wry 0.57.1 per `Cargo.lock`) and in browsers.

**What was verified by running, and what was not.** Only desktop Chromium was run (Playwright
`playwright-core` 1.65.0 driving Chromium 158 headless with `--use-fake-device-for-media-stream`,
`--use-fake-ui-for-media-stream` and `--use-file-for-fake-audio-capture` playing 34 s of speech;
script and page are described in "Measurements" below). Everything about iOS (WKWebView and Safari),
Android (WebView and Chrome), WebKitGTK and permission behaviour is from documentation and from reading
the `wry` source; no device or emulator was available. The table marks each cell **[run]** or **[docs]**.

## Findings in one paragraph

Capture needs no Tauri plugin and no Tauri capability: `getUserMedia` and `MediaRecorder` are web APIs
of the system webview, and Tauri capabilities only gate IPC commands. The mobile projects need
a manifest/plist entry (Android `RECORD_AUDIO`, iOS `NSMicrophoneUsageDescription`). Containers differ:
Chromium-family webviews and Android Chrome record WebM/Opus; Safari and iOS WKWebView record MP4/AAC
(and WebM/Opus from Safari 18.4); the client must feature-detect and send the container the recorder
reports. The one measured surprise: Chromium's default bitrate is about 129 kbps, so 30 s is 483 KB
(under 1 MB, but with only 2x headroom); asking for `audioBitsPerSecond: 32000` gives about 113 KB. Safari's
bitrate options are not documented as supported, so the 1 MB limit has to be enforced by measuring the
Blob, not by trusting a requested bitrate.

## Per-platform table

| | Tauri Android (system WebView) | Tauri iOS (WKWebView) | Android Chrome | iOS Safari | Desktop Chromium (WebView2, Chrome) | Desktop WebKit (macOS WKWebView, Linux WebKitGTK) |
|---|---|---|---|---|---|---|
| Engine | Chromium WebView | WebKit | Chromium | WebKit | Chromium | WebKit |
| `MediaRecorder` output | `audio/webm;codecs=opus` [docs: Chromium behaviour; same engine as the run] | `audio/mp4` (AAC) [docs: WebKit blog 11353; WebM/Opus from Safari 18.4 per WebKit 18.4 notes] | `audio/webm;codecs=opus` [docs] | `audio/mp4` (AAC); `audio/webm;codecs=opus` from 18.4 [docs] | `audio/webm;codecs=opus` **[run]**; `audio/mp4` also reported supported but it contains **Opus, not AAC** **[run]** | macOS: as Safari [docs]. WebKitGTK: depends on the GStreamer plugins installed; not checked [unverified] |
| Mime types to request | try in order: `audio/webm;codecs=opus`, `audio/mp4`, then no mimeType | same | same | same | same | same |
| Permission entry | Manifest: `RECORD_AUDIO` and `MODIFY_AUDIO_SETTINGS` | `Info.plist`: `NSMicrophoneUsageDescription` | Browser prompt per origin | Browser prompt per origin (Safari asks again on each page load unless the user chose to remember) [docs/known behaviour] | Browser prompt; WebView2 host decides | macOS: Info.plist `NSMicrophoneUsageDescription` also needed in the bundle |
| Who prompts | wry `RustWebChromeClient.onPermissionRequest` requests the Android runtime permission, then grants or denies the WebView request [docs: wry source] | wry's WKUIDelegate grants the web-layer request when no handler is set, iOS then shows its own system prompt using the plist string [docs: wry 0.57.1 source] | Chrome | Safari | Browser | wry |
| Denied | `NotAllowedError`; wry calls `request.deny()` [docs] | `NotAllowedError`; iOS does not prompt again, the Learner must use Settings [docs] | `NotAllowedError`; "Blocked" site setting | `NotAllowedError` | `NotAllowedError` [docs MDN]; headless without a prompt UI throws `NotSupportedError` **[run]** | same |
| Recorder stops on track end | yes **[run]**: stopping all tracks fires `stop` and delivers the data | same [docs: spec] | same | same | **[run]** | same |

[run] = observed with Chromium 158 headless; [docs] = official documentation or source read, not executed.

## Measurements (Chromium 158, fake device playing speech)

Script: a throwaway page that calls `getUserMedia({audio:{channelCount:1, echoCancellation:true,
noiseSuppression:true, autoGainControl:true}})`, picks the first supported mime type from
`audio/webm;codecs=opus, audio/webm, audio/mp4;codecs=mp4a.40.2, audio/mp4, ...`, records for N
seconds and returns the Blob. Run through Playwright; nothing from it is committed (it was a scratch
page; the code is simple enough to rewrite from the table above).

| Recording | Mime type reported | Bytes | Effective kbps | Container check (ffprobe) |
|---|---|---|---|---|
| 5 s, default bitrate | `audio/webm;codecs=opus` | 80,474 | 128.8 | opus, 48 kHz, mono, matroska/webm, 4.92 s |
| 15 s, default | same | 241,796 | 129.0 | opus, 14.94 s |
| 30 s, default | same | 483,296 | 128.9 | opus, 29.94 s |
| 30 s, `audioBitsPerSecond` 32000 | same | 113,611 | 30.3 | opus, 29.94 s |
| 30 s, 24000 | same | 86,484 | 23.1 | opus, 29.94 s |
| 30 s, 16000 | same | 59,321 | 15.8 | opus, 29.94 s |
| 10 s, `audio/mp4` default | `audio/mp4;codecs=opus` | 161,263 | 129 | **opus** in MP4, 9.92 s |
| 10 s, `audio/mp4`, 32000 | `audio/mp4;codecs=opus` | 37,993 | 30.4 | opus in MP4 |

- Capture sample rate was 48 kHz mono with echo cancellation and noise suppression on.
- The recorder emitted one chunk per recording with no timeslice; it fires `dataavailable` once at `stop()`.
- The WebM file has no duration cue in a streamed recording; ffprobe still read 29.94 s. Whether each
  transcription provider tolerates such a file is measured in `evals/spikes/transcription/README.md`.
- Budget: 32 kbps Opus is 4 kB/s, so 30 s is about 120 KB, 8x under the 1 MB limit. Keep the default
  only if the client measures the Blob (the default is 483 KB, 2x under).
- Not run: permission denied by the user (headless Chromium without a prompt UI throws
  `NotSupportedError`, not `NotAllowedError`), permission revoked in system settings mid-recording,
  incoming call or app backgrounding, any iOS or Android environment.

## What the client should do (inputs for tasks 6.3 and 6.6)

1. Secure context: `navigator.mediaDevices` is `undefined` outside one. The dev and prod origins
   (`http://localhost`, the Tauri custom scheme/host on each OS, the HTTPS site) must be confirmed as
   secure contexts on a real iOS and Android build before the UI is built. **Unverified**: Android
   serves `http://tauri.localhost` (a `*.localhost` host, which Chromium treats as potentially
   trustworthy); iOS serves a `tauri://` custom scheme, and whether WKWebView treats it as secure for
   `getUserMedia` was not established. Feature-detect `navigator.mediaDevices?.getUserMedia` and fall
   back to typing with an explanation.
2. Request the stream on the first press (a user gesture), not at page load; keep it for the session
   only while the control is held or toggled, and call `track.stop()` on every track at the end so the
   OS microphone indicator goes off. Reusing one stream across presses keeps the indicator on, which
   conflicts with the spec's "recording only while held".
3. Container selection: `const type = ['audio/webm;codecs=opus','audio/mp4'].find(MediaRecorder.isTypeSupported)`
   and pass no `mimeType` when none match. Always upload `recorder.mimeType`, not the requested one
   (Chromium's `audio/mp4` is Opus, WKWebView's is AAC), and let the API sniff the container rather
   than trust the label.
4. Size: request `audioBitsPerSecond: 32000`, then check `blob.size <= 1 MB` before upload; stop at 30 s with
   a timer in the client (do not rely on bitrate). Discard under 0.5 s.
5. Errors to map to Learner states: `NotAllowedError` (denied; offer Settings guidance, keep typing),
   `NotFoundError` (no microphone), `NotReadableError` (another app holds it), `NotSupportedError`
   (webview without capture), and the track `ended`/`mute` events (interruption: stop and discard or
   keep what was recorded, never silently continue).
6. Interruptions: on iOS the microphone is interrupted by calls, Siri and backgrounding; the track
   mutes or ends [docs: Apple/WebKit general behaviour, not tested]. Treat `visibilitychange` to hidden
   as a cancel.

## Tauri project configuration

- **Android**: after `pnpm tauri android init` the manifest is
  `src-tauri/gen/android/app/src/main/AndroidManifest.xml`; add
  `<uses-permission android:name="android.permission.RECORD_AUDIO"/>` and
  `<uses-permission android:name="android.permission.MODIFY_AUDIO_SETTINGS"/>` (wry's
  `RustWebChromeClient` asks for both when a page requests audio capture). Tauri docs show
  editing this generated manifest as the supported way to add permissions. `RECORD_AUDIO` is a runtime
  ("dangerous") permission, so Android shows its dialog on first capture. The repo has no `gen/` yet.
- **iOS**: `NSMicrophoneUsageDescription` in a plist merged by Tauri. Per the Tauri 2 configuration reference
  (`bundle.iOS.infoPlist`), Tauri also looks for `Info.plist` and `Info.ios.plist` next to
  `tauri.conf.json`, so create `apps/client/src-tauri/Info.ios.plist` (iOS only, not macOS) with a string
  such as "Duolify uses the microphone only while you hold the microphone button, to turn your voice into text." Apple's
  rule is that a missing key terminates the app at first microphone access. Minimum iOS in Tauri
  config defaults to 15.0, which is above the 14.3 where WKWebView gained `getUserMedia`.
- **Desktop macOS** bundle needs the same key in `Info.plist` (Tauri documents this example for WebRTC).
- **Capabilities**: none needed for capture. Do not add a microphone plugin.
- **CSP** (`tauri.conf.json`): `connect-src` currently lists `http://localhost:8080` and PostHog; the
  upload to `POST /v1/voice/transcriptions` needs the production API origin there. No `media-src` or
  `blob:` is needed unless the client plays the recording back, which this change does not do.
- **wry version**: 0.57.1 includes the permission API added in wry 0.56.0 (WebView2, WKWebView,
  WebKitGTK and Android); with no handler set, the iOS/macOS delegate grants the web-layer request and
  leaves the decision to the OS prompt.

## Known pitfalls

- A Chromium `audio/mp4` recording is Opus, not AAC; the file extension and label must not be used to pick a decoder.
- Safari/WKWebView historically ignores bitrate options, so output size is whatever AAC default Safari picks;
  measure the Blob.
- Safari asks for permission again on each page load unless remembered; inside the Tauri iOS app the
  OS prompt appears once per install.
- `MediaRecorder` with no timeslice returns one blob at stop; fine for 30 s, but a crash loses the data (acceptable, nothing is stored).
- Streamed WebM from `MediaRecorder` lacks duration metadata; confirm each provider accepts it (see the
  transcription spike).
- WebKit ties capture prompts to a user gesture (general WebKit behaviour, not tested here): call `getUserMedia`
  first in the press handler, before any `await`, so the first press keeps its gesture.

## Sources

MDN `MediaRecorder.isTypeSupported`, "Using the MediaStream Recording API", `MediaDevices.getUserMedia`;
WebKit blog "MediaRecorder API" (11353) and "WebKit features in Safari 18.4"; Tauri 2 configuration
reference (`bundle.iOS.infoPlist`, `bundle.android`); Tauri docs (file-system plugin page for editing
`AndroidManifest.xml`, macOS bundle page for `NSMicrophoneUsageDescription`); wry `CHANGELOG.md`,
`RustWebChromeClient.kt` and `wry_web_view_ui_delegate.rs` at wry-v0.57.1; Android `WebChromeClient`
reference ("If this method isn't overridden, the permission is denied."); Android and Apple permission
reference pages (the Apple page fetched returned only its title, so the missing-key behaviour is from
general Apple documentation knowledge).
