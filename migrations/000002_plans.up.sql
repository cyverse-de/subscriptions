BEGIN;

SET search_path = public, pg_catalog;

--
-- A table containing a list of resource usage plans available to CyVerse users.
--
CREATE TABLE IF NOT EXISTS plans (
    id uuid NOT NULL DEFAULT uuid_generate_v1(),
    "name" text NOT NULL UNIQUE,
    description text NOT NULL,
    PRIMARY KEY (id)
);

--
-- The default list of plans.
--
INSERT INTO plans (id, "name", description) VALUES
    ('99e47c22-950a-11ec-84a4-406c8f3e9cbb', 'Basic', 'Basic plan'),
    ('c6d39580-98dc-11ec-bbe3-406c8f3e9cbb', 'Regular', 'Regular plan'),
    ('cdf7ac7a-98dc-11ec-bbe3-406c8f3e9cbb', 'Pro', 'Professional plan'),
    ('d80b5482-98dc-11ec-bbe3-406c8f3e9cbb', 'Commercial', 'Commercial plan')
    ON CONFLICT DO NOTHING;

--
-- A table containing a list of resource types that can have quotas applied to them.
--
CREATE TABLE IF NOT EXISTS resource_types (
    id uuid NOT NULL DEFAULT uuid_generate_v1(),
    "name" text NOT NULL UNIQUE,
    unit text NOT NULL,
    PRIMARY KEY (id)
);

--
-- The default list of resource types.
--
INSERT INTO resource_types (id, "name", unit) VALUES
    ('99e3bc7e-950a-11ec-84a4-406c8f3e9cbb', 'cpu.hours', 'cpu hours'),
    ('99e3f91e-950a-11ec-84a4-406c8f3e9cbb', 'data.size', 'bytes')
    ON CONFLICT DO NOTHING;

--
-- A table containing default quota values associated with the various plans for each resource type.
--
CREATE TABLE IF NOT EXISTS plan_quota_defaults (
   id uuid NOT NULL DEFAULT uuid_generate_v1(),
   plan_id uuid NOT NULL,
   resource_type_id uuid NOT NULL,
   quota_value numeric NOT NULL,
   FOREIGN KEY (plan_id) REFERENCES plans(id) ON DELETE CASCADE,
   FOREIGN KEY (resource_type_id) REFERENCES resource_types(id) ON DELETE CASCADE,
   PRIMARY KEY (id)
);

--
-- The list of default quota values for the initial plans and resource types.
--
INSERT INTO plan_quota_defaults (id, plan_id, resource_type_id, quota_value) VALUES
    ('46febbba-9511-11ec-8844-406c8f3e9cbb', '99e47c22-950a-11ec-84a4-406c8f3e9cbb', '99e3bc7e-950a-11ec-84a4-406c8f3e9cbb', 20),
    ('60b3d5ae-9511-11ec-8844-406c8f3e9cbb', '99e47c22-950a-11ec-84a4-406c8f3e9cbb', '99e3f91e-950a-11ec-84a4-406c8f3e9cbb', 5 * 10^9),
    ('e286b526-6e50-4395-99b0-431f76cb4dd9', 'c6d39580-98dc-11ec-bbe3-406c8f3e9cbb', '99e3bc7e-950a-11ec-84a4-406c8f3e9cbb', 100),
    ('0ebd2c19-7c1d-4418-a02f-df5f6d782901', 'c6d39580-98dc-11ec-bbe3-406c8f3e9cbb', '99e3f91e-950a-11ec-84a4-406c8f3e9cbb', 50 * 10^9),
    ('7efddabe-47d6-401c-b857-d08361397fcf', 'cdf7ac7a-98dc-11ec-bbe3-406c8f3e9cbb', '99e3bc7e-950a-11ec-84a4-406c8f3e9cbb', 2000),
    ('2c39ff2f-2ec7-4ac8-a10e-79fd82b39c09', 'cdf7ac7a-98dc-11ec-bbe3-406c8f3e9cbb', '99e3f91e-950a-11ec-84a4-406c8f3e9cbb', 3 * 10^12),
    ('1e3804ed-4ed3-4cd9-8e58-1e725fa79c1b', 'd80b5482-98dc-11ec-bbe3-406c8f3e9cbb', '99e3bc7e-950a-11ec-84a4-406c8f3e9cbb', 5000),
    ('de496045-b954-4f41-b068-3c71b32d2287', 'd80b5482-98dc-11ec-bbe3-406c8f3e9cbb', '99e3f91e-950a-11ec-84a4-406c8f3e9cbb', 5 * 10^12)
    ON CONFLICT DO NOTHING;

--
-- A table indicating which plans are or have ever been active for each user.
--
CREATE TABLE IF NOT EXISTS user_plans (
    id uuid NOT NULL DEFAULT uuid_generate_v1(),
    user_id uuid NOT NULL,
    plan_id uuid NOT NULL,
    effective_start_date timestamp with time zone NOT NULL,
    effective_end_date timestamp with time zone,
    created_by text NOT NULL,
    created_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_modified_by text NOT NULL,
    last_modified_at timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (plan_id) REFERENCES plans(id) ON DELETE CASCADE,
    PRIMARY KEY (id)
);

--
-- A trigger to set the created_by field when a new row is added to the user_plans table.
--
DROP TRIGGER IF EXISTS user_plans_created_by_trigger ON user_plans CASCADE;
CREATE TRIGGER user_plans_created_by_trigger
    BEFORE INSERT ON user_plans
    FOR EACH ROW
    EXECUTE PROCEDURE insert_username(created_by);

--
-- A trigger to set the last_modified_by field when a row is added to the user_plans table.
--
DROP TRIGGER IF EXISTS user_plans_last_modified_by_insert_trigger ON user_plans CASCADE;
CREATE TRIGGER user_plans_last_modified_by_insert_trigger
    BEFORE INSERT ON user_plans
    FOR EACH ROW
    EXECUTE PROCEDURE insert_username(last_modified_by);

--
-- A trigger to set the last_modified_by field when a row is modified in the user_plans table.
--
DROP TRIGGER IF EXISTS user_plans_last_modified_by_trigger ON user_plans CASCADE;
CREATE TRIGGER user_plans_last_modified_by_trigger
    BEFORE UPDATE ON user_plans
    FOR EACH ROW
    EXECUTE PROCEDURE insert_username(last_modified_by);

--
-- A trigger to set the last_modified_at field when a row is modified in the user_plans table.
--
DROP TRIGGER IF EXISTS user_plans_last_modified_at_trigger ON user_plans CASCADE;
CREATE TRIGGER user_plans_last_modified_at_trigger
    BEFORE UPDATE ON user_plans
    FOR EACH ROW
    EXECUTE PROCEDURE moddatetime(last_modified_at);

COMMIT;
