## Decisions
Reports live in `content_reports`; the target is a course id, review id or learner id (users are reported by public username, so no learner id is exposed). A partial unique index allows one open report per reporter and target; a repeat submission merges into it (the first reason is kept, `merge_count` rises, text is only filled when empty). A counter table enforces the rolling 24-hour cap, taken under the reporter row lock as for reviews.

Distinct-reporter counting is race-safe: every course report locks the listing row `FOR UPDATE` before inserting and counting, so the transaction that crosses the threshold is unique. It sets `marketplace_listings.hidden_at` and upserts one open `moderation_queue_items` row (severity = highest configured reason severity, later reports raise it). Hidden courses are excluded from lists, the Course page, reviews and the owner relist cannot clear the flag; only moderation (E15-10) can. The Redis catalogue is invalidated after commit. Copies are untouched.

Report text is stored, bounded to 500 Unicode characters, never screened by a model, never emitted and never returned by any learner API. Erasing the reporter clears reporter and text by trigger; the report and its counted decision stay for three years.

Defaults (all configuration): reasons unsafe, inappropriate, spam, copyright, misleading, other; threshold 3; 10 per day; 500 characters; 1095 days retention. Severity: unsafe 100, copyright 80, inappropriate 60, misleading 40, spam 30, other 10.

## Data and backup
`content_reports`, `content_report_submissions` and `moderation_queue_items` join nightly dumps, export (own reports) and the restore anonymizer (`content_reports.text` redacted; other text columns reviewed safe). No AI behaviour is added, so evals and fallbacks are unchanged.
