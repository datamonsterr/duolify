# Mobile builds and releases

## Why
Learners cannot install a native mobile build, and CI does not exercise the Tauri client on mobile devices.

## What changes
- Build and exercise Android and iOS simulator clients in advisory CI jobs, with screenshots and a full-stack Android learner flow.
- Generate native projects in CI, provide platform-specific staging CSP and permissions, and retain existing native SSO and secure storage.
- Prepare versioned releases from Conventional Commits and publish a verified release-signed APK, simulator build, checksums and offline-generated QR codes.

## Impact
- Areas: client, CI, repository tooling and release documentation.
- Data backup: no new learner data; existing server backup and recovery remain authoritative. Signing keys are externally managed GitHub secrets and must be backed up by the owner outside the repository.
- Analytics: existing client events carry platform and version under the analytics privacy and opt-out rules; no new event is needed.
- Reliability: device checks are initially advisory; promote Android after three consecutive green runs. Fail closed on missing signing secrets or certificate mismatch. AI behavior is unchanged, so no model evals are added.
- No Apple Developer account, store submission, production deploy, or migration is required.
