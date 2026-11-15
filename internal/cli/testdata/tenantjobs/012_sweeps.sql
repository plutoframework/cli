CREATE TABLE sweeps (
    org_id text NOT NULL,
    id bigserial PRIMARY KEY,
    ran_at timestamptz NOT NULL DEFAULT now(),
    subject text NOT NULL
);
