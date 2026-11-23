# Award completion XP and daily goals

E5-01 consumes E4's authoritative completion outbox and retained summaries into immutable XP events. Learners choose a configured daily XP goal and see server-backed progress on completion and the progress screen. Effort never grants Credits (ADR 0007).

No engine, provider or protobuf changes. Nightly encrypted Postgres dumps cover XP and goal markers; Redis/analytics remain disposable projections. Account export includes the new learner records and deletion cascades erase them.
