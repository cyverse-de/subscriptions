--
-- Makes the database changes required to support addon rate and default quota changes.
--

BEGIN;

SET search_path = public, pg_catalog;

-- Create the addon_rates table.
CREATE TABLE IF NOT EXISTS addon_rates (
    id uuid NOT NULL default uuid_generate_v1(),
    addon_id uuid NOT NULL,
    effective_date timestamp WITH time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    rate numeric NOT NULL,
    FOREIGN KEY (addon_id) REFERENCES addons(id) ON DELETE CASCADE,
    PRIMARY KEY (id)
);

-- Populate the addon_rates table. This could cause a problem if this isn't the exact set of addons, but the chances of
-- that happening in any DE deployment at this time are small because the subscription admin pages don't currently have
-- a feature to add or remove addons.
INSERT INTO addon_rates (id, addon_id, effective_date, rate) VALUES
('d612d958-82ad-11ef-b4b2-5a8d7f4f1112', 'c21dd61f-aa41-40ad-8005-859679ceed9c', '2022-01-01', 125.00)
ON CONFLICT (id) DO NOTHING;

-- There can only be one rate for each subscription addon that can become effective at a specific time.
CREATE UNIQUE INDEX IF NOT EXISTS addon_rates_effective_date_addon_index
    ON addon_rates(addon_id, effective_date);

-- Add the addon_rate_id column to the subscription_addons table.
ALTER TABLE IF EXISTS subscription_addons
    ADD COLUMN IF NOT EXISTS addon_rate_id uuid REFERENCES addon_rates(id) ON DELETE CASCADE;
UPDATE subscription_addons SET addon_rate_id = (
    SELECT id FROM addon_rates WHERE addon_id = subscription_addons.addon_id LIMIT 1
);
ALTER TABLE IF EXISTS subscription_addons ALTER COLUMN addon_rate_id SET NOT NULL;

COMMIT;
