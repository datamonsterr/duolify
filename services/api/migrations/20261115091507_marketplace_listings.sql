-- +goose Up
-- Public Marketplace listing metadata and ranking inputs (E15-07). Postgres is the source of
-- truth; the Redis result-page cache is derived and rebuilds. Title and description are copied
-- from the latest passing publication draft, never from client input.
CREATE TABLE marketplace_listings (
  course_id uuid PRIMARY KEY REFERENCES courses(id) ON DELETE CASCADE,
  owner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  draft_id uuid REFERENCES publication_drafts(id) ON DELETE SET NULL,
  version integer NOT NULL CHECK (version >= 1),
  title text NOT NULL CHECK (length(trim(title)) > 0),
  description text NOT NULL CHECK (length(trim(description)) > 0),
  category text NOT NULL CHECK (category IN ('science','tech','languages','life_skills','arts')),
  language text NOT NULL CHECK (language ~ '^[a-z]{2,3}$'),
  tags text[] NOT NULL DEFAULT '{}' CHECK (cardinality(tags) <= 10),
  lesson_count integer NOT NULL CHECK (lesson_count >= 0),
  generated_lessons integer NOT NULL CHECK (generated_lessons >= 0),
  -- 'simple' configuration: courses are multilingual, so no stemming. Title A, tags B, description C.
  search tsvector NOT NULL,
  published_at timestamptz NOT NULL DEFAULT now(),
  delisted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX marketplace_listings_search_idx ON marketplace_listings USING gin (search) WHERE delisted_at IS NULL;
CREATE INDEX marketplace_listings_new_idx ON marketplace_listings (category, language, published_at DESC, course_id DESC) WHERE delisted_at IS NULL;
CREATE INDEX marketplace_listings_owner_idx ON marketplace_listings (owner_id) WHERE delisted_at IS NULL;

-- Ranking inputs. rating_sum and rating_count are written by the ratings slice (E15-08) and are
-- zero until then; the Bayesian rating accepts zero ratings.
CREATE TABLE marketplace_listing_stats (
  course_id uuid PRIMARY KEY REFERENCES marketplace_listings(course_id) ON DELETE CASCADE,
  enrollments_30d integer NOT NULL DEFAULT 0 CHECK (enrollments_30d >= 0),
  completion_rate double precision NOT NULL DEFAULT 0 CHECK (completion_rate BETWEEN 0 AND 1),
  popular_score double precision NOT NULL DEFAULT 0 CHECK (popular_score >= 0),
  rating_sum bigint NOT NULL DEFAULT 0 CHECK (rating_sum >= 0),
  rating_count integer NOT NULL DEFAULT 0 CHECK (rating_count >= 0),
  refreshed_at timestamptz NOT NULL DEFAULT now(),
  CHECK (rating_sum <= rating_count::bigint * 5)
);

-- +goose Down
DROP TABLE marketplace_listing_stats;
DROP TABLE marketplace_listings;
