package sqlcheck

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requireSQLC(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("sqlc"); err != nil {
		t.Skip("sqlc not installed")
	}
}

func writeProject(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return root
}

func projectConfig(root string) Config {
	return Config{
		Root:       root,
		SchemaDirs: []string{"db/migrations"},
		QueryRoots: []string{"db/queries"},
		Tables: []Table{
			{
				Name: "audit_events", Group: "audit", TenantColumn: "org_id",
				AppendOnly: true,
			},
			{Name: "audit_tags", Group: "audit", TenantColumn: "org_id"},
		},
		Sets: []QuerySet{{
			Group:  "audit",
			Files:  []string{"db/queries/audit/*.sql"},
			Tables: []string{"audit_events"},
		}},
	}
}

const goodQueries = `-- name: List :many
SELECT id, body FROM audit_events WHERE org_id = sqlc.arg(org_id);

-- name: Append :exec
INSERT INTO audit_events (org_id, body) VALUES (@org_id, @body);
`

func baseFiles(queries string) map[string]string {
	return map[string]string{
		"db/migrations/001_init.sql": goodMigration + `
CREATE TABLE audit_tags (id bigserial PRIMARY KEY, org_id uuid NOT NULL);
ALTER TABLE audit_tags ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_tags FORCE ROW LEVEL SECURITY;
CREATE POLICY p ON audit_tags
    USING (org_id = current_setting('app.tenant_id', true)::uuid);
`,
		"db/queries/audit/audit.sql": queries,
	}
}

func TestCheckAcceptsCleanProject(t *testing.T) {
	requireSQLC(t)

	root := writeProject(t, baseFiles(goodQueries))

	got, err := Check(context.Background(), projectConfig(root))
	if err != nil || len(got) != 0 {
		t.Fatalf("want clean, got %v %v", got, err)
	}
}

func TestCheckRejectsThroughSQLC(t *testing.T) {
	requireSQLC(t)

	cases := map[string]struct {
		queries string
		want    string
	}{
		"or branch": {
			"-- name: Q :many\nSELECT id FROM audit_events\n" +
				"WHERE org_id = sqlc.arg(org_id) OR kind = 'x';\n",
			"OR at the top level",
		},
		"narg": {
			"-- name: Q :many\nSELECT id FROM audit_events\n" +
				"WHERE org_id = sqlc.narg(org_id);\n",
			"nullable",
		},
		"slice": {
			"-- name: Q :many\nSELECT id FROM audit_events\n" +
				"WHERE org_id = ANY(sqlc.slice(orgs)::uuid[]);\n",
			"does not bind",
		},
		"cte": {
			"-- name: Q :many\nWITH x AS (SELECT id FROM audit_events\n" +
				"WHERE org_id = @org_id) SELECT id FROM x;\n",
			"common table expression",
		},
		"subquery": {
			"-- name: Q :many\nSELECT id FROM audit_events\n" +
				"WHERE org_id = @org_id AND id IN\n" +
				"(SELECT id FROM audit_tags WHERE org_id = @org_id);\n",
			"subquery",
		},
		"undeclared set table": {
			"-- name: Q :many\nSELECT t.id FROM audit_tags t\n" +
				"WHERE t.org_id = @org_id;\n",
			"does not list",
		},
		"insert without tenant": {
			"-- name: Q :exec\nINSERT INTO audit_events (body)\n" +
				"VALUES (@body);\n",
			"without the tenant column",
		},
		"sqlc error": {
			"-- name: Q :many\nSELECT nope FROM audit_events\n" +
				"WHERE org_id = @org_id;\n",
			"could not analyse",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := writeProject(t, baseFiles(goodQueries+"\n"+tc.queries))

			got, err := Check(context.Background(), projectConfig(root))
			if err != nil {
				t.Fatal(err)
			}

			if !strings.Contains(messages(got), tc.want) {
				t.Fatalf("want %q in:\n%s", tc.want, messages(got))
			}
		})
	}
}

func TestCheckReportsQueryLine(t *testing.T) {
	requireSQLC(t)

	bad := goodQueries + "\n-- name: Bad :many\nSELECT id FROM audit_events;\n"
	root := writeProject(t, baseFiles(bad))

	got, _ := Check(context.Background(), projectConfig(root))
	if len(got) != 1 || got[0].File != "db/queries/audit/audit.sql" ||
		got[0].Line != 7 {
		t.Fatalf("want audit.sql:7, got %v", got)
	}
}

func TestCheckRejectsStrayQueryFile(t *testing.T) {
	requireSQLC(t)

	files := baseFiles(goodQueries)
	files["db/queries/other/rogue.sql"] = "-- name: X :one\nSELECT 1;\n"
	root := writeProject(t, files)

	got, _ := Check(context.Background(), projectConfig(root))
	if !strings.Contains(messages(got), "not part of any declared") {
		t.Fatalf("got %v", got)
	}
}

func TestCheckFailsClosedWithoutSQLC(t *testing.T) {
	root := writeProject(t, baseFiles(goodQueries))
	cfg := projectConfig(root)
	cfg.SQLCPath = filepath.Join(root, "missing-sqlc")

	got, err := Check(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(messages(got), "sqlc is required") {
		t.Fatalf("must fail closed, got %v", got)
	}
}

func TestCheckSQLCDiff(t *testing.T) {
	requireSQLC(t)

	files := baseFiles(goodQueries)
	files["sqlc.yaml"] = `version: "2"
sql:
  - engine: postgresql
    schema: db/migrations
    queries: db/queries/audit
    gen:
      go:
        package: audit
        out: db/gen/audit
`
	root := writeProject(t, files)
	cfg := projectConfig(root)
	cfg.SQLCConfig = "sqlc.yaml"

	got, _ := Check(context.Background(), cfg)
	if !strings.Contains(messages(got), "sqlc diff is not clean") {
		t.Fatalf("ungenerated output must fail diff, got %v", got)
	}

	generate := exec.Command("sqlc", "generate")

	generate.Dir = root
	if out, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("generate: %v\n%s", err, out)
	}

	got, _ = Check(context.Background(), cfg)
	if len(got) != 0 {
		t.Fatalf("want clean after generate, got %v", got)
	}

	generated := filepath.Join(root, "db/gen/audit/audit.sql.go")

	data, err := os.ReadFile(generated)
	if err != nil {
		t.Fatal(err)
	}

	tampered := strings.Replace(string(data), "org_id = $1", "true", 1)
	if err := os.WriteFile(generated, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}

	got, _ = Check(context.Background(), cfg)
	if !strings.Contains(messages(got), "sqlc diff is not clean") {
		t.Fatalf("tampered output must fail diff, got %v", got)
	}
}
