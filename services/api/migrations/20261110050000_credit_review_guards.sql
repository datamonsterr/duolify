-- +goose Up
-- All profile writers (including setup, settings and new accounts) share the
-- same transactional timezone policy. Pin the old zone before saving a new one.
-- +goose StatementBegin
CREATE FUNCTION schedule_credit_timezone(p_learner uuid, p_previous text, p_next text, p_requested timestamptz)
RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM credit_timezones WHERE learner_id=p_learner AND effective_at<=p_requested) THEN
        INSERT INTO credit_timezones(learner_id,time_zone,effective_at)
        VALUES(p_learner,COALESCE(NULLIF(p_previous,''),'Asia/Ho_Chi_Minh'),'1970-01-01T00:00:00Z');
    END IF;
    IF p_previous IS DISTINCT FROM p_next THEN
        INSERT INTO credit_timezones(learner_id,time_zone,effective_at)
        VALUES(p_learner,COALESCE(NULLIF(p_next,''),'Asia/Ho_Chi_Minh'),p_requested+interval '24 hours');
    END IF;
END;
$$;
-- +goose StatementEnd

-- Retain any earlier daily-grant zone when initializing pre-existing accounts.
INSERT INTO credit_timezones(learner_id,time_zone,effective_at)
SELECT p.learner_id,COALESCE(
    (SELECT NULLIF(e.provenance->>'timezone','') FROM credit_entries e
     WHERE e.learner_id=p.learner_id AND e.kind='daily_grant' ORDER BY e.id LIMIT 1),
    NULLIF(p.time_zone,''),'Asia/Ho_Chi_Minh'),'1970-01-01T00:00:00Z'
FROM learner_profiles p WHERE NOT EXISTS(
    SELECT 1 FROM credit_timezones z WHERE z.learner_id=p.learner_id AND z.effective_at<=now());

-- +goose StatementBegin
CREATE FUNCTION track_profile_credit_timezone() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        PERFORM schedule_credit_timezone(NEW.learner_id,NEW.time_zone,NEW.time_zone,clock_timestamp());
    ELSE
        PERFORM schedule_credit_timezone(NEW.learner_id,OLD.time_zone,NEW.time_zone,clock_timestamp());
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER learner_profile_credit_timezone
    BEFORE INSERT OR UPDATE OF time_zone ON learner_profiles
    FOR EACH ROW EXECUTE FUNCTION track_profile_credit_timezone();

-- Account erasure and restore startup use retained job ownership. No quote,
-- profile or timezone read may be required after the learner link is severed.
-- +goose StatementBegin
CREATE FUNCTION release_deleted_credit_jobs(p_owner_reference text) RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    j record;
    cost bigint;
    final_result jsonb;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('credits:financial',0));
    FOR j IN SELECT jobs.*,q.pricing_version FROM credit_jobs jobs
        JOIN pricing_quotes q ON q.id=jobs.quote_id
        WHERE jobs.owner_reference=p_owner_reference AND jobs.state IN ('reserved','running')
        FOR UPDATE OF jobs
    LOOP
        cost := CASE WHEN j.state='running' THEN j.held_nanos ELSE 0 END;
        INSERT INTO credit_entries(learner_id,owner_reference,amount,kind,reference,pricing_version,provenance)
        SELECT j.learner_id,j.owner_reference,a.amount,'release',
               j.id::text||':'||a.component||':'||a.source_id::text,j.pricing_version,
               jsonb_build_object('source_id',a.source_id::text,'component',a.component,'bucket',a.bucket,'date',a.grant_date)
        FROM credit_allocations a WHERE a.job_id=j.id;
        INSERT INTO credit_usage(job_id,usage,cost_nanos) VALUES(j.id,'[]',cost);
        UPDATE credit_cost_counters SET held_nanos=held_nanos-j.held_nanos,spent_nanos=spent_nanos+cost
        WHERE (scope='learner' AND owner_reference=j.owner_reference AND day=j.local_date)
           OR (scope='platform' AND owner_reference='platform' AND day=j.platform_date);
        final_result := jsonb_build_object('id',j.id,'pricing_version',j.pricing_version,'state','released',
                                          'content_spent',0,'audio_spent',0,'cost_nanos',cost);
        UPDATE credit_jobs SET state='released',result=final_result,finished_at=clock_timestamp() WHERE id=j.id;
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- Cover direct erasure too (restore fixtures and identity cascades). The FK's
-- SET NULL action must not bypass the ledger/timezone anonymization guard.
-- +goose StatementBegin
CREATE FUNCTION prepare_credit_account_deletion() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE ref text;
BEGIN
    ref := encode(sha256(convert_to(OLD.id::text,'UTF8')),'hex');
    PERFORM release_deleted_credit_jobs(ref);
    UPDATE credit_entries SET learner_id=NULL,owner_reference=ref WHERE learner_id=OLD.id;
    UPDATE credit_timezones SET learner_id=NULL,owner_reference=ref WHERE learner_id=OLD.id;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER learner_credit_account_deletion
    BEFORE DELETE ON learners FOR EACH ROW EXECUTE FUNCTION prepare_credit_account_deletion();

-- Repair jobs already anonymized by an earlier version, without reconstructing identity.
-- +goose StatementBegin
DO $$
DECLARE owner text;
BEGIN
    FOR owner IN SELECT DISTINCT owner_reference FROM credit_jobs
        WHERE learner_id IS NULL AND state IN ('reserved','running')
    LOOP
        PERFORM release_deleted_credit_jobs(owner);
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'financial history requires retention; use a forward migration'; END $$;
-- +goose StatementEnd
