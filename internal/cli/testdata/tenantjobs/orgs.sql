-- name: RecordSweep :exec
INSERT INTO sweeps (org_id, subject) VALUES (@org_id, @subject);

-- name: CountSweeps :one
SELECT count(*) FROM sweeps WHERE org_id = @org_id;
