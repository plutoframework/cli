-- name: CreateOrg :one
INSERT INTO orgs (id, slug, name)
VALUES (@id, @slug, @name)
RETURNING slug, name;

-- name: AddMember :exec
INSERT INTO users (org_id, id, role)
VALUES (@org_id, @id, @role);

-- name: CountMembers :one
SELECT count(*) FROM users WHERE org_id = @org_id;
