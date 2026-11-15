CREATE TABLE orgs (
    id text PRIMARY KEY,
    slug text NOT NULL UNIQUE,
    name text NOT NULL
);
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
    online boolean NOT NULL DEFAULT false,
    last_seq bigint NOT NULL DEFAULT 0,
    last_seen timestamptz
);
CREATE TABLE gateway_beats (
    org_id text NOT NULL,
    id bigserial PRIMARY KEY,
    gateway_id text NOT NULL,
    seq bigint NOT NULL,
    at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE audit_events (
    seq bigserial PRIMARY KEY,
    org_id text NOT NULL,
    actor_id text NOT NULL,
    type text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
