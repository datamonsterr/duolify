-- Apply as a Postgres administrator to the migrated database. Supply the login credential separately.
BEGIN;

DO $role$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'duolify_edge_ro') THEN
    CREATE ROLE duolify_edge_ro NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE
      NOINHERIT NOREPLICATION NOBYPASSRLS;
  END IF;
  IF EXISTS (
    SELECT 1 FROM pg_auth_members
    WHERE member = 'duolify_edge_ro'::regrole OR roleid = 'duolify_edge_ro'::regrole
  ) THEN
    RAISE EXCEPTION 'duolify_edge_ro must have no role memberships';
  END IF;
  -- TEMPORARY is inherited from PUBLIC by default and cannot be revoked from one member alone.
  EXECUTE format('REVOKE TEMPORARY ON DATABASE %I FROM PUBLIC, duolify_edge_ro', current_database());
  EXECUTE format('GRANT CONNECT ON DATABASE %I TO duolify_edge_ro', current_database());
  EXECUTE format(
    'ALTER ROLE duolify_edge_ro IN DATABASE %I SET default_transaction_read_only = on', current_database()
  );
END;
$role$;

ALTER ROLE duolify_edge_ro NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
REVOKE ALL ON ALL TABLES IN SCHEMA edge_public FROM duolify_edge_ro;
GRANT USAGE ON SCHEMA edge_public TO duolify_edge_ro;
GRANT SELECT ON edge_public.marketplace_browse_v1, edge_public.marketplace_course_v1,
  edge_public.marketplace_rating_v1 TO duolify_edge_ro;

-- Fail before committing if the login owns an application relation, can access other
-- relations or can bypass the grants through an application SECURITY DEFINER routine.
DO $grants$
BEGIN
  IF EXISTS (
    SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
      AND n.nspname NOT LIKE 'pg_%'
      AND c.relkind IN ('r', 'p', 'v', 'm', 'f')
      AND (
        c.relowner = 'duolify_edge_ro'::regrole
        OR has_table_privilege('duolify_edge_ro', c.oid, 'INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER,MAINTAIN')
        OR has_any_column_privilege('duolify_edge_ro', c.oid, 'INSERT,UPDATE,REFERENCES')
        OR (has_any_column_privilege('duolify_edge_ro', c.oid, 'SELECT') AND NOT (
          n.nspname = 'edge_public' AND c.relname IN (
            'marketplace_browse_v1', 'marketplace_course_v1', 'marketplace_rating_v1'
          )
        ))
      )
  ) OR EXISTS (
    SELECT 1 FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
    WHERE n.nspname NOT IN ('pg_catalog', 'information_schema') AND n.nspname NOT LIKE 'pg_%'
      AND p.prosecdef
      AND has_schema_privilege('duolify_edge_ro', n.oid, 'USAGE')
      AND has_function_privilege('duolify_edge_ro', p.oid, 'EXECUTE')
  ) OR EXISTS (
    SELECT 1 FROM pg_namespace
    WHERE nspname NOT LIKE 'pg_%' AND has_schema_privilege('duolify_edge_ro', oid, 'CREATE')
  ) OR has_database_privilege('duolify_edge_ro', current_database(), 'CREATE,TEMPORARY')
  THEN
    RAISE EXCEPTION 'duolify_edge_ro has privileges outside the public projections';
  END IF;
END;
$grants$;

COMMIT;
