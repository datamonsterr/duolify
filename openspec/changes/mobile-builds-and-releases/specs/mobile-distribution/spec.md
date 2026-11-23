## ADDED Requirements

### Requirement: Mobile device verification
CI SHALL build an Android debug APK and iOS simulator app and exercise launch and landscape lesson playback on an API 34 emulator and an iPhone simulator. Android SHALL additionally exercise a signed-in learner against the disposable full-stack backend. Both jobs SHALL retain screenshots and be advisory until Android has three consecutive green runs.

#### Scenario: Mobile CI run
- **WHEN** client native code changes
- **THEN** both platforms are built and their device flows run with screenshot artifacts

### Requirement: Mobile release artifacts
A release SHALL include a signed Android APK with a stable download name and a versioned copy, a zipped iOS simulator app, checksums, changelog notes, and QR codes for the APK and the iPhone web app. Signing secrets SHALL remain external to the repository and APK certificate verification SHALL precede publication.

#### Scenario: Signing credentials unavailable
- **WHEN** a release is requested without all Android signing credentials
- **THEN** the release fails without publishing a debug-signed APK

#### Scenario: Dry run
- **WHEN** release validation is dispatched in dry-run mode
- **THEN** CI builds and verifies the would-be release artifacts without creating a tag or release

### Requirement: Native staging and sign-in
Mobile builds SHALL default to staging service origins using a platform CSP overlay, support the existing `duolify://` SSO callback and secure credential store, and declare microphone permissions.

#### Scenario: Mobile sign-in
- **WHEN** a learner completes SSO in the browser
- **THEN** the callback returns to the app and the session is stored securely
