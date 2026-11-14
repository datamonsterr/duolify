-- +goose Up
CREATE TABLE course_reviews (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  course_id uuid NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
  learner_id uuid REFERENCES learners(id) ON DELETE SET NULL,
  version integer NOT NULL CHECK (version > 0),
  stars integer NOT NULL CHECK (stars BETWEEN 1 AND 5),
  text text NOT NULL DEFAULT '' CHECK (char_length(text) <= 2000),
  verdict text NOT NULL DEFAULT 'pending_screening' CHECK (verdict IN ('pass','needs_review','block','pending_screening')),
  operation_id uuid NOT NULL DEFAULT gen_random_uuid(),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (learner_id,course_id)
);
CREATE INDEX course_reviews_page ON course_reviews(course_id,created_at DESC,id DESC);
CREATE TABLE course_review_submissions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX course_review_submissions_daily ON course_review_submissions(learner_id,created_at);
CREATE TABLE course_review_screenings (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  review_id uuid NOT NULL REFERENCES course_reviews(id) ON DELETE CASCADE,
  learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  verdict text NOT NULL CHECK (verdict IN ('pass','needs_review','block','pending_screening')),
  reason_codes text[] NOT NULL DEFAULT '{}',
  policy_version text NOT NULL DEFAULT 'review-v1' CHECK (policy_version='review-v1'),
  attempts integer NOT NULL CHECK (attempts >= 0),
  fallback_used boolean NOT NULL,
  cost_nanos bigint NOT NULL CHECK (cost_nanos >= 0),
  created_at timestamptz NOT NULL DEFAULT now()
);
-- Attribution erasure also clears text, including restored rows and direct FK deletion.
-- +goose StatementBegin
CREATE FUNCTION erase_course_review_attribution() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.learner_id IS NULL THEN
    NEW.text := '';
    NEW.verdict := 'pending_screening';
  END IF;
  RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER erase_course_review_attribution BEFORE UPDATE OF learner_id ON course_reviews
FOR EACH ROW EXECUTE FUNCTION erase_course_review_attribution();
-- Atomic deltas serialize on the stats row, so concurrent edits/deletes never lose ratings.
-- +goose StatementBegin
CREATE FUNCTION update_course_review_rating() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    UPDATE marketplace_listing_stats SET rating_sum=rating_sum+NEW.stars,rating_count=rating_count+1 WHERE course_id=NEW.course_id;
  ELSIF TG_OP = 'DELETE' THEN
    UPDATE marketplace_listing_stats SET rating_sum=rating_sum-OLD.stars,rating_count=rating_count-1 WHERE course_id=OLD.course_id;
  ELSE
    UPDATE marketplace_listing_stats SET rating_sum=rating_sum+NEW.stars-OLD.stars WHERE course_id=NEW.course_id;
  END IF;
  RETURN NULL;
END $$;
-- +goose StatementEnd
CREATE TRIGGER course_review_rating_insert AFTER INSERT ON course_reviews FOR EACH ROW EXECUTE FUNCTION update_course_review_rating();
CREATE TRIGGER course_review_rating_edit AFTER UPDATE OF stars ON course_reviews FOR EACH ROW EXECUTE FUNCTION update_course_review_rating();
CREATE TRIGGER course_review_rating_delete AFTER DELETE ON course_reviews FOR EACH ROW EXECUTE FUNCTION update_course_review_rating();

-- +goose Down
DROP TABLE course_review_screenings;
DROP TABLE course_review_submissions;
DROP TABLE course_reviews;
DROP FUNCTION update_course_review_rating();
DROP FUNCTION erase_course_review_attribution();
