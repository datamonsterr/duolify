-- +goose Up
-- Daily check-in Credits (ADR 0020, owner decision 2026-11-22): attendance state, two new ledger kinds
-- (check_in_grant, check_in_bonus), the check_in allocation bucket, and the Postgres idempotency keys.

-- One attendance row per learner and learner-local date, written even when the capped grant is zero.
-- next_check_in_at is the frozen next local-midnight instant; a zone change cannot advance it.
CREATE TABLE credit_check_ins (
    learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
    local_date date NOT NULL,
    time_zone text NOT NULL,
    checked_at timestamptz NOT NULL,
    next_check_in_at timestamptz NOT NULL,
    granted bigint NOT NULL CHECK (granted >= 0),
    first_time boolean NOT NULL DEFAULT false,
    PRIMARY KEY (learner_id, local_date)
);
CREATE INDEX credit_check_ins_latest ON credit_check_ins (learner_id, checked_at DESC, local_date DESC);

ALTER TABLE credit_entries DROP CONSTRAINT credit_entries_kind_check;
ALTER TABLE credit_entries ADD CONSTRAINT credit_entries_kind_check
    CHECK (kind IN ('daily_grant', 'purchase', 'subscription_grant', 'spend', 'reserve', 'release', 'refund', 'expire', 'reward', 'check_in_grant', 'check_in_bonus'));

-- Defence in depth beside credit_check_ins' primary key: one regular grant per learner and local day,
-- and the first-time bonus exactly once per learner account.
CREATE UNIQUE INDEX credit_entries_check_in_grant_uniq ON credit_entries (learner_id, reference) WHERE kind = 'check_in_grant';
CREATE UNIQUE INDEX credit_entries_check_in_bonus_uniq ON credit_entries (learner_id) WHERE kind = 'check_in_bonus';

ALTER TABLE credit_allocations DROP CONSTRAINT credit_allocations_bucket_check;
ALTER TABLE credit_allocations ADD CONSTRAINT credit_allocations_bucket_check
    CHECK (bucket IN ('daily_free', 'check_in', 'subscription', 'purchase'));

-- +goose Down
-- Attendance rows and ledger history are financial records (seven-year retention); roll forward instead.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'check-in ledger history requires seven-year retention; use a forward migration'; END $$;
-- +goose StatementEnd
