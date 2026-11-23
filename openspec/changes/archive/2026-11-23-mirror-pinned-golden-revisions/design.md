# Design: Mirror pinned golden revisions for offline recovery

## Architecture & Flow

```
Live Eval Request (pinned revision SHA-1/SHA-256)
  │
  ├── 1. Check local cache (evals/.cache/{dataset}-{revision}.jsonl)
  │      └── Require .sha256; verify bytes and sidecar against repository digest pin
  │          ├── Valid -> use cached dataset (source: "cache")
  │          └── Corrupt / missing sidecar -> fail explicitly; remove incomplete cache before refetch
  │
  ├── 2. Primary: Download from Hugging Face
  │      └── GET {base_url}/datasets/{dataset}/resolve/{revision}/golden.jsonl
  │          ├── Success -> verify exact bytes against repository digest pin, write file + .sha256 to cache (source: "hf")
  │          └── Network / timeout / 5xx -> Mirror Fallback; client / integrity error -> FAIL
  │
  └── 3. Fallback: Download from S3/RustFS Mirror (HF_GOLDEN_MIRROR_URL)
         └── GET {mirror_url}/.../golden.jsonl
             ├── Require checksum (.sha256 or header); verify bytes against repository digest pin
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

## Issue #160 defect 5 hardening

`evals/pinned-datasets.toml` is a Git-reviewed dataset/revision to SHA-256 map,
embedded in the Rust binary. Missing pins fail before I/O. Synthetic doubles have
separate fixture identities until E10-01 production provisioning; no production
hash is inferred from untrusted objects. An existing cache with a missing sidecar
fails explicitly; recovery removes the incomplete cache and downloads verified
bytes. Matching cache/mirror sidecars cannot legitimize bytes that mismatch the
repository pin. Mirror objects without checksum metadata fail even when their
bytes match the pin. HF network failures, timeouts (including HTTP 408) and 5xx
permit mirror recovery; all other client and integrity failures stop immediately.
Both HTTP clients have bounded timeouts. Hashing uses response bytes before UTF-8
conversion, avoiding implicit decoding changes.

Reports carry `dataset_verified`, false for custom/offline inputs, missing
configuration/secrets and older reports. Only a successful repository digest
comparison marks a fetched dataset verified. `check_promotion_gate` refuses false
or absent verification, and `live_promoted` uses that same gate. The JSON/Markdown
report contract changes without adding analytics fields or learner data.
Existing content-free `eval_run_completed` analytics and 90-day report retention
remain in place. No model behavior changes or live provider calls are required;
regressions use fake HF/RustFS and recorded model responses only. Repo pins are
Git-backed; mirror objects retain immutable revision keys and protected versioned
copies; caches remain disposable and require re-verification after restore.
