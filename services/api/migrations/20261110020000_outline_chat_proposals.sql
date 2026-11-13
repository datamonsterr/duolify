-- +goose Up
-- Proposed development terms only. E12-09/E12-10 must approve commercial activation.
INSERT INTO pricing_versions(version,effective_from,approved,config)
SELECT 'usd-dev-outline-chat-unapproved',now(),false,
 jsonb_set(jsonb_set(jsonb_set(jsonb_set(jsonb_set(config,'{version}','"usd-dev-outline-chat-unapproved"'),'{effective_from}',to_jsonb(now())),
 '{actions,outline_chat_change}','{"credits":5}'::jsonb),
 '{daily_free,eligible_actions}',(config#>'{daily_free,eligible_actions}')||'"outline_chat_change"'::jsonb),
 '{limits,outline_chat_change}','{"input_tokens":12000,"output_tokens":2000,"timeout_ms":10000,"max_attempts":2,"max_cost_nanos":5000000}'::jsonb)
FROM pricing_versions WHERE version='usd-dev-course-brief-unapproved';
INSERT INTO pricing_quote_policies(version,policy)
SELECT 'usd-dev-outline-chat-unapproved',jsonb_set(policy,'{action_models,outline_chat_change}','"haiku"')
FROM pricing_quote_policies WHERE version='usd-quotes-2026-11-09';

ALTER TABLE outline_drafts ADD CONSTRAINT outline_draft_owner UNIQUE(id,learner_id);
CREATE TABLE outline_proposals (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
 draft_id uuid NOT NULL,
 quote_id uuid NOT NULL UNIQUE REFERENCES pricing_quotes(id),
 base_revision bigint NOT NULL CHECK(base_revision>0),
 base_outline jsonb NOT NULL CHECK(jsonb_typeof(base_outline)='array'),
 request text NOT NULL CHECK(length(request) BETWEEN 1 AND 1000),
 operations jsonb,
 result_outline jsonb,
 attempts jsonb,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','validated','delivered','failed')),
 decision text NOT NULL DEFAULT '' CHECK(decision IN ('','applied','discarded','undone')),
 applied_revision bigint,
 decision_draft jsonb,
 created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(draft_id,learner_id) REFERENCES outline_drafts(id,learner_id) ON DELETE CASCADE
);
CREATE INDEX outline_proposals_owner ON outline_proposals(learner_id,draft_id);
-- +goose StatementBegin
CREATE FUNCTION protect_outline_proposal() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.learner_id<>OLD.learner_id OR NEW.draft_id<>OLD.draft_id OR NEW.quote_id<>OLD.quote_id OR NEW.base_revision<>OLD.base_revision OR NEW.base_outline<>OLD.base_outline OR NEW.request<>OLD.request OR NEW.created_at<>OLD.created_at THEN RAISE EXCEPTION 'immutable proposal base'; END IF;
 IF OLD.state<>'pending' AND (NEW.operations IS DISTINCT FROM OLD.operations OR NEW.result_outline IS DISTINCT FROM OLD.result_outline OR NEW.attempts IS DISTINCT FROM OLD.attempts) THEN RAISE EXCEPTION 'immutable proposal diff'; END IF;
 IF (OLD.state='delivered' AND NEW.state<>'delivered') OR (OLD.state='failed' AND NEW.state<>'failed') THEN RAISE EXCEPTION 'terminal proposal'; END IF;
 IF OLD.state='validated' AND NEW.state='pending' THEN RAISE EXCEPTION 'validated proposal cannot restart'; END IF;
 IF NEW.decision IS DISTINCT FROM OLD.decision AND NOT ((OLD.decision='' AND NEW.decision IN ('applied','discarded') AND NEW.state='delivered') OR (OLD.decision='applied' AND NEW.decision='undone')) THEN RAISE EXCEPTION 'terminal proposal decision'; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER outline_proposals_immutable BEFORE UPDATE ON outline_proposals FOR EACH ROW EXECUTE FUNCTION protect_outline_proposal();

-- +goose Down
DROP TABLE outline_proposals;
DROP FUNCTION protect_outline_proposal();
ALTER TABLE outline_drafts DROP CONSTRAINT outline_draft_owner;
-- Immutable financial Pricing history is retained.
