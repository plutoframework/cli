package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func migrationFixture(t *testing.T, framework map[string]string) (
	Manifest, string,
) {
	t.Helper()

	m := manifestContractFixture(t)
	m.SchemaVersion = SchemaVersion
	m.Project = Project{
		Name:      "ownership",
		Module:    "example.test/ownership",
		EnvPrefix: "OWNERSHIP_",
	}
	m.Database = Database{Engine: "postgres", Queries: "sqlc"}
	m.Capabilities.Cache = "none"
	m.Capabilities.LocalCredentials = true
	m.Migrations.Framework = framework

	files, err := candidateManaged(m)
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	for path, source := range files {
		writeMigrationFile(t, root, path, string(source))
	}

	return m, root
}

func writeMigrationFile(t *testing.T, root, path, content string) {
	t.Helper()

	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFrameworkMigrationNumbersFollowTheManifest(t *testing.T) {
	m, _ := migrationFixture(t, nil)

	files, err := candidateManaged(m)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := files["db/migrations/008_credential_attempts.sql"]; !ok {
		t.Fatal("new applications keep the default number")
	}

	m.Migrations.Framework = map[string]string{"credential_attempts": "009"}

	files, err = candidateManaged(m)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := files["db/migrations/009_credential_attempts.sql"]; !ok {
		t.Fatal("recorded number not used")
	}

	if _, ok := files["db/migrations/008_credential_attempts.sql"]; ok {
		t.Fatal("default path still generated")
	}

	initial, err := InitialFiles(m)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := initial["db/migrations/008_credential_attempts.sql"]; ok {
		t.Fatal("InitialFiles regenerated the default path")
	}

	if !strings.Contains(
		string(initial["pluto.toml"]),
		"credential_attempts = '009'",
	) {
		t.Fatalf("number not encoded:\n%s", initial["pluto.toml"])
	}

	m.Migrations = Migrations{}

	fresh, err := InitialFiles(m)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(fresh["pluto.toml"]), "migrations") {
		t.Fatal("new applications must not record migration numbers")
	}
}

func TestFrameworkMigrationValidation(t *testing.T) {
	attempts := func(number string) map[string]string {
		return map[string]string{"credential_attempts": number}
	}

	type rejection struct {
		framework map[string]string
		want      string
	}

	rejected := map[string]rejection{
		"unknown":  {map[string]string{"nope": "009"}, "unknown framework"},
		"format":   {attempts("9"), "three digit"},
		"reserved": {attempts("099"), "002 to 098"},
		"initial":  {attempts("001"), "002 to 098"},
		"clash":    {attempts("005"), "used by both"},
	}

	for name, test := range rejected {
		t.Run(name, func(t *testing.T) {
			m, _ := migrationFixture(t, nil)
			m.Migrations.Framework = test.framework

			err := m.Validate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want %q", err, test.want)
			}
		})
	}

	m, _ := migrationFixture(t, nil)
	m.Boundary.Mode = "published"
	m.Migrations.Framework = map[string]string{"credential_attempts": "009"}

	if err := m.Validate(); err == nil ||
		!strings.Contains(err.Error(), "candidate boundary profile") {
		t.Fatalf("published profile accepted framework numbers: %v", err)
	}
}

func TestMigrationOwnershipARCH613(t *testing.T) {
	m, root := migrationFixture(
		t,
		map[string]string{"credential_attempts": "009"},
	)

	// gryt's shape: an authored migration beside framework ones.
	writeMigrationFile(t, root, "db/migrations/008_core_model.sql", "-- core\n")

	if diagnostics := candidateIntegrity(root, m); len(diagnostics) != 0 {
		t.Fatalf("canonical positive failed: %v", diagnostics)
	}

	stray := "db/migrations/008_credential_attempts.sql"
	writeMigrationFile(t, root, stray, "-- stray\n")

	expectARCH613(t, root, m, stray, "records 009")

	if err := os.Remove(filepath.Join(root, stray)); err != nil {
		t.Fatal(err)
	}

	duplicate := "db/migrations/009_other.sql"
	writeMigrationFile(t, root, duplicate, "-- other\n")

	expectARCH613(t, root, m, duplicate, "shared with")
}

func expectARCH613(
	t *testing.T,
	root string,
	m Manifest,
	file, message string,
) {
	t.Helper()

	for _, diagnostic := range candidateIntegrity(root, m) {
		if diagnostic.ID == "ARCH613" && diagnostic.File == file &&
			strings.Contains(diagnostic.Message, message) {
			return
		}
	}

	t.Fatalf("no ARCH613 %q for %s in %v", message, file,
		candidateIntegrity(root, m))
}
