## Why
Screened, published Courses need a small public catalogue so a Learner can find a Course by topic, category and language and enroll for free. Publication to the Marketplace stays behind a release flag that is OFF by default until moderation (E15-10) and takedown (E15-11) ship and legal approves (ADR 0012).

## What Changes
- Owner routes `GET|PUT|DELETE /v1/courses/{id}/marketplace-listing` (actor from session) list or delist the latest passing publication draft with category, language and up to 5 topic tags. Eligibility: verified email, account age at least 3 days, at least 3 generated Lessons, title, description, category and language present, adult publisher; every number is configuration.
- Public, anonymous-safe, per-IP rate limited reads: `GET /v1/marketplace/courses` (Postgres full-text `q`, `category`, `language`, `sort=popular|new|rating`, opaque `cursor`, `limit`) and `GET /v1/marketplace/courses/{id}` (attribution, version, outline, up to 3 allowlisted preview Slides reused from E15-02, ready vs total Lessons, aggregate enrollment count, rating average and count).
- `POST /v1/marketplace/courses/{id}/enroll` reuses the E15-03 free pinned-copy enrollment (source `marketplace`); no paid courses and no revenue share.
- Table `marketplace_listings` (+ derived `marketplace_listing_stats`) holds listing metadata, the `tsvector` and ranking inputs. Ratings arrive in E15-08; the Bayesian rating ranking accepts zero ratings now.
- Redis caches result pages only. It is derived: loss rebuilds from Postgres, and a delist, relist or publish deletes every cached page immediately.
- A blocked pair never sees each other's listings when the viewer is signed in (`social.NotBlockedSQL`, `BlockedEitherWay`).
- Release flag `MARKETPLACE_ENABLED` (default false). With the flag off, every marketplace route answers a neutral 404 and `GET /v1/features` reports `marketplace_enabled:false`; tests enable it explicitly.
- Client: Marketplace screen (search, category chips, sort, cards, load more, loading, empty with "Create this course", offline/error with Retry, long content) and Course page (attribution, version, outline, preview Slides, ready vs paid Lessons, Enroll), desktop and mobile, per `docs/design/canvas/E15-Market.dc.html`. The nav entry appears only when the flag is on.
- Analytics: `marketplace_browsed`, `marketplace_searched` (query_length only), `course_viewed`.

## Out of scope
Ratings and reviews (E15-08), reports (E15-09), moderation and takedown (E15-10, E15-11), updates (E15-05), invites (E15-06), paid courses, revenue share, search-engine indexing.
