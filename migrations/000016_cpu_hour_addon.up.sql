--
-- Creates an add-on for cpu-hour-only subscriptions.
--

BEGIN;

SET search_path = public, pg_catalog;

INSERT INTO addons (id, name, description, resource_type_id, default_amount, default_paid)
VALUES (
    'f8d2066c-e3b2-4559-839e-88bfc997c89f',
    '5000 CPU Hours',
    '5000 CPU Hours to be used before the current subscription ends..',
    (SELECT id FROM resource_types WHERE name = 'cpu.hours'),
    5000,
    TRUE
)
ON CONFLICT DO NOTHING;

INSERT INTO addon_rates (id, addon_id, effective_date, rate) VALUES
('672e49af-b7b7-4a0e-a929-00f4a1c5e7f2', 'f8d2066c-e3b2-4559-839e-88bfc997c89f', '2025-01-01', 100.00)
ON CONFLICT (id) DO NOTHING;

COMMIT;
