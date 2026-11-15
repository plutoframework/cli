package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runtimeRoleFixture(t *testing.T) (Manifest, string) {
	t.Helper()

	m := seamFixture(t)
	m.Tables[0].AppendOnly = true
	m.Database.RuntimeRole = "app_runtime"

	root := t.TempDir()
	dir := filepath.Join(root, "db/migrations")

	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}

	for name, sql := range map[string]string{
		"002_auth.sql":  `CREATE TABLE pluto_auth_sessions (id text);`,
		"010_users.sql": `CREATE TABLE users (id text);`,
		"011_audit.sql": `CREATE TABLE audit_events (id text);`,
		"003_idem.sql":  `CREATE TABLE IF NOT EXISTS "pluto_idempotency" (k text);`,
		"099_other.txt": `ignored`,
		"012_notpl.sql": `CREATE TABLE other_app_table (id text);`,
	} {
		if err := os.WriteFile(
			filepath.Join(dir, name), []byte(sql), 0o600,
		); err != nil {
			t.Fatal(err)
		}
	}

	return m, root
}

func TestRuntimeRoleMigrationIsLeastPrivilege(t *testing.T) {
	m, root := runtimeRoleFixture(t)

	proposals, err := ProposeRuntimeRoleMigration(root, m, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(proposals) != 1 {
		t.Fatalf("want one proposal, got %d", len(proposals))
	}

	var path, sql string
	for key, value := range proposals {
		path, sql = key, string(value)
	}

	if path != "db/migrations/013_runtime_role.sql" {
		t.Fatalf("unexpected path %s", path)
	}

	for _, want := range []string{
		"CREATE ROLE app_runtime NOLOGIN",
		"NOSUPERUSER NOBYPASSRLS",
		"GRANT SELECT, INSERT ON audit_events TO app_runtime;",
		"GRANT SELECT, INSERT, UPDATE, DELETE ON users TO app_runtime;",
		"ON pluto_auth_sessions TO app_runtime;",
		"ON pluto_idempotency TO app_runtime;",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("missing %q in:\n%s", want, sql)
		}
	}

	if strings.Contains(sql, "other_app_table") ||
		strings.Contains(sql, "PASSWORD '") &&
			!strings.Contains(sql, "PASSWORD '...'") {
		t.Fatalf("unexpected grant or secret:\n%s", sql)
	}

	if strings.Contains(sql, "UPDATE, DELETE ON audit_events") ||
		strings.Contains(sql, "OWNER") {
		t.Fatalf("append-only table over-granted:\n%s", sql)
	}
}

func TestRuntimeRoleMigrationIsIdempotentAndNumbersAfterTaken(t *testing.T) {
	m, root := runtimeRoleFixture(t)

	taken := map[string][]byte{"db/migrations/013_rls_users.sql": nil}

	proposals, err := ProposeRuntimeRoleMigration(root, m, taken)
	if err != nil {
		t.Fatal(err)
	}

	for path, sql := range proposals {
		if path != "db/migrations/014_runtime_role.sql" {
			t.Fatalf("unexpected path %s", path)
		}

		if err := os.WriteFile(
			filepath.Join(root, path), sql, 0o600,
		); err != nil {
			t.Fatal(err)
		}
	}

	again, err := ProposeRuntimeRoleMigration(root, m, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(again) != 0 {
		t.Fatalf("applied grants must not be re-proposed: %v", again)
	}

	m.Database.RuntimeRole = ""

	none, err := ProposeRuntimeRoleMigration(root, m, nil)
	if err != nil || len(none) != 0 {
		t.Fatalf("no runtime role means no proposal: %v %v", none, err)
	}
}

func TestRuntimeRoleGuardGeneratedAndWired(t *testing.T) {
	m := seamFixture(t)
	m.Database.RuntimeRole = "app_runtime"

	source := candidateRuntimeRoleGuard(m)

	formatted, err := formatGenerated(source, m.Project.Module)
	if err != nil {
		t.Fatalf("guard does not format: %v", err)
	}

	for _, want := range []string{
		`"audit_events"`,
		`runtimeRole = "app_runtime"`,
		"rolbypassrls",
		"pg_has_role(current_user, c.relowner",
		"relforcerowsecurity",
	} {
		if !strings.Contains(string(formatted), want) {
			t.Fatalf("guard missing %q:\n%s", want, formatted)
		}
	}

	if strings.Contains(strings.ToLower(string(formatted)), "override") &&
		!strings.Contains(string(formatted), "there is no override") {
		t.Fatal("guard must not offer an override")
	}

	files, err := candidateManaged(m)
	if err != nil {
		t.Skipf("fixture is not a full candidate manifest: %v", err)
	}

	if _, ok := files["internal/storage/postgres/runtime_role_gen.go"]; !ok {
		t.Fatal("guard file not generated")
	}

	for _, path := range []string{"cmd/api/main.go", "cmd/worker/main.go"} {
		if !strings.Contains(
			string(files[path]), "postgres.VerifyRuntimeRole(",
		) {
			t.Fatalf("%s does not verify the runtime role", path)
		}
	}
}
