CREATE TABLE audit_events (
    seq bigserial PRIMARY KEY,
    org_id text NOT NULL,
    actor_id text NOT NULL,
    type text NOT NULL,
    payload json NOT NULL,
    hash text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
