## MODIFIED Requirements

### Requirement: Character data backup and retention
Character ownership, immutable versions, screening verdicts, Course Casts and Lesson/chat/published Character snapshots or references MUST live in Postgres and be included in encrypted nightly dumps with 14-day retention. Any S3 rig/compiled SVG assets and references MUST use versioning where supported and have nightly encrypted checksum-pinned protected asset copies/manifests; R2 cannot recover overwritten versions from version history, restored together with the database. Unreferenced private records/assets MUST be purged within 30 days of Character/account deletion; immutable Lesson and published references MUST follow Course/E15 retention and reference-counted asset deletion. Raw prompt job payloads MUST expire within 24 hours, screening metadata within 30 days, and financial references under existing ledger retention. Restores MUST reapply deletions/takedowns before serving data.

#### Scenario: Restore Characters and Casts
- **WHEN** a nightly database and asset backup is restored
- **THEN** selected and snapshotted Characters resolve to the same validated specs, rigs and voices without paid regeneration

#### Scenario: Delete Character with a published reference
- **WHEN** the owner deletes a Character whose approved content is in an enrolled published snapshot
- **THEN** it disappears from future private selections while the published payload remains until E15's last-reference retention ends
