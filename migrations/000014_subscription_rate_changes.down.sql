--
-- Removes the database changes required to support plan rate and default quota changes.
--

BEGIN;

SET search_path = public, pg_catalog;

-- Drop the plan_rate_id column from the subscriptions table;
ALTER TABLE IF EXISTS subscriptions DROP COLUMN IF EXISTS plan_rate_id;

-- Drop the plan_rates table.
DROP TABLE IF EXISTS plan_rates;

-- Delete all but the newest plan quota default value for each resource type from each subscription plan. This step
-- isn't idempotent because the `effective_date` column is being removed. The column is removed as close to the end
-- of the migration as possible in order to reduce the likelihood of errors related to non-idempotency.
DELETE FROM plan_quota_defaults pqd
USING (
      SELECT plan_id, resource_type_id, max(effective_date) AS most_recent_date
      FROM plan_quota_defaults
      GROUP BY plan_id, resource_type_id
) keepers
WHERE keepers.plan_id = pqd.plan_id
AND keepers.resource_type_id = pqd.resource_type_id
AND keepers.most_recent_date != pqd.effective_date;

-- Remove the plan_quota_defaults unique key on plan_id, resource_type_id, and effective_date.
DROP INDEX IF EXISTS plan_quota_defaults_resource_type_plan_effective_date_index;

-- Add the plan_quota_defaults unique key on plan_id and resource_type_id.
CREATE UNIQUE INDEX IF NOT EXISTS plan_quota_defaults_resource_type_plan_index
ON plan_quota_defaults (resource_type_id, plan_id);

-- Drop the effective_date column from the plan_quota_defaults table. This is done as close to the end of the migration
-- as possible becuase removing the column makes one of the previous migration steps non-idempotent.
ALTER TABLE IF EXISTS plan_quota_defaults DROP COLUMN IF EXISTS effective_date;

COMMIT;
