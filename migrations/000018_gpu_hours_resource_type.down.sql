BEGIN;

SET search_path = public, pg_catalog;

DELETE FROM resource_types WHERE "name" = 'gpu.hours';

COMMIT;
