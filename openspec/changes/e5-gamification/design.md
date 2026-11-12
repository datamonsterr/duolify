# Design

Postgres configuration supplies base XP, first-try-perfect bonus, default goal and goal options. E4 retains first-try-perfect and elapsed test duration in the unique completion summary. The consumer joins that summary to its outbox, snapshots the learner's stored IANA timezone and completion-local date, and inserts one XP event per learner/lesson. A learner row lock serializes different lesson awards and goal edits; database uniqueness additionally prevents duplicate awards and goal markers under real concurrency.

Reads and completion requests consume pending work; a periodic API sweep resumes after outages. Failed transactions remain pending. Goal progress sums immutable XP events by their retained local dates. Goal edits use the existing profile daily_xp field, never a duplicate preference. A marker records the first goal crossing per learner/local date, including lowering a goal below existing progress. XP remains after course deletion for the life of the account.

Analytics uses the central fail-closed consent gate. The winning transaction emits xp_earned, lesson_completed (authoritative XP), and daily_goal_met after commit. Event identifiers support downstream deduplication; PostHog is best effort and never the recovery source. Capture errors are logged without rolling back a committed award. No learner text is transmitted.

Defaults: base 10 XP, first-try-perfect bonus 5 XP, daily options 10/20/30/50 XP, default 20, all stored in database config. Legacy summaries conservatively receive no perfect bonus. Streaks, freezes and league membership remain separate tickets; XP events provide their durable source. No AI behavior changes or eval changes.
