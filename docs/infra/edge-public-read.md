# Public-read Worker database access

CF-03 uses Hyperdrive for eligible anonymous Marketplace reads. The Go origin serves share authorization, the authored demo, viewer-specific requests and the fallback. Local conformance runs against migrated Postgres 18 and the genuine read-only role using the same Go fixtures. Tunnel, TLS, Access and hosted rate limiting are separate owner release checks.

## Provision the database role

Apply Goose migration `20261127021911_edge_marketplace_projections.sql` with the API migration user. The migration creates three versioned public projections in `edge_public`. They expose allowlisted Course cards, public creator names, aggregate catalogue statistics, and the same outline and sample slides as Go. Learner IDs, private rows, raw snapshots, answers, narration and link credentials are excluded.

Run [provision-marketplace-reader.sql](../../infra/edge/provision-marketplace-reader.sql) as the Postgres administrator against the migrated database. Goose runs in application and restore environments without a guarantee of cluster-wide `CREATEROLE` access. The runbook grants the dedicated role database `CONNECT`, schema `USAGE`, and `SELECT` on the three views. It sets default transactions read-only, verifies effective privileges and removes the database's default `PUBLIC` temporary-table grant. If another role uses temporary tables, grant that role `TEMPORARY` explicitly before applying the runbook.

The runbook checks for application security-definer functions executable by the reader. Set the role's login password through the authorized secrets process, then enable login. The integration fixture applies the same SQL and verifies base-table reads and writes are denied even after changing the session to read-write. Use a synthetic credential on disposable Postgres.

## Restore and release

The projections are derived views. Goose can rebuild them from the same backed-up publication and moderation records. After a restore, apply the migration and reapply the role grants with the runbook. Worker and Hyperdrive cache entries are disposable.

Before hosted routing, verify TLS and the scoped Access service credential over Tunnel, and disable Hyperdrive query caching for these views. Check the real Free configuration and the rate-limit binding in the target account, including a connection failure and quota exhaustion. Switch to `PUBLIC_READ_MODE=go_cache` if the hosted path cannot satisfy these checks.

[Hyperdrive local development](https://developers.cloudflare.com/hyperdrive/configuration/local-development/) · [Hyperdrive via Tunnel](https://developers.cloudflare.com/hyperdrive/configuration/connect-to-private-database/) · [Postgres 18 privileges](https://www.postgresql.org/docs/18/ddl-priv.html)
