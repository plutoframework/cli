CREATE TABLE users (
    org_id text NOT NULL,
    id text NOT NULL,
    role text NOT NULL,
    state text NOT NULL DEFAULT 'active',
    PRIMARY KEY (org_id, id)
);

CREATE TABLE approval_requests (
    org_id text NOT NULL,
    id text NOT NULL,
    status text NOT NULL DEFAULT 'pending',
    PRIMARY KEY (org_id, id)
);

CREATE TABLE orgs (
    id text PRIMARY KEY,
    slug text NOT NULL UNIQUE,
    name text NOT NULL,
    billing_email text NOT NULL DEFAULT 'private@example.test'
);
