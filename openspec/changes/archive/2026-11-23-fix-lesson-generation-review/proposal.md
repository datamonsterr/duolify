## Why
Review #161 found credit holds leaking on failed lesson generation, charges preceding persistence, unsafe repeat generation, recap disclosure and cosmetic retry variants.

## What Changes
- Release failed running reservations through trusted settlement; retry cleanup errors.
- Checkpoint validated private results and settle/persist atomically without repeating provider calls.
- Require ready_to_generate and a unique active generation; enqueue River in the same transaction.
- Scope recaps to their learner and reject invented or missing topics.
- Generate real paraphrases with the bounded engine provider path and core validators; reuse approved pools for free, never fabricate source echoes.

## Impact
Lesson-test API and engine, a new checkpoint column/index, full-row encrypted backups and account privacy, recorded offline E10 critical evals. No provider credentials, commercial prices, client changes or proto changes.
