BEGIN;

SET search_path = public, pg_catalog;

--
-- Seed the gpu.hours resource type for GPU usage tracking.
-- No plan_quota_defaults are seeded, as usage is not limited initially
--
INSERT INTO resource_types (id, "name", unit, consumable) VALUES
    ('bd0cef09-051e-4773-824c-c1902992bc41', 'gpu.hours', 'gpu hours', true)
    ON CONFLICT DO NOTHING;

COMMIT;
