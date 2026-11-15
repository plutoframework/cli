package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

// tenantJobsFile copies a tenant job fixture into the generated application.
func tenantJobsFile(t *testing.T, root, fixture, target string) {
	t.Helper()

	source, err := os.ReadFile(filepath.Join("testdata", "tenantjobs", fixture))
	if err != nil {
		t.Fatal(err)
	}

	writeSeam(t, root, target, string(source))
}

// tenantJobsApp generates the tenantjobs fixture: a tenant root, a members
// table, a sweep table and one scheduled each_tenant operation whose authored
// body records a sweep row for each tenant.
func tenantJobsApp(t *testing.T) string {
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
		t.Fatal("the tenant jobs end-to-end test requires " +
			"PLUTO_CLI_TEST_DATABASE_URL")
	}

	if _, err := exec.LookPath("sqlc"); err != nil {
		t.Fatal("sqlc is required:", err)
	}

	root := temporary(t)

	args := []string{
		"--no-input", "init", ".",
		"--name", "tjapp",
		"--module", "example.test/tenantjobs",
		"--env-prefix", "TJAPP_",
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

	tenantJobsFile(t, root, "010_orgs.sql", "db/migrations/010_orgs.sql")
	tenantJobsFile(t, root, "011_users.sql", "db/migrations/011_users.sql")
	tenantJobsFile(t, root, "012_sweeps.sql", "db/migrations/012_sweeps.sql")
	tenantJobsFile(t, root, "orgs.sql", "db/queries/orgs/orgs.sql")
	tenantJobsFile(t, root, "blueprint.toml", "tenantjobs-blueprint.toml")

	mustInvoke(t, root, "--no-input", "add", "blueprint", "--file",
		filepath.Join(root, "tenantjobs-blueprint.toml"))

	for _, path := range []string{
		"internal/adapters/orgs/sweep_store.go",
		"internal/logic/orgs/sweep.go",
		"internal/operations/orgs/sweep_gate_gen.go",
		"internal/operations/orgs/sweep_workflow_gen.go",
		"internal/storage/tenantindex/index_gen.go",
	} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Fatalf("blueprint did not create %s: %v", path, err)
		}
	}

	// The tenant index policy and view ride with the root table's forced
	// row-level security migration.
	index, err := os.ReadFile(
		filepath.Join(root, migrationPath(t, root, "rls_orgs")),
	)
	if err != nil || !strings.Contains(string(index), "pluto_tenant_index") {
		t.Fatalf("root migration lacks the tenant index: %v", err)
	}

	for fixture, target := range map[string]string{
		"rules.go.txt":       "internal/rules/orgs/rules.go",
		"sweep.go.txt":       "internal/logic/orgs/sweep.go",
		"sweep_store.go.txt": "internal/adapters/orgs/sweep_store.go",
	} {
		tenantJobsFile(t, root, fixture, target)
	}

	external(t, root, "sqlc", "generate")
	external(t, root, "go", "mod", "tidy")
	mustInvoke(t, root, "fmt")
	external(t, root, "go", "run", "./cmd/openapi")
	external(t, root, "sqlc", "diff")

	if report := project.InspectArchitecture(root); !report.Passed {
		t.Fatalf("tenant jobs architecture: %+v", report.Diagnostics)
	}

	mustInvoke(t, root, "vet")
	lintCandidateManaged(t, root)

	return root
}

func TestCandidateTenantJobsEndToEnd(t *testing.T) {
	root := tenantJobsApp(t)

	// The system runtime fixture supplies the real sessions and a runtime
	// database role that owns nothing and cannot bypass row-level security.
	system, err := os.ReadFile("testdata/candidate_system_runtime.go.tmpl")
	if err != nil {
		t.Fatal(err)
	}

	writeSeam(
		t, root, "internal/app/system_runtime_test.go",
		strings.ReplaceAll(
			string(system), "example.test/sysapp", "example.test/tenantjobs",
		),
	)

	runtime, err := os.ReadFile("testdata/candidate_tenantjobs_runtime.go.tmpl")
	if err != nil {
		t.Fatal(err)
	}

	writeSeam(
		t, root, "internal/app/tenantjobs_runtime_test.go", string(runtime),
	)
	tenantJobsFile(
		t, root, "enqueue_test.go.txt", "internal/scope/enqueue_test.go",
	)

	output := external(
		t, root, "go", "test", "-race", "-json", "-count=1",
		"-run", "^(TestTenantJobs|TestTxEnqueue)", "./internal/app",
		"./internal/scope",
	)
	assertCandidateCaseNames(
		t, output,
		"TestTenantJobsEachTenantScheduled",
		"TestTenantJobsRejectsForgedDeliveries",
		"TestTenantJobsIndexIsNarrow",
		"TestTxEnqueue",
	)

	if report := project.InspectArchitecture(root); !report.Passed {
		t.Fatalf("architecture with tests: %+v", report.Diagnostics)
	}

	assertTenantJobNegatives(
		t, root, "example.test/tenantjobs/",
		migrationPath(t, root, "rls_orgs"),
	)
}

// assertTenantJobNegatives mutates one file, expects the canonical inspection
// to report the named check, then restores it and expects a pass again.
func assertTenantJobNegatives(t *testing.T, root, module, policyFile string) {
	t.Helper()

	appendTo := func(extra string) func(string) string {
		return func(original string) string {
			return original + "\n" + extra
		}
	}

	cases := []seamNegative{
		{
			name:  "logic reads the tenant index",
			check: "ARCH610",
			path:  "internal/logic/orgs/index.go",
			mutate: func(string) string {
				return "package orgs\n\nimport _ \"" + module +
					"internal/storage/tenantindex\"\n"
			},
		},
		{
			name:  "adapter reads the tenant index",
			check: "ARCH610",
			path:  "internal/adapters/orgs/index.go",
			mutate: func(string) string {
				return "package orgs\n\nimport _ \"" + module +
					"internal/storage/tenantindex\"\n"
			},
		},
		{
			name:  "logic reaches the generated job gate",
			check: "ARCH610",
			path:  "internal/logic/orgs/gate.go",
			mutate: func(string) string {
				return "package orgs\n\nimport _ \"" + module +
					"internal/operations/orgs\"\n"
			},
		},
		{
			name:  "body binds its own scope",
			check: "ARCH603",
			path:  "internal/logic/orgs/bind.go",
			mutate: func(string) string {
				return "package orgs\n\nimport \"" + module +
					"internal/scope\"\n\nvar _ = scope.Bind\n"
			},
		},
		{
			name:  "body forges an execution",
			check: "ARCH604",
			path:  "internal/logic/orgs/forge.go",
			mutate: func(string) string {
				return "package orgs\n\nimport \"" + module +
					"internal/scope\"\n\nfunc forge() scope.Execution {\n" +
					"\treturn scope.Execution{}\n}\n"
			},
		},
		{
			name:   "body spawns a goroutine",
			check:  "ARCH611",
			path:   "internal/logic/orgs/sweep.go",
			mutate: appendTo("func spawn() { go func() {}() }\n"),
		},
		{
			name:  "hand-edited gate skips the tenant check",
			check: "ARCH202",
			path:  "internal/operations/orgs/sweep_gate_gen.go",
			mutate: func(original string) string {
				return strings.Replace(
					original, "if !found {", "if false {", 1,
				)
			},
		},
		{
			name:  "hand-edited index leaves its switch off",
			check: "ARCH202",
			path:  "internal/storage/tenantindex/index_gen.go",
			mutate: func(original string) string {
				return strings.Replace(
					original, "'on', true", "'on', false", 1,
				)
			},
		},
		{
			name:  "authored query over the tenant index",
			check: "ARCH612",
			path:  "db/queries/orgs/orgs.sql",
			mutate: appendTo("-- name: Peek :many\n" +
				"SELECT tenant_id FROM pluto_tenant_index;\n"),
		},
		{
			name:  "authored query across tenants",
			check: "ARCH612",
			path:  "db/queries/orgs/orgs.sql",
			mutate: appendTo("-- name: All :one\n" +
				"SELECT count(*) FROM sweeps;\n"),
		},
		{
			name:  "missing tenant index policy",
			check: "ARCH612",
			path:  policyFile,
			mutate: func(original string) string {
				head, _, _ := strings.Cut(
					original,
					"-- Generated by pluto: the tenant index",
				)

				return head
			},
		},
		{
			name:  "tenant index policy always on",
			check: "ARCH612",
			path:  policyFile,
			mutate: func(original string) string {
				return strings.Replace(
					original,
					"current_setting('app.tenant_index', true) = 'on'",
					"true",
					1,
				)
			},
		},
		{
			name:  "wider permissive policy next to the index",
			check: "ARCH612",
			path:  policyFile,
			mutate: appendTo(
				"CREATE POLICY wide ON orgs FOR SELECT USING (true);\n",
			),
		},
	}

	runSystemNegatives(t, root, cases)
}
