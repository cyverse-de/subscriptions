BEGIN;

SET search_path = public, pg_catalog;

DROP TRIGGER IF EXISTS updates_last_modified_at_trigger ON updates CASCADE;
DROP TRIGGER IF EXISTS updates_last_modified_by_trigger ON updates CASCADE;
DROP TRIGGER IF EXISTS updates_last_modified_by_insert_trigger ON updates CASCADE;
DROP TRIGGER IF EXISTS updates_created_by_trigger ON updates CASCADE;

DROP INDEX IF EXISTS updates_resource_type_user_plan_index;
DROP TABLE IF EXISTS updates CASCADE;

DROP TABLE IF EXISTS update_operations CASCADE;
DROP TYPE tracked_metrics CASCADE;

DROP TRIGGER IF EXISTS usages_last_modified_at_trigger ON usages CASCADE;
DROP TRIGGER IF EXISTS usages_last_modified_by_trigger ON usages CASCADE;
DROP TRIGGER IF EXISTS usages_last_modified_by_insert_trigger ON usages CASCADE;
DROP TRIGGER IF EXISTS usages_created_by_trigger ON usages CASCADE;

DROP INDEX IF EXISTS usages_resource_type_user_plan_index;
DROP TABLE IF EXISTS usages CASCADE;

DROP TRIGGER IF EXISTS quotas_last_modified_at_trigger ON quotas CASCADE;
DROP TRIGGER IF EXISTS quotas_last_modified_by_trigger ON quotas CASCADE;
DROP TRIGGER IF EXISTS quotas_last_modified_by_insert_trigger ON quotas CASCADE;
DROP TRIGGER IF EXISTS quotas_created_by_trigger ON quotas CASCADE;

DROP INDEX IF EXISTS quotas_resource_type_user_plan_index;
DROP TABLE IF EXISTS quotas CASCADE;

COMMIT;
