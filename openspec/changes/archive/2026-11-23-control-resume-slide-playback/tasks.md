## 1. Acceptance and implementation
- [x] 1.1 Validate change and write failing e2e, integration, then unit tests.
- [x] 1.2 Implement parent clock, sandbox controls, safe events, accessible controls and resume API.
- [x] 1.3 Register telemetry, privacy/export and backup coverage.
- [x] 1.4 Run focused API/web checks and document skipped native checks.

Native checks are intentionally skipped on this host: client:lint native Cargo
format/Clippy, client:test native Cargo tests, and client:tauri-build. Web variants
and client:build cover this implementation locally; CI owns native verification.
