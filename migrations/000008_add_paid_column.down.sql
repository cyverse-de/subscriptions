BEGIN;

SET search_path = public, pg_catalog;

ALTER TABLE IF EXISTS subscriptions DROP COLUMN IF EXISTS paid;

COMMIT;