BEGIN;

SET search_path = public, pg_catalog;

--
-- There can only be one quota default for each plan and resource type.
--
CREATE UNIQUE INDEX IF NOT EXISTS plan_quota_defaults_resource_type_plan_index
    ON plan_quota_defaults (resource_type_id, plan_id);

COMMIT;
