CREATE TABLE users (
    org_id text NOT NULL,
    id text NOT NULL,
    role text NOT NULL,
    PRIMARY KEY (org_id, id)
);
