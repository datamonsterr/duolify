-- name: ActivePricing :one
SELECT version, effective_from, config
FROM pricing_versions
WHERE approved AND effective_from <= now()
ORDER BY effective_from DESC LIMIT 1;

-- name: NextPricing :one
SELECT version, effective_from, config
FROM pricing_versions
WHERE approved AND effective_from > now()
ORDER BY effective_from ASC LIMIT 1;
