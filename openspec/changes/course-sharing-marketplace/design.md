## Context

Terms from `CONTEXT.md`: **Username**, **Friend list**, **Course visibility**, **Share link**, **Course invite**, **Marketplace**, **Published course**, **Course copy**, **Pricing config**. Lessons are Motion HTML slides with narration (ADR 0009). The API owns all state (ADR 0002); the Engine stays stateless.

## Decisions

### Usernames and friends

- Canonical form: Unicode NFKC, lowercased; allowed `a-z 0-9 _ .`, 3 to 20 characters, starts with a letter, no two separators in a row, not ending in a separator. Stored as `username` (display case kept) and `username_canonical` with a unique index. Confusable and reserved checks run on the canonical form.
- Reserved: a config list (`admin`, `support`, `duolify`, `moderator`, `staff`, `help`, `security`, plus profanity and slur filter). Rename cooldown 30 days; the old name is held for 30 days so nobody can impersonate the previous owner immediately.
- Search: exact match and prefix, minimum 3 characters, 20 results, rate limited 30 per minute, only learners with `discoverable = true` (default true). A blocked pair never sees each other.
- Friend request limits (config): 20 pending outgoing, 50 per day, 500 friends. A declined request blocks the same sender for 7 days. Privacy settings: `discoverable`, `friend_requests_from` (`everyone | nobody`), `course_invites_from` (`friends | everyone | nobody`, default `friends`), `show_streak_to_friends`.

### Visibility and snapshots

- `courses.visibility` in `private | link | public`. Changing to `link` or `public` runs safety screening (below) on the new version. `public` also needs listing eligibility: title, description, category, language, at least `min_published_lessons` generated lessons (config, default 3), account age at least 3 days with a verified email.
- Snapshot `course_versions` is immutable: version number, content hash, outline, per-lesson slide manifest, Motion HTML, narration text, Lesson test questions and retry variant pool, narration audio assets, changelog text, screening verdict, created_at. Assets live in object storage keyed by content hash and are shared by all versions and copies (reference counted).
- Allowlist builder: only course content fields above. Never included: Learner profile, notes, bookmarks, Tutor chat, progress, Course instructions, learner-specific personalization. A snapshot scanner rejects any field outside the allowlist and flags emails, phone numbers and keys in text.
- Narration audio: shared as stored assets (ElevenLabs terms for redistribution to other users must be confirmed; open question).
- Republish creates version n+1 after screening. Enrolled Course copies keep their pinned version. The client shows "update available" with the changelog. Updating re-pins the copy: completed lessons stay completed matched by a stable `lesson_key`; lessons removed upstream stay in history as archived. A forked copy (steered) does not receive updates in v1.
- Unpublish (`private`) stops new enrollment, delists and kills the link; existing copies keep their pinned version. Owner deletes a course: same, plus content kept while any copy references it, purged 30 days after the last reference ends. Owner account deletion: attribution becomes "deleted user", personal data removed, snapshots kept for enrolled copies.

### Link, invite, enrollment

- Share link: opaque random token (128 bits, URL-safe), stored only as a SHA-256 hash. URL `https://<app-domain>/c/<token>`; app deep link `duolify://course/<token>` via universal links. Preview endpoint is public (no account): title, creator username, description, outline, up to 3 preview slides, enrollment count, version. `noindex`. Revoke kills the token at once; the owner can rotate to a new token.
- Course invite to a username or friend: states `pending | accepted | declined | expired | revoked`, expiry 30 days, limits 20 per day per sender, 1 pending per sender and course per recipient. Delivered by in-app notification and push (respecting settings). Accept enrolls.
- Enrollment creates `course_copies` (learner, course, pinned version, `forked_at_lesson` null). Progress, XP, streak, tests, notes and chat are per learner and live outside the snapshot.
- Reuse: lessons present in the pinned version are served from the snapshot at zero credits. Gating by Lesson test still applies to unlocking. Lessons beyond the owner's last generated one are generated on demand through `GenerateLesson` after the previous test is passed, stored in the learner's copy (not added to the published course), and charged to the enrolling learner at the active Pricing config (`lesson_content` + `narration`). The course page shows "N of M lessons ready; the rest are generated as you learn, costing Credits". Owner-pays is an open question.

### Marketplace

- Search: Postgres full-text on title, description, topic tags, with filters category (fixed taxonomy), language, and sorts `popular` (30-day enrollments weighted by completion rate), `new` (published_at), `rating` (Bayesian average with a minimum count). Cursor pagination, Redis cache for hot queries.
- Reviews: 1 to 5 stars, optional text up to 2,000 characters, one per learner per course, editable, tied to the version reviewed, allowed after completing N lessons (config, default 3). The owner cannot review their own course.
- Reports: reasons `unsafe`, `inappropriate`, `spam`, `copyright`, `misleading`, `other`; targets course, review, user. One open report per reporter and target. Auto-hide a course from listings when 3 distinct reporters (config) report it until a moderator decides.
- Moderation: role `moderator`; queue ordered by severity then age; target first response 48 hours; actions `dismiss | warn | delist | takedown | ban_publishing`; every action in an audit log; owner notified with a reason and can appeal once. Takedown: delisted, link dead, no new enrollment; for severe categories (`unsafe`, legal) existing copies are blocked too; otherwise existing copies keep access.
- Safety screening `ScreenCourse` runs on every version before it goes live. Layers: deterministic (URL denylist, PII patterns, size) then LLM classifier over title, description, outline and sampled lesson text and narration. Verdicts `pass | needs_review | block`. `block`: cannot publish. `needs_review`: queue before going live. `pass`: live (public still sampled by moderators). Fail closed: if screening is unavailable the publish stays `pending_screening`; learning is unaffected.
- Attribution: course page shows creator username and avatar, version, published date. Enrollment counts are aggregate integers only.

### Abuse limits (config)

Max 10 published courses per account, 3 publishes per course per day, 20 reviews per day, 10 reports per day, 30 enrollments per day, link previews rate limited per IP, username search as above.

### Proposed proto additions (not applied here)

```proto
rpc ScreenCourse(ScreenCourseRequest) returns (ScreenCourseResponse);
message ScreenCourseRequest {
  string title = 1; string description = 2; string language = 3;
  repeated UnitState outline = 4;
  repeated SampledLesson samples = 5;   // text of slides and narration, bounded
}
message ScreenCourseResponse {
  enum Verdict { VERDICT_UNSPECIFIED = 0; PASS = 1; NEEDS_REVIEW = 2; BLOCK = 3; }
  Verdict verdict = 1;
  repeated string category_codes = 2;   // e.g. "unsafe_instructions", "spam_links"
  Usage usage = 3;
}
rpc ScreenReview(ScreenReviewRequest) returns (ScreenReviewResponse);
```

### Reliability

Golden-set tasks `screen_course` and `screen_review`. Timeouts: course screening 120 s (background job), review screening 10 s. Screening is platform-paid with a per-version cost cap: sample at most 12 lessons or 60,000 input tokens.

### Data

Snapshots in object storage (versioned bucket, replicated per the backup spec); metadata in Postgres. Reports and moderation actions retained 3 years; screening results 1 year; reviews until deleted by author or account deletion (then anonymized: text removed, rating kept in aggregate).

## Risks

- Reusing the owner's lessons exposes learners to unreviewed generated content: mitigated by screening, reports and takedown.
- Ratings gaming: mitigated by the completion gate, one review per learner, account-age checks and moderator tools.
- Copyright: owner content is AI-generated from a topic, but owners can name copyrighted material; report flow includes `copyright`; a formal takedown process is an open question.

## Open questions

1. Paid courses and creator revenue share (future): pricing, payout, tax, store billing rules, refunds. Not specified; recorded only.
2. Owner-pays or shared-pays model for on-demand Lessons (v1 default: the enrolling Learner pays).
3. May ElevenLabs narration audio be redistributed to other learners under the current plan and terms (ADR 0009)?
4. Should Lessons a Learner generates beyond the snapshot flow back to the owner's next republish (opt-in)?
5. Formal copyright takedown (DMCA-style) process and designated agent.
6. Public course pages indexable by search engines, and CDN for previews.
7. Updates for forked (steered) Course copies.
8. Minimum age and parental rules for reviews, usernames and the Friend list.
