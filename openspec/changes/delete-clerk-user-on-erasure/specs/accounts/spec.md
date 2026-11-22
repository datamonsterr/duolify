# Accounts Specification

## Purpose

Remove a Learner's identity from Clerk when the Duolify account is erased, without making erasure depend on Clerk.

## Requirements

## ADDED Requirements

### Requirement: Account erasure deletes the linked Clerk user

When a Learner's account is erased, or an erasure tombstone is re-applied after a restore, the API SHALL queue deletion of every Clerk user bound to the Learner in the same transaction that deletes the Learner. A worker SHALL delete queued Clerk users through the Clerk Backend API and remove each request once Clerk accepted it or reported the user already deleted.

#### Scenario: Clerk is unreachable at erasure

- **WHEN** a Learner with a Clerk binding erases the account while Clerk is down
- **THEN** the Learner and the binding are erased, the request answers 200 without calling Clerk, and the Clerk deletion stays queued for retry

#### Scenario: Retry succeeds

- **WHEN** Clerk is reachable on a later worker run
- **THEN** the Clerk user is deleted once, the request leaves the queue, and a further run makes no call

### Requirement: Clerk deletion is idempotent and off without Clerk

A Clerk user already deleted (404) SHALL count as success. Any other failure SHALL keep the request queued and increment its attempt count. Without a Clerk secret key the worker SHALL NOT be scheduled and no Clerk call SHALL be made; requests SHALL wait until Clerk is configured. Logs and errors MUST NOT contain a Clerk user id.

#### Scenario: Clerk not configured

- **WHEN** the API runs without `CLERK_SECRET_KEY` and a learner with a stale Clerk binding is erased
- **THEN** no Clerk call is made and the request waits in the queue

#### Scenario: Already deleted at Clerk

- **WHEN** Clerk answers 404 for a queued user
- **THEN** the request is removed as done

### Requirement: External provider erasure survives database restore

Before acknowledging account erasure, the API SHALL durably append a recoverable encrypted Clerk subject to an immutable external ledger independent of SQL snapshots. Production startup SHALL replay these intents independently of restored learner/provider rows before listening. Ciphertext SHALL authenticate its object key; storage, decryption or replay failures SHALL prevent startup. Recovery SHALL retain the encryption key with the recovery secrets. Nonproduction dumps SHALL continue purging the SQL queue; no new analytics event or AI behavior is introduced.

#### Scenario: Restore predates linking or learner creation

- **WHEN** an accepted erasure's database queue and provider binding are absent from a restored snapshot
- **THEN** external replay recreates the Clerk deletion request even if the learner is also absent

### Requirement: Retry fairness and downgrade preservation

Failed requests SHALL remain durable with capped exponential backoff. Unattempted and least-recently attempted eligible requests SHALL run first in bounded batches that fit the worker deadline. Queue downgrade SHALL acquire an exclusive table lock and refuse to drop a nonempty queue.

#### Scenario: Permanently failing prefix

- **WHEN** 50 older deletions keep failing and a later deletion can succeed
- **THEN** later deletion is attempted while failures remain queued with backoff

#### Scenario: Rollback with pending erasures

- **WHEN** a downgrade would drop a queue with pending requests
- **THEN** the migration fails and preserves every identifier
