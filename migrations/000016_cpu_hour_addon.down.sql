--
-- Deletes the add-on for cpu-hour-only subscriptions.
--

BEGIN;

SET search_path = public, pg_catalog;

DELETE FROM addons WHERE id = 'f8d2066c-e3b2-4559-839e-88bfc997c89f';

COMMIT;
