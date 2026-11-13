-- +goose Up
-- Rename, search and abuse limits are configuration, next to the E6-01 hold_days.
ALTER TABLE username_config
  ADD COLUMN rename_cooldown_days integer NOT NULL DEFAULT 30 CHECK (rename_cooldown_days BETWEEN 1 AND 365),
  ADD COLUMN search_max_results integer NOT NULL DEFAULT 20 CHECK (search_max_results BETWEEN 1 AND 50),
  ADD COLUMN search_per_minute integer NOT NULL DEFAULT 30 CHECK (search_per_minute BETWEEN 1 AND 600),
  ADD COLUMN name_attempts_per_hour integer NOT NULL DEFAULT 30 CHECK (name_attempts_per_hour BETWEEN 1 AND 1000);

-- Prefix search runs `canonical LIKE 'abc%'`; the default btree cannot serve that in a non-C locale.
CREATE INDEX learner_usernames_canonical_prefix_idx ON learner_usernames (canonical text_pattern_ops);

-- Fixed-window counters keep the API stateless and shared across instances. Disposable:
-- rows expire by window_start and cascade with the learner.
CREATE TABLE social_rate_limits (
  learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  bucket text NOT NULL CHECK (bucket IN ('search','username_attempt')),
  window_start timestamptz NOT NULL DEFAULT now(),
  count integer NOT NULL CHECK (count >= 0),
  PRIMARY KEY (learner_id, bucket)
);

-- +goose Down
DROP TABLE social_rate_limits;
DROP INDEX learner_usernames_canonical_prefix_idx;
ALTER TABLE username_config DROP COLUMN rename_cooldown_days, DROP COLUMN search_max_results, DROP COLUMN search_per_minute, DROP COLUMN name_attempts_per_hour;
