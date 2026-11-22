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
