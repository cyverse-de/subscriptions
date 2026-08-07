--
-- Decreases the number of CPU hours available to users by a factor of 10.
--

BEGIN;

SET search_path = public, pg_catalog;

-- Divide the number of CPU hours for each subscription plan by 10.
UPDATE plan_quota_defaults
SET quota_value = CAST(quota_value / 10 AS bigint)
WHERE resource_type_id = (
    SELECT id FROM resource_types
    WHERE name = 'cpu.hours'
);

-- Divide the number of CPU hours for each subscription by 10.
UPDATE quotas
SET quota = CAST(quota / 10 AS bigint)
WHERE resource_type_id = (
    SELECT id FROM resource_types
    WHERE name = 'cpu.hours'
);

COMMIT;
