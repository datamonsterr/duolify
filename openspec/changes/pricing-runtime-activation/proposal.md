# Pricing runtime activation and rollback

E12-06 makes reviewed Pricing versions effective without a deployment. Postgres time selects the approved version; cancellation and activation history are append-only controls around immutable versions. Rollback copies an earlier version into a newly reviewed draft and preserves both its active parent and rollback source.

This change adds no AI behavior and does not alter quote, grant, reservation, or paid-period terms already accepted under a version.
