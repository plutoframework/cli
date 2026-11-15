-- name: ApproveRequest :execrows
UPDATE approval_requests
SET status = 'approved'
WHERE org_id = @org_id AND id = @id AND status = 'pending';
