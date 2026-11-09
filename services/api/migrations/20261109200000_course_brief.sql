-- +goose Up
INSERT INTO pricing_versions(version,effective_from,approved,config)
SELECT 'usd-dev-course-brief-unapproved',now(),false,
 jsonb_set(jsonb_set(jsonb_set(jsonb_set(config,'{version}','"usd-dev-course-brief-unapproved"'),'{effective_from}',to_jsonb(now())),
 '{actions,course_brief_chat}','{"credits":0}'::jsonb),
 '{limits,course_brief_chat}','{"input_tokens":2000,"output_tokens":1000,"attempt_timeout_ms":3000,"max_attempts":3,"max_cost_nanos":2100000,"input_nanos_per_token":100,"output_nanos_per_token":500,"requests_per_hour":30,"platform_requests_per_day":10000}'::jsonb)
FROM pricing_versions WHERE version='usd-dev-unapproved';
INSERT INTO pricing_quote_policies(version,policy)
SELECT 'usd-dev-course-brief-unapproved',jsonb_set(policy,'{action_models,course_brief_chat}','"haiku"')
FROM pricing_quote_policies WHERE version='usd-dev-unapproved';

CREATE TABLE course_brief_revisions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
 brief jsonb NOT NULL CHECK (jsonb_typeof(brief)='object'),
 content_hash text NOT NULL CHECK (length(content_hash)=64),
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL DEFAULT now()+interval '30 days'
);
CREATE INDEX course_brief_revisions_owner ON course_brief_revisions(learner_id,expires_at);
-- +goose StatementBegin
CREATE FUNCTION immutable_course_brief() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'confirmed course brief revisions are immutable'; END $$;
-- +goose StatementEnd
CREATE TRIGGER course_brief_no_update BEFORE UPDATE ON course_brief_revisions
 FOR EACH ROW EXECUTE FUNCTION immutable_course_brief();

-- +goose Down
DROP TABLE course_brief_revisions;
DROP FUNCTION immutable_course_brief();
-- Pricing history is retained; use a forward migration to retire dev config.
