--
-- Makes the database changes required to support multi-year subscriptions.
--

BEGIN;

SET search_path = public, pg_catalog;

ALTER TABLE IF EXISTS resource_types ADD IF NOT EXISTS consumable boolean DEFAULT FALSE;

UPDATE resource_types SET consumable = TRUE WHERE "name" = 'cpu.hours';
UPDATE resource_types SET consumable = FALSE WHERE "name" = 'data.size';

COMMIT;
