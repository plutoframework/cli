-- name: AppendEvent :one
INSERT INTO audit_events (org_id, actor_id, type, payload, hash)
VALUES (@org_id, @actor_id, @type, @payload, @hash)
RETURNING seq, type, payload, hash, created_at;

-- name: ListEvents :many
SELECT seq, type, payload, hash, created_at
FROM audit_events
WHERE org_id = @org_id
  AND seq > @after
  AND (@actor_id::text = '' OR actor_id = @actor_id::text)
  AND (@event_type::text = '' OR type = @event_type::text)
  AND (@since::timestamptz IS NULL OR created_at >= @since)
  AND (@until::timestamptz IS NULL OR created_at < @until)
ORDER BY seq ASC
LIMIT @max_rows;

-- name: ScanEvents :many
SELECT seq, type, payload, hash, created_at
FROM audit_events
WHERE org_id = @org_id AND seq > @after
ORDER BY seq ASC
LIMIT @max_rows;
