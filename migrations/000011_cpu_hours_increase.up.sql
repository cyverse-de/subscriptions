--
-- Increases the number of CPU hours available to users by a factor of 10.
--

BEGIN;

SET search_path = public, pg_catalog;

-- Multiply the number of CPU hours for each subscription plan by 10.
UPDATE plan_quota_defaults
SET quota_value = quota_value * 10
WHERE resource_type_id = (
    SELECT id FROM resource_types
    WHERE name = 'cpu.hours'
);

-- Multiply the number of CPU hours for each subscription by 10.
UPDATE quotas
SET quota = quota * 10
WHERE resource_type_id = (
    SELECT id FROM resource_types
    WHERE name = 'cpu.hours'
);

COMMIT;
