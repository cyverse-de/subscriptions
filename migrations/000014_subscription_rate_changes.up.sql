--
-- Makes the database changes required to support plan rate and default quota changes.
--

BEGIN;

SET search_path = public, pg_catalog;

-- Add the new plan_rates table.
CREATE TABLE IF NOT EXISTS plan_rates (
    id uuid NOT NULL DEFAULT uuid_generate_v1(),
    plan_id uuid NOT NULL,
    effective_date timestamp WITH time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    rate numeric NOT NULL,
    FOREIGN KEY (plan_id) REFERENCES plans(id) ON DELETE CASCADE,
    PRIMARY KEY (id)
);

-- There can only be one rate for each subscription plan that can become effective at a specific time.
CREATE UNIQUE INDEX IF NOT EXISTS plan_rates_plan_effective_date_index
    ON plan_rates (plan_id, effective_date);

-- Populate the plan_rates table with initial values. This could cause a problem if this isn't the exact set of plans,
-- but the changes of having a different set of plans in any DE deployment at this point in time is small because the
-- subscription admin pages don't currently have a feature to allow administrators to add or remove subscription plans.
INSERT INTO plan_rates (id, plan_id, effective_date, rate) VALUES
('120c56e0-82a0-11ef-aac3-5a8d7f4f1112', '99e47c22-950a-11ec-84a4-406c8f3e9cbb', '2022-01-01', 0.00),
('1db9ab6e-82a0-11ef-aac3-5a8d7f4f1112', 'c6d39580-98dc-11ec-bbe3-406c8f3e9cbb', '2022-01-01', 200.00),
('29f6cff6-82a0-11ef-aac3-5a8d7f4f1112', 'cdf7ac7a-98dc-11ec-bbe3-406c8f3e9cbb', '2022-01-01', 340.00),
('312d94c6-82a0-11ef-aac3-5a8d7f4f1112', 'd80b5482-98dc-11ec-bbe3-406c8f3e9cbb', '2022-01-01', 2000.00)
ON CONFLICT (id) DO NOTHING;

-- Drop the plan_quota_defaults unique key on plan_id and resource_type_id.
DROP INDEX IF EXISTS plan_quota_defaults_resource_type_plan_index;

-- Add the effective date column to the plan_quota_defaults table.
ALTER TABLE IF EXISTS plan_quota_defaults ADD COLUMN effective_date timestamp WITH time zone;
UPDATE plan_quota_defaults SET effective_date = '2022-01-01';
ALTER TABLE IF EXISTS plan_quota_defaults ALTER COLUMN effective_date SET NOT NULL;

-- Ensure that no two plan quota default values for the same plan have both the same resource type and effective date.
CREATE UNIQUE INDEX IF NOT EXISTS plan_quota_defaults_resource_type_plan_effective_date_index
    ON plan_quota_defaults (resource_type_id, effective_date, plan_id);


-- Add the plan rate ID column to the subscriptions table.
ALTER TABLE IF EXISTS subscriptions
ADD COLUMN IF NOT EXISTS plan_rate_id uuid REFERENCES plan_rates (id) ON DELETE CASCADE;

-- Populate the new column.
UPDATE subscriptions SET plan_rate_id = (
       SELECT id FROM plan_rates
       WHERE plan_id = subscriptions.plan_id
       AND effective_date <= subscriptions.effective_start_date
       ORDER BY effective_date DESC
       LIMIT 1
);

-- Require a plan rate ID for all subscriptions.
ALTER TABLE IF EXISTS subscriptions ALTER COLUMN plan_rate_id SET NOT NULL;

COMMIT;
