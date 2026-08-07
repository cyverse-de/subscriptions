--
-- Deletes the add-on for data-only subscriptions.
--

BEGIN;

SET search_path = public, pg_catalog;

DELETE FROM addons WHERE id = 'c21dd61f-aa41-40ad-8005-859679ceed9c';

COMMIT;
