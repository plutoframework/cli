CREATE TABLE users (
    org_id text NOT NULL,
    id text NOT NULL,
    role text NOT NULL,
    PRIMARY KEY (org_id, id)
);

CREATE TABLE approval_requests (
    org_id text NOT NULL,
    id text NOT NULL,
    status text NOT NULL DEFAULT 'pending',
    PRIMARY KEY (org_id, id)
);
