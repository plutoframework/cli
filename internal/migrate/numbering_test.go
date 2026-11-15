package migrate

import (
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

func credentialManifest() project.Manifest {
	m := publishedManifest("multi")
	m.Capabilities.LocalCredentials = true

	return m
}

// gryt's migrations: published foundation 001-007, its own 008_core_model,
// and generated-range files 099 and 100.
func grytFixture(t *testing.T) string {
	t.Helper()

	root := publishedFixture(t, credentialManifest())
	writeTree(t, root, map[string]string{
		"db/migrations/008_core_model.sql":   "CREATE TABLE core ();\n",
		"db/migrations/099_memberships.sql":  "CREATE TABLE m ();\n",
		"db/migrations/100_orders_order.sql": "CREATE TABLE o ();\n",
	})

	return root
}

func TestMigrateAllocatesAfterExistingNumbers(t *testing.T) {
	root := grytFixture(t)
	before := map[string]string{}

	for _, name := range []string{
		"db/migrations/002_auth_sessions.sql",
		"db/migrations/008_core_model.sql",
		"db/migrations/099_memberships.sql",
		"db/migrations/100_orders_order.sql",
	} {
		before[name] = readTree(t, root, name)
	}

	plan, err := Build(
		root,
		ProfileCandidate,
		Options{Sources: candidateSources(t)},
	)
	if err != nil {
		t.Fatal(err)
	}

	if plan.Blocked {
		t.Fatalf("blocked: %+v", plan.Manual)
	}

	if err := Write(plan); err != nil {
		t.Fatal(err)
	}

	if readTree(t, root, "db/migrations/009_credential_attempts.sql") ==
		"<missing>" {
		t.Fatal("009_credential_attempts.sql not generated")
	}

	if readTree(t, root, "db/migrations/008_credential_attempts.sql") !=
		"<missing>" {
		t.Fatal("default number generated beside authored 008")
	}

	for name, content := range before {
		if readTree(t, root, name) != content {
			t.Fatalf("existing migration %s changed", name)
		}
	}

	_, manifest, err := project.Discover(root)
	if err != nil {
		t.Fatal(err)
	}

	if got := manifest.Migrations.Framework; len(got) != 1 ||
		got["credential_attempts"] != "009" {
		t.Fatalf("recorded numbers %v", got)
	}

	if diagnostics := project.InspectArchitecture(root).Diagnostics; hasID(
		diagnostics,
		"ARCH613",
	) {
		t.Fatalf("ARCH613 after migrate: %v", diagnostics)
	}

	again, err := Build(
		root,
		ProfileCandidate,
		Options{Sources: candidateSources(t)},
	)
	if err != nil || len(again.Paths()) != 0 {
		t.Fatalf("not idempotent: %v %v", err, again.Paths())
	}
}

func TestMigrateKeepsDefaultsWhenFree(t *testing.T) {
	root := publishedFixture(t, credentialManifest())

	plan, err := Build(
		root,
		ProfileCandidate,
		Options{Sources: candidateSources(t)},
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := Write(plan); err != nil {
		t.Fatal(err)
	}

	if readTree(t, root, "db/migrations/008_credential_attempts.sql") ==
		"<missing>" {
		t.Fatal("default number not used")
	}

	if strings.Contains(readTree(t, root, "pluto.toml"), "migrations") {
		t.Fatal("defaults must not be recorded")
	}
}

func TestMigrateMatchesIdenticalMigrationByContent(t *testing.T) {
	root := publishedFixture(t, credentialManifest())

	// The same schema applied under another number is matched by content.
	body := readTree(t, root, "db/migrations/004_local_credentials.sql")
	writeTree(t, root, map[string]string{
		"db/migrations/012_local_credentials.sql": body,
	})

	if err := removeFile(
		root,
		"db/migrations/004_local_credentials.sql",
	); err != nil {
		t.Fatal(err)
	}

	plan, err := Build(
		root,
		ProfileCandidate,
		Options{Sources: candidateSources(t)},
	)
	if err != nil {
		t.Fatal(err)
	}

	if plan.Blocked {
		t.Fatalf("blocked: %+v", plan.Manual)
	}

	if _, ok := plan.Writes["db/migrations/004_local_credentials.sql"]; ok {
		t.Fatal("content-identical migration regenerated at the default number")
	}

	if err := Write(plan); err != nil {
		t.Fatal(err)
	}

	_, manifest, err := project.Discover(root)
	if err != nil {
		t.Fatal(err)
	}

	if manifest.Migrations.Framework["local_credentials"] != "012" {
		t.Fatalf("recorded %v", manifest.Migrations.Framework)
	}
}

func hasID(diagnostics []project.ArchitectureDiagnostic, id string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.ID == id {
			return true
		}
	}

	return false
}
