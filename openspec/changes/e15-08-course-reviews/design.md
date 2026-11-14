## Decisions
Reviews retain their initial pinned course version. PUT upserts one review; every text edit is screened before display. Only pass exposes text, including to the public payload; all other verdicts retain stars and show a neutral notice. DELETE removes the review contribution. Account deletion instead clears learner_id and text and retains the anonymous rating. Postgres transactions serialize per-actor submissions and per-course aggregates; screening runs outside database locks, then authorization and limits are checked again at commit. All numeric limits are configuration. Defaults: three completed lessons, 20 submissions/edits per rolling 24 hours including deleted reviews, 2000 Unicode characters, 4000 input/300 output tokens and a platform USD 0.02 cap that config may lower. Screening uses no learner Credits or reservation. At most two attempts share ten seconds and a platform USD cap, with usage emitted for each returned attempt. Missing usage/schema/provider fails closed.

## Data and backup
course_reviews and review submission counters join nightly dumps, export/deletion policy and synthetic restore fixtures. Every text column is classified by the restore anonymizer. Review screening is retained for one year; account deletion clears screening content and attribution. Aggregate stars survive account erasure.

## Reliability
Fixture golden rows pin an offline revision until live Hugging Face provisioning; recorded mock outputs cover abuse, spam, languages, injection and safe educational criticism. The existing model promotion gate continues to apply.
