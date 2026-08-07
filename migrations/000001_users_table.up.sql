BEGIN;

SET search_path = public, pg_catalog;

CREATE TABLE IF NOT EXISTS users (
    id uuid NOT NULL DEFAULT uuid_generate_v1(),
    username text NOT NULL UNIQUE,
    PRIMARY KEY (id)
);

COMMIT;
