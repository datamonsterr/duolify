-- +goose Up
-- UNAPPROVED proposal ONLY. E17-02 approval and E10-06 live evidence remain gates.
-- Existing approved prices and quote policies are immutable and unchanged.
INSERT INTO pricing_versions(version,effective_from,approved,config)
SELECT 'usd-dev-character-create-unapproved','2026-11-10T08:00:00Z',false,
 jsonb_set(jsonb_set(jsonb_set(jsonb_set(config,'{version}','"usd-dev-character-create-unapproved"'),'{effective_from}','"2026-11-10T08:00:00Z"'),'{actions,character_create}','{"credits":40}'),'{limits,character_create}',
 '{"input_tokens":4000,"output_tokens":2000,"timeout_ms":90000,"max_attempts":2,"max_cost_nanos":50000000,"count":10,"day_attempts":5,"minute_attempts":2,"prompt_chars":1500,"screen_input_tokens":18000,"screen_output_tokens":200,"screen_timeout_ms":10000,"screen_max_attempts":2}')
FROM pricing_versions WHERE version='usd-quotes-2026-11-09';
INSERT INTO pricing_quote_policies(version,policy)
SELECT 'usd-dev-character-create-unapproved',
 jsonb_set(jsonb_set(jsonb_set(jsonb_set(policy,'{models,character_mock}',(policy->'models'->'haiku')||'{"provider":"mock"}'::jsonb),'{models,character_mock_fallback}',(policy->'models'->'haiku')||'{"provider":"mock","model":"anthropic/claude-haiku-5.5"}'::jsonb),'{action_models,character_create}','"character_mock"'),'{fallback_models,character_mock}','"character_mock_fallback"')
FROM pricing_quote_policies WHERE version='usd-quotes-2026-11-09';
CREATE TABLE character_create_jobs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
 request_key text NOT NULL CHECK(length(request_key) BETWEEN 1 AND 100),
 character_id uuid REFERENCES characters(id) ON DELETE SET NULL,
 expected_version integer NOT NULL DEFAULT 0 CHECK(expected_version>=0),
 operation text NOT NULL CHECK(operation IN ('create','regenerate')),
 state text NOT NULL CHECK(state IN ('screening','quoted','queued','running','succeeded','failed','rejected','cancelled')),
 screen_cost_nanos bigint NOT NULL DEFAULT 0 CHECK(screen_cost_nanos>=0),
 reason_code text NOT NULL DEFAULT '',
 quote_id uuid REFERENCES pricing_quotes(id),
 reservation_id uuid REFERENCES credit_jobs(id),
 prompt text CHECK(length(prompt)<=1500),
 prompt_expires_at timestamptz NOT NULL DEFAULT now()+interval '24 hours',
 deadline_at timestamptz,
 result jsonb,
 created_at timestamptz NOT NULL DEFAULT now(),
 finished_at timestamptz,
 UNIQUE(learner_id,request_key)
);
ALTER TABLE character_screenings ADD COLUMN operation_id uuid REFERENCES character_create_jobs(id) ON DELETE CASCADE;
CREATE INDEX character_create_quota ON character_create_jobs(learner_id,created_at);
CREATE INDEX character_create_pending ON character_create_jobs(state,deadline_at) WHERE state IN ('screening','quoted','queued','running');
-- +goose Down
ALTER TABLE character_screenings DROP COLUMN operation_id;
DROP TABLE character_create_jobs;
-- Immutable financial config remains for seven-year audit history.
