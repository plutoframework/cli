package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

// gatewayFile copies a gateway fixture into the generated application.
func gatewayFile(t *testing.T, root, fixture, target string) {
	t.Helper()

	source, err := os.ReadFile(filepath.Join("testdata", "gateway", fixture))
	if err != nil {
		t.Fatal(err)
	}

	writeSeam(t, root, target, string(source))
}

// gatewayApp generates the integrated gateway application: a tenant root,
// user memberships, a machine principal with signed operations (object, page
// and stream), authored write endpoints behind a default-off flag with one
// idempotent write, a per-tenant job, and recorded authentication events.
func gatewayApp(t *testing.T) string {
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
		t.Fatal("the gateway end-to-end test requires " +
			"PLUTO_CLI_TEST_DATABASE_URL")
	}

	if _, err := exec.LookPath("sqlc"); err != nil {
		t.Fatal("sqlc is required:", err)
	}

	root := temporary(t)

	args := []string{
		"--no-input", "init", ".",
		"--name", "gwapp",
		"--module", "example.test/gwapp",
		"--env-prefix", "GWAPP_",
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

	gatewayFile(t, root, "010_gateways.sql", "db/migrations/010_gateways.sql")
	gatewayFile(t, root, "gateways.sql", "db/queries/gateways/gateways.sql")
	gatewayFile(t, root, "blueprint.toml", "gateway-blueprint.toml")

	mustInvoke(t, root, "--no-input", "add", "blueprint", "--file",
		filepath.Join(root, "gateway-blueprint.toml"))

	gatewayFile(t, root, "rules.go.txt", "internal/rules/gateways/rules.go")

	for _, action := range []string{
		"create", "rename", "revoke", "rotate", "heartbeat", "status",
		"history", "channel", "markstale",
	} {
		gatewayFile(
			t, root, action+".go.txt",
			"internal/logic/gateways/"+action+".go",
		)
	}

	gatewayFile(
		t, root, "record.go.txt", "internal/logic/gateways/recordauthevent.go",
	)
	gatewayFile(t, root, "store.go.txt", "internal/adapters/gateways/store.go")
	gatewayFile(
		t, root, "audit_store.go.txt", "internal/adapters/gateways/audit_store.go",
	)

	external(t, root, "sqlc", "generate")
	external(t, root, "go", "mod", "tidy")
	mustInvoke(t, root, "fmt")
	external(t, root, "go", "run", "./cmd/openapi")
	external(t, root, "sqlc", "diff")

	if report := project.InspectArchitecture(root); !report.Passed {
		t.Fatalf("gateway architecture: %+v", report.Diagnostics)
	}

	mustInvoke(t, root, "vet")
	lintCandidateManaged(t, root)

	return root
}

func TestCandidateGatewayEndToEnd(t *testing.T) {
	root := gatewayApp(t)
	t.Log(root)
}
