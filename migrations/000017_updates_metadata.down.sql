BEGIN;

SET search_path = public, pg_catalog;

ALTER TABLE IF EXISTS updates DROP COLUMN IF EXISTS metadata;

COMMIT;
