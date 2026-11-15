CREATE TABLE users (
    org_id text NOT NULL,
    id text NOT NULL,
    role text NOT NULL,
    state text NOT NULL DEFAULT 'active',
    PRIMARY KEY (org_id, id)
);
