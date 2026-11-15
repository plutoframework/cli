CREATE TABLE users (
    org_id text NOT NULL,
    id text NOT NULL,
    role text NOT NULL,
    PRIMARY KEY (org_id, id)
);
CREATE TABLE gateways (
    org_id text NOT NULL,
    id text PRIMARY KEY,
    name text NOT NULL,
    public_key text UNIQUE,
    state text NOT NULL DEFAULT 'pending',
    last_seq bigint NOT NULL DEFAULT 0,
    last_seen timestamptz
);
