-- name: CreateGateway :one
INSERT INTO gateways (org_id, id, name)
VALUES (@org_id, gen_random_uuid()::text, @name)
RETURNING id;

-- name: RenameGateway :one
UPDATE gateways
SET name = @name
WHERE org_id = @org_id AND id = @id
RETURNING id, name, state, online;

-- name: Beat :one
UPDATE gateways
SET last_seq = @seq, last_seen = now(), online = true
WHERE org_id = @org_id AND id = @id
RETURNING last_seq;

-- name: RecordBeat :exec
INSERT INTO gateway_beats (org_id, gateway_id, seq)
VALUES (@org_id, @gateway_id, @seq);

-- name: GatewayStatus :one
SELECT id, name, state, online, last_seq
FROM gateways
WHERE org_id = @org_id AND id = @id;

-- name: ListBeats :many
SELECT id, seq, at
FROM gateway_beats
WHERE org_id = @org_id AND gateway_id = @gateway_id AND id > @after
ORDER BY id ASC
LIMIT @max_rows;

-- name: MarkStale :execrows
UPDATE gateways
SET online = false
WHERE org_id = @org_id AND online AND last_seen < @before;

-- name: AppendAudit :exec
INSERT INTO audit_events (org_id, actor_id, type)
VALUES (@org_id, @actor_id, @type);
