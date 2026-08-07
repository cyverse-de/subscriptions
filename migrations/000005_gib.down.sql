BEGIN;

SET search_path = public, pg_catalog;

--
-- Update the plan quota defaults.
--
UPDATE plan_quota_defaults SET quota_value = 5 * 10^9 WHERE id = '60b3d5ae-9511-11ec-8844-406c8f3e9cbb';
UPDATE plan_quota_defaults SET quota_value = 50 * 10^9 WHERE id = '0ebd2c19-7c1d-4418-a02f-df5f6d782901';
UPDATE plan_quota_defaults SET quota_value = 3 * 10^12 WHERE id = '2c39ff2f-2ec7-4ac8-a10e-79fd82b39c09';
UPDATE plan_quota_defaults SET quota_value = 5 * 10^12 WHERE id = 'de496045-b954-4f41-b068-3c71b32d2287';

--
-- Update the quota values themselves. This conversion assumes that no modifications have been made to existing
-- quotas already. If any modifications have been made, it will be necessary to update them again.
--
UPDATE quotas q SET quota = pqd.quota_value
    FROM user_plans up
    JOIN plans p ON up.plan_id = p.id
    JOIN plan_quota_defaults pqd ON p.id = pqd.plan_id
    WHERE q.user_plan_id = up.id
    AND q.resource_type_id = pqd.resource_type_id;

COMMIT;
