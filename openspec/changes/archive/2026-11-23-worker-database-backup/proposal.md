# Worker database backup (E11-01)

## Why
The bootstrap worker cannot recover a database. Prove an encrypted nightly backup
against synthetic Postgres 18 data and local RustFS without cloud credentials.

## What Changes
- Add a nightly UTC River job with bounded retries, date/schema object names,
  streaming age encryption and atomic S3 uploads to a separate bucket.
- Package pinned dump/encryption tools in a worker image and isolate writer
  permissions from retention administration and the public API.
- Test real encrypted upload and restore, failure recovery and private analytics.

## Impact
API workers, local infrastructure, analytics catalogue and data inventory.
No AI behavior changes. Cloud provisioning, retention automation and production
restore drills remain E11 follow-up tickets.
