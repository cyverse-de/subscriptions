BEGIN;

SET search_path = public, pg_catalog;

DROP TRIGGER IF EXISTS user_plans_last_modified_at_trigger ON user_plans CASCADE;
DROP TRIGGER IF EXISTS user_plans_last_modified_by_trigger ON user_plans CASCADE;
DROP TRIGGER IF EXISTS user_plans_last_modified_by_insert_trigger ON user_plans CASCADE;
DROP TRIGGER IF EXISTS user_plans_created_by_trigger ON user_plans CASCADE;

DROP TABLE IF EXISTS user_plans;
DROP TABLE IF EXISTS plan_quota_defaults;
DROP TABLE IF EXISTS resource_types;
DROP TABLE IF EXISTS plans;

COMMIT;
