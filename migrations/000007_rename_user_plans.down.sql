BEGIN;

SET search_path = public, pg_catalog;

ALTER TABLE IF EXISTS subscriptions RENAME TO user_plans;
ALTER TRIGGER subscriptions_last_modified_at_trigger ON user_plans RENAME TO user_plans_last_modified_at_trigger;
ALTER TRIGGER subscriptions_last_modified_by_trigger ON user_plans RENAME TO user_plans_last_modified_by_trigger;
ALTER TRIGGER subscriptions_last_modified_by_insert_trigger ON user_plans RENAME TO user_plans_last_modified_by_insert_trigger;
ALTER TRIGGER subscriptions_created_by_trigger ON user_plans RENAME TO user_plans_created_by_trigger;
ALTER INDEX IF EXISTS subscriptions_pkey RENAME TO user_plans_pkey;
ALTER TABLE IF EXISTS user_plans RENAME CONSTRAINT subscriptions_plan_id_fkey TO user_plans_plan_id_fkey ;
ALTER TABLE IF EXISTS user_plans RENAME CONSTRAINT subscriptions_user_id_fkey TO user_plans_user_id_fkey;

ALTER TABLE IF EXISTS quotas RENAME COLUMN subscription_id TO user_plan_id;
ALTER INDEX IF EXISTS quotas_resource_type_subscription_index RENAME TO quotas_resource_type_user_plan_index;
ALTER TABLE IF EXISTS quotas RENAME CONSTRAINT quotas_subscriptions_id_fkey TO quotas_user_plan_id_fkey;

ALTER TABLE IF EXISTS usages RENAME COLUMN subscription_id TO user_plan_id;
ALTER INDEX IF EXISTS usages_resource_type_subscription_index RENAME TO usages_resource_type_user_plan_index;
ALTER TABLE IF EXISTS usages RENAME CONSTRAINT usages_subscription_id_fkey TO usages_user_plan_id_fkey;

COMMIT;