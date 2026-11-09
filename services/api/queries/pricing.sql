-- name: ActivePricing :one
SELECT version, effective_from, config
FROM pricing_versions
WHERE approved AND effective_from <= now()
  AND NOT EXISTS (SELECT 1 FROM pricing_version_cancellations c WHERE c.version=pricing_versions.version)
ORDER BY effective_from DESC LIMIT 1;

-- name: NextPricing :one
SELECT version, effective_from, config
FROM pricing_versions
WHERE approved AND effective_from > now()
  AND NOT EXISTS (SELECT 1 FROM pricing_version_cancellations c WHERE c.version=pricing_versions.version)
ORDER BY effective_from ASC LIMIT 1;
