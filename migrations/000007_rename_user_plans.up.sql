BEGIN;

SET search_path = public, pg_catalog;

ALTER TABLE IF EXISTS user_plans RENAME TO subscriptions;
ALTER TRIGGER user_plans_last_modified_at_trigger ON subscriptions RENAME TO subscriptions_last_modified_at_trigger;
ALTER TRIGGER user_plans_last_modified_by_trigger ON subscriptions RENAME TO subscriptions_last_modified_by_trigger;
ALTER TRIGGER user_plans_last_modified_by_insert_trigger ON subscriptions RENAME TO subscriptions_last_modified_by_insert_trigger;
ALTER TRIGGER user_plans_created_by_trigger ON subscriptions RENAME TO subscriptions_created_by_trigger;
ALTER INDEX IF EXISTS user_plans_pkey RENAME TO subscriptions_pkey;
ALTER TABLE IF EXISTS subscriptions RENAME CONSTRAINT user_plans_plan_id_fkey TO subscriptions_plan_id_fkey;
ALTER TABLE IF EXISTS subscriptions RENAME CONSTRAINT user_plans_user_id_fkey TO subscriptions_user_id_fkey;

ALTER TABLE IF EXISTS quotas RENAME COLUMN user_plan_id TO subscription_id;
ALTER INDEX IF EXISTS quotas_resource_type_user_plan_index RENAME TO quotas_resource_type_subscription_index;
ALTER TABLE IF EXISTS quotas RENAME CONSTRAINT quotas_user_plan_id_fkey TO quotas_subscriptions_id_fkey;

ALTER TABLE IF EXISTS usages RENAME COLUMN user_plan_id to subscription_id;
ALTER INDEX IF EXISTS usages_resource_type_user_plan_index RENAME TO usages_resource_type_subscription_index;
ALTER TABLE IF EXISTS usages RENAME CONSTRAINT usages_user_plan_id_fkey TO usages_subscription_id_fkey;

COMMIT;