BEGIN;

SET search_path = public, pg_catalog;

ALTER TABLE IF EXISTS subscriptions ADD COLUMN paid BOOLEAN NOT NULL DEFAULT true;

COMMIT;