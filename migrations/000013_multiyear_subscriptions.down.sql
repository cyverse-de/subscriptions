--
-- Removes the database changes required to support multi-year subscriptions.
--

BEGIN;

SET search_path = public, pg_catalog;

ALTER TABLE IF EXISTS resource_types DROP IF EXISTS consumable CASCADE;

COMMIT;
