package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

// machineFile copies a machine fixture into the generated application.
func machineFile(t *testing.T, root, fixture, target string) {
	t.Helper()

	source, err := os.ReadFile(filepath.Join("testdata", "machine", fixture))
	if err != nil {
		t.Fatal(err)
	}

	writeSeam(t, root, target, string(source))
}

// machineApp generates the gwapp fixture: a gateway principal that enrolls
// with a single-use token and then authenticates with signed requests.
func machineApp(t *testing.T) string {
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
		t.Fatal("the machine end-to-end test requires " +
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

	machineFile(t, root, "010_gateways.sql", "db/migrations/010_gateways.sql")
	machineFile(t, root, "gateways.sql", "db/queries/gateways/gateways.sql")
	machineFile(t, root, "blueprint.toml", "machine-blueprint.toml")

	mustInvoke(t, root, "--no-input", "add", "blueprint", "--file",
		filepath.Join(root, "machine-blueprint.toml"))

	for _, path := range []string{
		"db/migrations/011_rls_gateways.sql",
		"db/migrations/012_rls_users.sql",
		"db/migrations/101_machine_identity.sql",
		"internal/storage/machineauth/machineauth_gen.go",
		"internal/adapters/gateways/store.go",
		"internal/logic/gateways/heartbeat.go",
	} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Fatalf("blueprint did not create %s: %v", path, err)
		}
	}

	machineFile(t, root, "rules.go.txt", "internal/rules/gateways/rules.go")
	machineFile(t, root, "create.go.txt", "internal/logic/gateways/create.go")
	machineFile(
		t, root, "heartbeat.go.txt", "internal/logic/gateways/heartbeat.go",
	)
	machineFile(t, root, "status.go.txt", "internal/logic/gateways/status.go")
	machineFile(t, root, "store.go.txt", "internal/adapters/gateways/store.go")

	external(t, root, "sqlc", "generate")
	external(t, root, "go", "mod", "tidy")
	mustInvoke(t, root, "fmt")
	external(t, root, "go", "run", "./cmd/openapi")
	external(t, root, "sqlc", "diff")

	if report := project.InspectArchitecture(root); !report.Passed {
		t.Fatalf("machine architecture: %+v", report.Diagnostics)
	}

	mustInvoke(t, root, "vet")
	lintCandidateManaged(t, root)

	return root
}

func TestCandidateMachineEndToEnd(t *testing.T) {
	root := machineApp(t)

	source, err := os.ReadFile("testdata/candidate_machine_runtime.go.tmpl")
	if err != nil {
		t.Fatal(err)
	}

	writeSeam(t, root, "internal/app/machine_runtime_test.go", string(source))

	helpers, err := os.ReadFile("testdata/candidate_machine_helpers.go.tmpl")
	if err != nil {
		t.Fatal(err)
	}

	writeSeam(
		t, root, "internal/app/machine_helpers_test.go", string(helpers),
	)

	output := external(
		t, root, "go", "test", "-race", "-json", "-count=1",
		"-run", "^TestMachine", "./internal/app",
	)
	assertCandidateCaseNames(
		t,
		output,
		"TestMachineEnrollAndSign",
		"TestMachineSignatureRejections",
		"TestMachineReplayAndWindow",
		"TestMachineRevocation",
		"TestMachineEnrollmentRejections",
		"TestMachineEnrollmentSingleUse",
		"TestMachineEnrollRateLimit",
		"TestMachineTenantIsolation",
		"TestMachineRowLevelBoundaries",
	)

	assertMachineGenerated(t, root)
	assertMachineNegatives(t, root, "example.test/gwapp/")
}

// assertMachineGenerated checks the managed surface that tests cannot reach
// through HTTP: the contract documents the machine scheme, and user routes
// stay out of the machine permission set.
func assertMachineGenerated(t *testing.T, root string) {
	t.Helper()

	spec, err := os.ReadFile(filepath.Join(root, "openapi", "openapi.json"))
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"machineSignature", "x-pluto-machine", "X-Pluto-Signature",
	} {
		if !strings.Contains(string(spec), want) {
			t.Fatalf("openapi misses %s", want)
		}
	}

	files, err := filepath.Glob(
		filepath.Join(root, "internal", "access", "*.go"),
	)
	if err != nil || len(files) == 0 {
		t.Fatal("missing access package", err)
	}

	for _, file := range files {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}

		if strings.Contains(string(source), "gateway.heartbeat") {
			t.Fatalf("%s lists a machine permission as a user permission", file)
		}
	}
}

// assertMachineNegatives mutates one file, expects the canonical inspection
// to report the named check, then restores it and expects a pass again.
func assertMachineNegatives(t *testing.T, root, module string) {
	t.Helper()

	appendTo := func(extra string) func(string) string {
		return func(original string) string {
			return original + "\n" + extra
		}
	}

	policyFile := "db/migrations/011_rls_gateways.sql"

	cases := []seamNegative{
		{
			name:  "forged machine execution",
			check: "ARCH604",
			path:  "internal/logic/gateways/forge.go",
			mutate: func(string) string {
				return "package gateways\n\nimport \"" + module +
					"internal/scope\"\n\nfunc forge() scope.Execution {\n" +
					"\treturn scope.Execution{}\n}\n"
			},
		},
		{
			name:  "forged verified machine",
			check: "ARCH603",
			path:  "internal/logic/gateways/verified.go",
			mutate: func(string) string {
				return "package gateways\n\nimport \"" + module +
					"internal/ports/machineauth\"\n\n" +
					"var _ = machineauth.NewVerified\n"
			},
		},
		{
			name:  "machine bound outside its workflow",
			check: "ARCH603",
			path:  "internal/logic/gateways/bind.go",
			mutate: func(string) string {
				return "package gateways\n\nimport \"" + module +
					"internal/scope\"\n\nvar _ = scope.BindMachine\n"
			},
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
		{
			name:  "authored query over enrollment tokens",
			check: "ARCH612",
			path:  "db/queries/gateways/gateways.sql",
			mutate: appendTo("-- name: Peek :many\n" +
				"SELECT token_hash FROM pluto_machine_enrollments;\n"),
		},
		{
			name:  "authored query over the machine lookup view",
			check: "ARCH612",
			path:  "db/queries/gateways/gateways.sql",
			mutate: appendTo("-- name: Peek :many\n" +
				"SELECT id FROM pluto_lookup_gateways_gatewaymachine;\n"),
		},
		{
			name:  "missing machine lookup policy",
			check: "ARCH612",
			path:  policyFile,
			mutate: func(original string) string {
				head, _, _ := strings.Cut(
					original,
					"-- Generated by pluto: lookup",
				)

				return head
			},
		},
		{
			name:  "wider permissive policy on the machine table",
			check: "ARCH612",
			path:  policyFile,
			mutate: appendTo(
				"CREATE POLICY wide ON gateways FOR SELECT USING (true);\n",
			),
		},
		{
			name:  "machine lookup policy for all commands",
			check: "ARCH612",
			path:  policyFile,
			mutate: func(original string) string {
				return strings.Replace(original, "FOR SELECT", "FOR ALL", 1)
			},
		},
		{
			name:  "machine lookup keyed to another column",
			check: "ARCH612",
			path:  policyFile,
			mutate: func(original string) string {
				return strings.Replace(
					original,
					"USING (id::text = current_setting",
					"USING (name::text = current_setting",
					1,
				)
			},
		},
	}

	runSystemNegatives(t, root, cases)
}
