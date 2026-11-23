## Why
Learners need a private way to flag unsafe, spammy or misleading courses, reviews and users, and the marketplace must stop promoting a course that several different people report while a moderator decides.

## What Changes
- Add session-owned private reports for a course, a review or a user (by public username) with a configured reason and optional bounded text. One open report per reporter and target (duplicates merge), ten submissions per rolling day.
- At three distinct reporters (configurable) hide the course from lists, the Course page and the Redis catalogue at once, and enqueue a moderation-queue row ordered by severity then age. Existing copies keep their content; no report deletes or blocks anything.
- Reporters are never revealed to the owner; report text is visible to no learner.
- Add the report entry points and sheet to the course page and review list (desktop and mobile).
- Catalogue `course_reported` and `course_hidden_by_reports` before the emitters.
- Include reports in nightly dumps, retention (three years), account export, erasure of reporter text and restore checks.
- Out of scope: moderation UI and actions (E15-10), appeals, takedown.
