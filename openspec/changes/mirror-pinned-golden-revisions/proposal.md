# Mirror pinned golden revisions for offline recovery (E10-04)

## Why
When Hugging Face is unavailable during live evaluations or CI runs, evaluations must remain reproducible and resilient without compromising data integrity. Pinned immutable commit revisions of the Golden set must be mirrored to S3 object storage (RustFS locally), verified with SHA-256 checksums, and recovered automatically during an HF outage. Fallback datasets that are corrupt, unrelated, or from unverified sources must be explicitly rejected, and mock fixtures must never silently substitute for a requested live run.

## What Changes
- Enforce that dataset revisions for live/download evaluations are immutable 40-character hex commit hashes; unknown or non-pinned revisions fail explicitly.
- Add SHA-256 checksumming for downloaded and cached dataset revisions, verifying cache integrity and rejecting corrupted cached files.
- Add offline recovery from an S3 object-storage mirror (`HF_GOLDEN_MIRROR_URL` or `GOLDEN_MIRROR_URL`) when Hugging Face experiences an outage.
- Enforce mirror integrity: verify SHA-256 checksums of mirrored datasets and reject corrupt or unrelated fallback datasets.
- Ensure that mock fixtures remain deterministic and never silently substitute when a live run is requested.
- Record dataset revision and dataset source (`hf`, `mirror`, `cache`, or `fixture`) in JSON and Markdown eval reports.
- Emit `eval_run_completed` PostHog analytics with `dataset_revision` and `dataset_source` metadata, allowlisting them in `services/api/internal/analytics/analytics.go` and updating the analytics event catalogue.
- Add Golden set cases covering pinned source, failure recovery, and mirror integrity in `evals/fixtures/golden.sample.jsonl`.
- Update `docs/data-inventory.md`, `.env.example`, and ADR 0011 defaults.

## Impact
Touches `engine-eval`, eval fixtures, reports, `services/api` analytics allowlist, and documentation. No paid API calls or live external network requests are made in tests or CI.
