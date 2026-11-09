-- name: ActiveQuoteConfig :one
SELECT v.config,p.policy,now()::timestamptz AS quoted_at
FROM pricing_versions v JOIN pricing_quote_policies p ON p.version=v.version
WHERE v.version=(SELECT version FROM pricing_versions WHERE approved AND effective_from<=now() ORDER BY effective_from DESC LIMIT 1);

-- name: InsertQuote :one
INSERT INTO pricing_quotes(learner_id,owner_reference,pricing_version,snapshot,created_at,expires_at)
VALUES(sqlc.arg(learner_id),sqlc.arg(owner_reference),sqlc.arg(pricing_version),sqlc.arg(snapshot),sqlc.arg(created_at),sqlc.arg(expires_at)) RETURNING id::text;

-- name: GetQuote :one
SELECT q.snapshot,q.expires_at<=now() AS expired,EXISTS(SELECT 1 FROM pricing_quote_consents c WHERE c.quote_id=q.id) AS accepted
FROM pricing_quotes q WHERE q.id=sqlc.arg(id)::uuid AND q.learner_id=sqlc.arg(learner_id)::uuid;

-- name: AcceptQuote :execrows
INSERT INTO pricing_quote_consents(quote_id)
SELECT id FROM pricing_quotes WHERE id=sqlc.arg(id)::uuid AND learner_id=sqlc.arg(learner_id)::uuid AND expires_at>now()
ON CONFLICT DO NOTHING;
