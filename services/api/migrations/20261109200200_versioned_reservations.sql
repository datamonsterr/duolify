-- +goose Up
-- Successor catalogue: old quotes/config stay immutable.
INSERT INTO pricing_versions(version,effective_from,approved,config)
SELECT 'usd-quotes-2026-11-09', '2026-11-09T00:00:00Z',true,
 jsonb_set(jsonb_set(jsonb_set(config,'{version}','"usd-quotes-2026-11-09"'),'{effective_from}','"2026-11-09T00:00:00Z"'),'{limits,free_concurrent_generation}','1')
FROM pricing_versions WHERE version='usd-launch-2026-11-08';
INSERT INTO pricing_quote_policies(version,policy)
SELECT 'usd-quotes-2026-11-09',jsonb_set(policy,'{ttl_seconds}','300')
 || '{"max_attempts":2,"free_concurrency":1,"fallback_models":{"sonnet":"sonnet_fallback","haiku":"haiku_fallback"}}'::jsonb
 || jsonb_build_object('models',policy->'models' || '{"sonnet_fallback":{"provider":"openrouter","model":"anthropic/claude-sonnet-5.5","context_max":100000,"input":2000,"output":10000,"cache_write":2500,"cache_read":100},"haiku_fallback":{"provider":"openrouter","model":"anthropic/claude-haiku-5.5","context_max":100000,"input":100,"output":500,"cache_write":125,"cache_read":10}}'::jsonb)
FROM pricing_quote_policies WHERE version='usd-launch-2026-11-08';

CREATE TABLE credit_jobs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 quote_id uuid NOT NULL UNIQUE REFERENCES pricing_quotes(id),
 learner_id uuid REFERENCES learners(id) ON DELETE SET NULL,
 owner_reference text NOT NULL,
 state text NOT NULL CHECK(state IN ('reserved','running','settled','released')),
 local_date date NOT NULL,
 platform_date date NOT NULL,
 held_nanos bigint NOT NULL CHECK(held_nanos>=0),
 result jsonb,
 created_at timestamptz NOT NULL DEFAULT now(),
 deadline_at timestamptz NOT NULL,
 finished_at timestamptz
);
CREATE INDEX credit_jobs_owner ON credit_jobs(learner_id,state);
CREATE INDEX credit_jobs_deadline ON credit_jobs(deadline_at) WHERE state IN ('reserved','running');
CREATE TABLE credit_allocations (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 job_id uuid NOT NULL REFERENCES credit_jobs(id),
 source_id bigint NOT NULL REFERENCES credit_entries(id),
 component text NOT NULL CHECK(component IN ('content','audio')),
 bucket text NOT NULL CHECK(bucket IN ('daily_free','subscription','purchase')),
 grant_date text NOT NULL DEFAULT '',
 amount bigint NOT NULL CHECK(amount>0),
 UNIQUE(job_id,source_id,component)
);
CREATE TABLE credit_usage (
 job_id uuid PRIMARY KEY REFERENCES credit_jobs(id),
 usage jsonb NOT NULL CHECK(jsonb_typeof(usage)='array'),
 cost_nanos bigint NOT NULL CHECK(cost_nanos>=0),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE credit_cost_counters (
 scope text NOT NULL CHECK(scope IN ('learner','platform')),
 owner_reference text NOT NULL,
 day date NOT NULL,
 held_nanos bigint NOT NULL DEFAULT 0 CHECK(held_nanos>=0),
 spent_nanos bigint NOT NULL DEFAULT 0 CHECK(spent_nanos>=0),
 PRIMARY KEY(scope,owner_reference,day)
);
CREATE TABLE credit_artifacts (
 owner_reference text NOT NULL,
 artifact_id text NOT NULL,
 component text NOT NULL,
 job_id uuid NOT NULL REFERENCES credit_jobs(id),
 PRIMARY KEY(owner_reference,artifact_id,component)
);
CREATE TABLE credit_outbox (
 event_key text PRIMARY KEY,
 learner_id uuid REFERENCES learners(id) ON DELETE SET NULL,
 owner_reference text NOT NULL,
 event text NOT NULL,
 properties jsonb NOT NULL,
 dispatched_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER credit_allocations_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON credit_allocations FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_mutation();
CREATE TRIGGER credit_usage_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON credit_usage FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_mutation();
CREATE TRIGGER credit_artifacts_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON credit_artifacts FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_mutation();
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'financial reservations require retention; use a forward migration'; END $$;
-- +goose StatementEnd
