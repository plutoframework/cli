package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

const adapterQuery = `-- name: ListAuditEvents :many
SELECT seq, type FROM audit_events
WHERE org_id = sqlc.arg(org_id) AND seq > sqlc.arg(after_seq)
ORDER BY seq LIMIT 50;
`

const adapterTable = `CREATE TABLE audit_events (
    seq bigserial PRIMARY KEY,
    org_id text NOT NULL,
    type text NOT NULL
);
`

func sqlcFixture(t *testing.T) (string, Manifest, []byte) {
	t.Helper()

	if _, err := exec.LookPath("sqlc"); err != nil {
		t.Skip("sqlc is not installed")
	}

	m := Manifest{
		Boundary: BoundaryProfile{Mode: "candidate"},
		Project:  Project{Module: "example.com/gryt"},
		Tables: []Table{{
			Group:        "audit",
			Name:         "audit_events",
			TenantColumn: "org_id",
			AppendOnly:   true,
			RowSecurity:  true,
		}},
		Queries: []QuerySet{{
			Group:  "audit",
			Files:  []string{"db/queries/audit/*.sql"},
			Tables: []string{"audit_events"},
		}},
	}

	root := t.TempDir()
	write := func(path, content string) {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write("db/migrations/001_initial.sql", initialMigrationSQL)
	write("db/migrations/002_tables.sql", adapterTable)
	write("db/queries/schema.sql", "-- name: SchemaVersion :one\n"+
		"SELECT version FROM schema_metadata LIMIT 1;\n")
	write("db/queries/audit/audit.sql", adapterQuery)

	proposals, err := ProposeTableMigrations(root, m)
	if err != nil {
		t.Fatal(err)
	}

	if len(proposals) != 1 {
		t.Fatalf("want one proposal, got %v", proposals)
	}

	for path, source := range proposals {
		if path != "db/migrations/003_rls_audit_events.sql" {
			t.Fatalf("unexpected proposal path %s", path)
		}

		write(path, string(source))
	}

	write("sqlc.yaml", string(candidateSQLCConfig(m)))

	generate := exec.Command("sqlc", "generate")
	generate.Dir = root

	if output, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("sqlc generate: %v\n%s", err, output)
	}

	metadata, err := toml.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}

	return root, m, metadata
}

func TestAuthoredSQLAcceptsGeneratedPieces(t *testing.T) {
	root, _, metadata := sqlcFixture(t)

	if found := inspectAuthoredSQL(root, metadata); len(found) != 0 {
		t.Fatalf("clean project rejected: %+v", found)
	}

	override, err := os.ReadFile(
		filepath.Join(root, "internal/storage/queries/audit/models.go"),
	)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(override), "scope.Tenant") {
		t.Fatalf("tenant column was not overridden:\n%s", override)
	}

	// Proposals are idempotent: nothing further once satisfied.
	var m Manifest
	if err := toml.Unmarshal(metadata, &m); err != nil {
		t.Fatal(err)
	}

	again, err := ProposeTableMigrations(root, m)
	if err != nil || len(again) != 0 {
		t.Fatalf("second proposal = %v, %v", again, err)
	}
}

func TestAuthoredSQLRejectsBypasses(t *testing.T) {
	for name, mutate := range map[string]func(t *testing.T, root string){
		"query without tenant argument": func(t *testing.T, root string) {
			rewrite(t, root, "db/queries/audit/audit.sql",
				"org_id = sqlc.arg(org_id) AND ", "")
			regenerate(t, root)
		},
		"stray query file": func(t *testing.T, root string) {
			rewrite(t, root, "db/queries/audit/extra.txt", "", "")
			rewrite(t, root, "db/queries/other/raw.sql",
				"", "-- name: X :one\nSELECT 1;\n")
		},
		"RLS migration removed": func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(
				root, "db/migrations/003_rls_audit_events.sql",
			)); err != nil {
				t.Fatal(err)
			}
		},
		"stale generated query code": func(t *testing.T, root string) {
			rewrite(t, root, "internal/storage/queries/audit/audit.sql.go",
				"ORDER BY seq", "ORDER BY type")
		},
	} {
		t.Run(name, func(t *testing.T) {
			root, _, metadata := sqlcFixture(t)
			mutate(t, root)

			found := inspectAuthoredSQL(root, metadata)
			if !candidateReviewHas(found, "ARCH612", "") {
				t.Fatalf("bypass accepted: %+v", found)
			}
		})
	}
}

func TestAuthoredSQLFailsClosedWithoutSQLC(t *testing.T) {
	root, _, metadata := sqlcFixture(t)
	t.Setenv("PATH", t.TempDir())

	found := inspectAuthoredSQL(root, metadata)
	if !candidateReviewHas(found, "ARCH612", "pluto.toml") {
		t.Fatalf("missing sqlc accepted: %+v", found)
	}
}

func TestAuthoredSQLSkippedWithoutDeclarations(t *testing.T) {
	metadata := []byte("[boundary]\nmode=\"candidate\"\n")
	if found := inspectAuthoredSQL(t.TempDir(), metadata); len(found) != 0 {
		t.Fatalf("undeclared project checked: %+v", found)
	}
}

func rewrite(t *testing.T, root, path, old, replacement string) {
	t.Helper()

	full := filepath.Join(root, path)

	data, err := os.ReadFile(full)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}

	content := replacement

	if old != "" {
		content = strings.Replace(string(data), old, replacement, 1)
		if content == string(data) {
			t.Fatalf("%s lacks %q", path, old)
		}
	}

	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func regenerate(t *testing.T, root string) {
	t.Helper()

	command := exec.Command("sqlc", "generate")
	command.Dir = root

	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("sqlc generate: %v\n%s", err, output)
	}
}
