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

	for fixture, target := range map[string]string{
		"candidate_machine_helpers.go.tmpl": "internal/app/gateway_helpers_test.go",
		"candidate_gateway_runtime.go.tmpl": "internal/app/gateway_runtime_test.go",
	} {
		source, err := os.ReadFile(filepath.Join("testdata", fixture))
		if err != nil {
			t.Fatal(err)
		}

		writeSeam(t, root, target, string(source))
	}

	pattern := "^TestGateway"
	if only := os.Getenv("PLUTO_CLI_GATEWAY_RUN"); only != "" {
		pattern = only
	}

	output := external(
		t, root, "go", "test", "-race", "-json", "-count=1",
		"-run", pattern, "./internal/app",
	)
	assertCandidateCaseNames(
		t,
		output,
		"TestGatewayWriteEndpoint",
		"TestGatewayIdempotentWrite",
		"TestGatewayRotateAndRevoke",
		"TestGatewayEnrollmentAudited",
		"TestGatewayMachineRateLimit",
		"TestGatewayMachinePagination",
		"TestGatewayMachineStream",
		"TestGatewayStreamFlag",
		"TestGatewayMarkStale",
	)

	assertGatewayNegatives(t, root, "example.test/gwapp/")
}

// assertGatewayNegatives mutates one file, expects the canonical inspection to
// report the named check, then restores it.
func assertGatewayNegatives(t *testing.T, root, module string) {
	t.Helper()

	appendTo := func(extra string) func(string) string {
		return func(original string) string {
			return original + "\n" + extra
		}
	}

	query := "db/queries/gateways/gateways.sql"

	runSystemNegatives(t, root, []seamNegative{
		{
			name:  "authored query sets the machine state",
			check: "ARCH612",
			path:  query,
			mutate: appendTo("-- name: Revive :exec\n" +
				"UPDATE gateways SET state = 'active'\n" +
				"WHERE org_id = @org_id AND id = @id;\n"),
		},
		{
			name:  "authored query sets the machine key",
			check: "ARCH612",
			path:  query,
			mutate: appendTo("-- name: Rekey :exec\n" +
				"UPDATE gateways SET public_key = @public_key\n" +
				"WHERE org_id = @org_id AND id = @id;\n"),
		},
		{
			name:  "authored insert names the machine state",
			check: "ARCH612",
			path:  query,
			mutate: appendTo("-- name: Forge :one\n" +
				"INSERT INTO gateways (org_id, id, name, state)\n" +
				"VALUES (@org_id, gen_random_uuid()::text, @name, 'active')\n" +
				"RETURNING id;\n"),
		},
		{
			name:  "authored code imports the machine runtime",
			check: "ARCH610",
			path:  "internal/logic/gateways/verify.go",
			mutate: func(string) string {
				return "package gateways\n\nimport \"" + module +
					"internal/storage/machineauth\"\n\n" +
					"var _ = machineauth.NewVerifier\n"
			},
		},
	})
}
