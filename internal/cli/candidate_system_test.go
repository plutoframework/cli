package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

// systemFile copies a system-seam fixture into the generated application.
func systemFile(t *testing.T, root, fixture, target string) {
	t.Helper()

	source, err := os.ReadFile(filepath.Join("testdata", "system", fixture))
	if err != nil {
		t.Fatal(err)
	}

	writeSeam(t, root, target, string(source))
}

// systemApp generates the sysapp fixture: a tenant root, a provisioning
// operation, a generated slug lookup and one tenant-scoped read.
func systemApp(t *testing.T) string {
	t.Helper()

	if os.Getenv("PLUTO_CLI_CANDIDATE_PROFILE") != "1" {
		t.Skip("explicit local candidate profile disabled")
	}

	for _, name := range []string{"AUTH", "RBAC", "CORE", "JOBS"} {
		if os.Getenv("PLUTO_CLI_CANDIDATE_"+name+"_SOURCE") == "" {
			t.Fatal("missing explicit candidate source", name)
		}
	}

	if os.Getenv("PLUTO_CLI_TEST_DATABASE_URL") == "" {
		t.Fatal("the system end-to-end test requires " +
			"PLUTO_CLI_TEST_DATABASE_URL")
	}

	if _, err := exec.LookPath("sqlc"); err != nil {
		t.Fatal("sqlc is required:", err)
	}

	root := temporary(t)

	args := []string{
		"--no-input", "init", ".",
		"--name", "sysapp",
		"--module", "example.test/sysapp",
		"--env-prefix", "SYSAPP_",
		"--multi-tenant", "--local-credentials", "--oidc",
		"--no-mise", "--no-fnox", "--no-docker",
		"--boundary-profile", "candidate",
	}

	for _, name := range []string{"AUTH", "RBAC", "CORE", "JOBS"} {
		args = append(
			args,
			"--candidate-"+strings.ToLower(name)+"-source",
			os.Getenv("PLUTO_CLI_CANDIDATE_"+name+"_SOURCE"),
		)
	}

	mustInvoke(t, root, args...)

	systemFile(t, root, "010_orgs.sql", "db/migrations/010_orgs.sql")
	systemFile(t, root, "011_users.sql", "db/migrations/011_users.sql")
	systemFile(t, root, "orgs.sql", "db/queries/orgs/orgs.sql")
	systemFile(t, root, "blueprint.toml", "system-blueprint.toml")

	mustInvoke(t, root, "--no-input", "add", "blueprint", "--file",
		filepath.Join(root, "system-blueprint.toml"))

	for _, path := range []string{
		"db/migrations/012_rls_orgs.sql",
		"db/migrations/013_rls_users.sql",
		"internal/adapters/orgs/tenant_store.go",
		"internal/logic/orgs/provision.go",
	} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Fatalf("blueprint did not create %s: %v", path, err)
		}
	}

	systemFile(t, root, "rules.go.txt", "internal/rules/orgs/rules.go")
	systemFile(t, root, "provision.go.txt", "internal/logic/orgs/provision.go")
	systemFile(t, root, "count.go.txt", "internal/logic/orgs/count.go")
	systemFile(
		t,
		root,
		"tenant_store.go.txt",
		"internal/adapters/orgs/tenant_store.go",
	)

	external(t, root, "sqlc", "generate")
	external(t, root, "go", "mod", "tidy")
	mustInvoke(t, root, "fmt")
	external(t, root, "go", "run", "./cmd/openapi")
	external(t, root, "sqlc", "diff")

	if report := project.InspectArchitecture(root); !report.Passed {
		t.Fatalf("system architecture: %+v", report.Diagnostics)
	}

	mustInvoke(t, root, "vet")
	lintCandidateManaged(t, root)

	return root
}

func TestCandidateSystemSeamEndToEnd(t *testing.T) {
	root := systemApp(t)

	external(t, root, "go", "build", "./...")
}
