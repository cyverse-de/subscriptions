BEGIN;

SET search_path = public, pg_catalog;

ALTER TABLE IF EXISTS updates ADD COLUMN IF NOT EXISTS metadata TEXT;

COMMIT;
