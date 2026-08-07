--
-- Adds a table that tracks which addons have been applied to a subscription.
--

BEGIN;

SET search_path = public, pg_catalog;

CREATE TABLE IF NOT EXISTS subscription_addons (
    id uuid NOT NULL DEFAULT uuid_generate_v4(),
    subscription_id uuid NOT NULL,
    addon_id uuid NOT NULL,
    amount numeric NOT NULL,
    paid boolean NOT NULL DEFAULT true,

    FOREIGN KEY (subscription_id) REFERENCES subscriptions(id) ON DELETE CASCADE,
    FOREIGN KEY (addon_id) REFERENCES addons(id) ON DELETE CASCADE,
    PRIMARY KEY (id)
);

COMMIT;