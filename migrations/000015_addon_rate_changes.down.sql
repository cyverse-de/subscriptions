--
-- Removes the database changes required to support addon rate and default quota changes.
--

BEGIN;

SET search_path = public, pg_catalog;

-- Drop the addon_rate_id column from the subscription_addons table.
ALTER TABLE IF EXISTS subscription_addons DROP COLUMN IF EXISTS addon_rate_id;

-- Drop the addon_rates table.
DROP TABLE IF EXISTS addon_rates;

COMMIT;
