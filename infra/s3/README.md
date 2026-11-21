# S3 conformance

`mise run infra:s3-conformance` checks that an S3-compatible store supports every
operation the product relies on in `services/api/internal/backup` and
`services/api/internal/player`. Any store that replaces the current one must pass
it before data moves (spec: `infra-hosting`, "Object storage conformance before
migration"; ADR 0014).

| Operation | What it proves |
|---|---|
| bucket versioning | `GetBucketVersioning` reports `Enabled` |
| versioned put | each put returns a distinct version ID; `HeadObject` by version works |
| list versions | `ListObjectVersions` returns both versions, newest is latest |
| get by version | `GetObject` with `VersionId` returns the old body |
| delete-marker restore | `DeleteObject` makes a marker, reads give `NoSuchKey`, deleting the marker restores the object |
| conditional create | `If-None-Match: *` succeeds on a new key and returns HTTP 412 on an existing one |
| retention lock | a governance-locked object on the deletion-ledger prefix cannot be deleted by version |

The run prints one `PASS`/`FAIL` line per operation, names the failing operation,
exits non-zero on any failure, and replaces both credentials with `[redacted]` in
all output. The table lives in `services/api/internal/s3conformance`; a unit test
fails if backup or player code starts using an S3 call that the table does not
cover (or that `ExemptCalls` does not explain).

## Local (compose RustFS)

With no `S3_CONFORMANCE_ENDPOINT` set, the task starts the compose `objectstore`
and uses its root credentials:

```sh
mise run infra:s3-conformance
```

Result on `rustfs/rustfs:1.0.4`: all seven operations pass, including retention lock.

## Hosted stores

Set the variables (see `.env.example`) and run the same task. Credentials come from
the owner's secret store and are never committed.

```sh
S3_CONFORMANCE_ENDPOINT=https://hel1.your-objectstorage.com \
S3_CONFORMANCE_REGION=hel1 \
S3_CONFORMANCE_ACCESS_KEY_ID=... S3_CONFORMANCE_SECRET_ACCESS_KEY=... \
mise run infra:s3-conformance
```

- **Assets**: Hetzner Object Storage `hel1` (Helsinki).
- **Backup/DR bucket**: Hetzner Object Storage in Falkenstein or Nuremberg; use that
  location's endpoint and region.
- Leave `S3_CONFORMANCE_BUCKET` unset: the suite creates its own object-lock bucket
  (`s3conf-<random>`), enables versioning and deletes everything it made. The key
  needs bucket create/delete, versioning, object lock and `BypassGovernanceRetention`
  rights. If you only have a pre-made bucket, set `S3_CONFORMANCE_BUCKET`: the suite
  then writes under `s3-conformance/<random>/`, leaves versioning untouched (a
  bucket with versioning off fails by design) and never deletes the bucket. Retention
  lock needs a bucket created with object lock.
- If cleanup cannot remove something (a key without `BypassGovernanceRetention` cannot delete the locked test object), the task prints a `WARN` line naming the leftover bucket or keys with version IDs and still exits 0; delete them by hand, or wait: the lock expires after one hour.
- Use `S3_CONFORMANCE_PATH_STYLE=false` if the provider requires virtual-hosted style.
- Runs against the real Hetzner stores are tracked in #238 (owner-provided accounts).

## Fallback

If Hetzner Object Storage fails any operation, RustFS on a Hetzner Volume is the
documented fallback. Run this suite against that RustFS endpoint too; it must pass
before data moves.
