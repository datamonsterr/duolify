-- +goose Up
ALTER TABLE marketplace_listings ADD COLUMN hidden_at timestamptz;
CREATE TABLE content_reports (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  target_type text NOT NULL CHECK (target_type IN ('course','review','user')),
  -- Polymorphic and deliberately without a foreign key: a report outlives its target for the retention window.
  target_id uuid NOT NULL,
  course_id uuid,
  reporter_id uuid REFERENCES learners(id) ON DELETE SET NULL,
  reason text NOT NULL CHECK (reason IN ('unsafe','inappropriate','spam','copyright','misleading','other')),
  text text NOT NULL DEFAULT '' CHECK (char_length(text) <= 500),
  status text NOT NULL DEFAULT 'open' CHECK (status IN ('open','dismissed','actioned')),
  merge_count integer NOT NULL DEFAULT 0 CHECK (merge_count >= 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
-- One open report per reporter and target; an erased reporter (NULL) no longer occupies a slot.
CREATE UNIQUE INDEX content_reports_one_open ON content_reports(reporter_id,target_type,target_id) WHERE status='open';
CREATE INDEX content_reports_target ON content_reports(target_type,target_id,status);
CREATE INDEX content_reports_retention ON content_reports(created_at);
CREATE TABLE content_report_submissions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  reporter_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX content_report_submissions_daily ON content_report_submissions(reporter_id,created_at);
-- Read model for the moderator surface (E15-10). Ordered by severity, then age.
CREATE TABLE moderation_queue_items (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  target_type text NOT NULL CHECK (target_type IN ('course')),
  target_id uuid NOT NULL,
  severity integer NOT NULL CHECK (severity >= 0),
  report_count integer NOT NULL CHECK (report_count >= 0),
  status text NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved')),
  opened_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX moderation_queue_one_open ON moderation_queue_items(target_type,target_id) WHERE status='open';
CREATE INDEX moderation_queue_order ON moderation_queue_items(severity DESC,opened_at,id) WHERE status='open';
-- Erasing the reporter also clears the text, including restored rows and direct FK deletion.
-- +goose StatementBegin
CREATE FUNCTION erase_content_report_attribution() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.reporter_id IS NULL THEN
    NEW.text := '';
  END IF;
  RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER erase_content_report_attribution BEFORE UPDATE OF reporter_id ON content_reports
FOR EACH ROW EXECUTE FUNCTION erase_content_report_attribution();

-- +goose Down
DROP TABLE moderation_queue_items;
DROP TABLE content_report_submissions;
DROP TABLE content_reports;
DROP FUNCTION erase_content_report_attribution();
ALTER TABLE marketplace_listings DROP COLUMN hidden_at;
