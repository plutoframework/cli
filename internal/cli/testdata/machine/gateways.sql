-- name: CreateGateway :one
INSERT INTO gateways (org_id, id, name, state)
VALUES (@org_id, gen_random_uuid()::text, @name, 'pending')
RETURNING id;

-- name: Beat :one
UPDATE gateways
SET last_seq = @seq, last_seen = now()
WHERE org_id = @org_id AND id = @id
RETURNING last_seq;

-- name: GatewayStatus :one
SELECT id, name, state, last_seq
FROM gateways
WHERE org_id = @org_id AND id = @id;
