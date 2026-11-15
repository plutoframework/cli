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

	source, err := os.ReadFile("testdata/candidate_system_runtime.go.tmpl")
	if err != nil {
		t.Fatal(err)
	}

	writeSeam(t, root, "internal/app/system_runtime_test.go", string(source))

	output := external(
		t, root, "go", "test", "-race", "-json", "-count=1",
		"-run", "^TestSystem", "./internal/app",
	)
	assertCandidateCaseNames(
		t,
		output,
		"TestSystemProvisionThenTenantRoutes",
		"TestSystemProvisionRateLimit",
		"TestSystemLookup",
		"TestSystemLookupRateLimit",
		"TestSystemRowLevelBoundaries",
	)

	assertSystemNegatives(t, root)
}

// assertSystemNegatives mutates one file, expects the canonical inspection to
// report the named check, then restores it and expects a pass again.
func assertSystemNegatives(t *testing.T, root string) {
	t.Helper()

	module := "example.test/sysapp/"
	policyFile := "db/migrations/012_rls_orgs.sql"
	appendTo := func(extra string) func(string) string {
		return func(original string) string {
			return original + "\n" + extra
		}
	}

	cases := []seamNegative{
		{
			name:  "forged provisioning",
			check: "ARCH604",
			path:  "internal/logic/orgs/forge.go",
			mutate: func(string) string {
				return "package orgs\n\nimport \"" + module +
					"internal/scope\"\n\nfunc forge() scope.Provisioning {\n" +
					"\treturn scope.Provisioning{}\n}\n"
			},
		},
		{
			name:  "provisioning bound outside its workflow",
			check: "ARCH603",
			path:  "internal/logic/orgs/bind.go",
			mutate: func(string) string {
				return "package orgs\n\nimport \"" + module +
					"internal/scope\"\n\nvar _ = scope.BindProvisioning\n"
			},
		},
		{
			name:  "insert into a literal tenant",
			check: "ARCH612",
			path:  "db/queries/orgs/orgs.sql",
			mutate: appendTo("-- name: Sneak :exec\n" +
				"INSERT INTO users (org_id, id, role) " +
				"VALUES ('other', @id, @role);\n"),
		},
		{
			name:  "authored query over the lookup view",
			check: "ARCH612",
			path:  "db/queries/orgs/orgs.sql",
			mutate: appendTo("-- name: Peek :many\n" +
				"SELECT id, slug, name FROM pluto_lookup_orgs_byslug;\n"),
		},
		{
			name:  "missing lookup policy",
			check: "ARCH612",
			path:  policyFile,
			mutate: func(original string) string {
				head, _, _ := strings.Cut(original, "-- Generated by pluto: lookup")

				return head
			},
		},
		{
			name:  "wider permissive policy",
			check: "ARCH612",
			path:  policyFile,
			mutate: appendTo(
				"CREATE POLICY wide ON orgs FOR SELECT USING (true);\n",
			),
		},
		{
			name:  "lookup policy for all commands",
			check: "ARCH612",
			path:  policyFile,
			mutate: func(original string) string {
				return strings.Replace(original, "FOR SELECT", "FOR ALL", 1)
			},
		},
		{
			name:  "lookup policy keyed to another column",
			check: "ARCH612",
			path:  policyFile,
			mutate: func(original string) string {
				return strings.Replace(
					original,
					"USING (slug = current_setting",
					"USING (name = current_setting",
					1,
				)
			},
		},
		{
			name:  "lookup policy with a write check",
			check: "ARCH612",
			path:  policyFile,
			mutate: func(original string) string {
				return strings.Replace(
					original,
					"current_setting('app.lookup_orgs_byslug', true))",
					"current_setting('app.lookup_orgs_byslug', true)) "+
						"WITH CHECK (true)",
					1,
				)
			},
		},
	}

	runSystemNegatives(t, root, cases)
}

func runSystemNegatives(t *testing.T, root string, cases []seamNegative) {
	t.Helper()

	for _, negative := range cases {
		t.Run(negative.name, func(t *testing.T) {
			path := filepath.Join(root, filepath.FromSlash(negative.path))

			original, readErr := os.ReadFile(path)
			existed := readErr == nil

			t.Cleanup(func() {
				if existed {
					_ = os.WriteFile(path, original, 0o644)
				} else {
					_ = os.Remove(path)
				}
			})

			writeSeam(t, root, negative.path, negative.mutate(string(original)))

			if !seamDiagnostics(root)[negative.check] {
				t.Fatalf(
					"%s was not rejected by %s: %+v",
					negative.name,
					negative.check,
					project.InspectArchitecture(root).Diagnostics,
				)
			}

			if existed {
				if err := os.WriteFile(path, original, 0o644); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}

			if report := project.InspectArchitecture(root); !report.Passed {
				t.Fatalf("restored tree rejected: %+v", report.Diagnostics)
			}
		})
	}
}
