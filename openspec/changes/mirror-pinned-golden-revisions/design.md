# Design: Mirror pinned golden revisions for offline recovery

## Architecture & Flow

```
Live Eval Request (pinned revision SHA-1/SHA-256)
  │
  ├── 1. Check local cache (evals/.cache/{dataset}-{revision}.jsonl)
  │      └── Verify local SHA-256 checksum against .sha256 file
  │          ├── Valid -> use cached dataset (source: "cache")
  │          └── Corrupt -> reject corrupted cache and invalidate
  │
  ├── 2. Primary: Download from Hugging Face
  │      └── GET {base_url}/datasets/{dataset}/resolve/{revision}/golden.jsonl
  │          ├── Success -> compute SHA-256, write file + .sha256 to cache (source: "hf")
  │          └── Outage / Failure -> proceed to Mirror Fallback
  │
  └── 3. Fallback: Download from S3/RustFS Mirror (HF_GOLDEN_MIRROR_URL)
         └── GET {mirror_url}/.../golden.jsonl
             ├── Fetch and verify SHA-256 checksum (.sha256 or header)
             ├── Validate schema & cases (reject unrelated / malformed data)
             ├── Write verified dataset + .sha256 to cache (source: "mirror")
             └── Outage / Missing / Checksum Mismatch -> FAIL EXPLICITLY
```

## Key Decisions

1. **Pinned Revisions Only**: Revisions for remote evaluation must be exact 40-character hex commit hashes. Branch names like `main` or unpinned tags fail explicitly with an invalid revision error.
2. **Explicit Failure Over Silent Fallback**: When a live evaluation is requested (absence of `--offline`), an unavailable HF and missing/corrupt mirror will immediately fail. Mock fixtures are only used when `--offline` is explicitly passed.
3. **Integrity & Checksum Verification**: Every cached or mirrored artifact computes its SHA-256 digest. Corrupted caches or mismatched mirrors are rejected immediately with typed errors.
4. **Analytics & Metadata**: `eval_run_completed` includes `dataset_revision` and `dataset_source` properties. The analytics allowlist in `services/api/internal/analytics/analytics.go` permits these fields, retaining all privacy boundaries (zero learner text, reserved system UUID).
5. **Report Visibility**: Both JSON and Markdown evaluation reports display the dataset source (`hf`, `mirror`, `cache`, or `fixture`) alongside the pinned revision SHA.
