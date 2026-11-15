CREATE TABLE orgs (
    id text PRIMARY KEY,
    slug text NOT NULL UNIQUE,
    name text NOT NULL,
    billing_email text NOT NULL DEFAULT 'private@example.test'
);
