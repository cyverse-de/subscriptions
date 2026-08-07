BEGIN;

SET search_path = public, pg_catalog;

CREATE TABLE IF NOT EXISTS addons (
    id uuid NOT NULL DEFAULT uuid_generate_v4(),
    "name" text NOT NULL,
    description text NOT NULL,
    resource_type_id uuid NOT NULL,
    default_amount numeric NOT NULL,
    default_paid boolean NOT NULL DEFAULT true,

    FOREIGN KEY (resource_type_id) REFERENCES resource_types(id) ON DELETE CASCADE,
    PRIMARY KEY (id)
);

COMMIT;