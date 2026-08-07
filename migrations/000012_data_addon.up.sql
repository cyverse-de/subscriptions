--
-- Creates an add-on for data-only subscriptions.
--

BEGIN;

SET search_path = public, pg_catalog;

INSERT INTO addons (id, name, description, resource_type_id, default_amount, default_paid)
VALUES (
    'c21dd61f-aa41-40ad-8005-859679ceed9c',
    '1 TB',
    '1 TB of data storage for one year.',
    (SELECT id FROM resource_types WHERE name = 'data.size'),
    power(2, 40),
    TRUE
)
ON CONFLICT DO NOTHING;

COMMIT;
