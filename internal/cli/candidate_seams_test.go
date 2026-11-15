package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

// seamFile copies an embedded-fixture into the generated application.
func seamFile(t *testing.T, root, fixture, target string) {
	t.Helper()

	source, err := os.ReadFile(filepath.Join("testdata", "seams", fixture))
	if err != nil {
		t.Fatal(err)
	}

	writeSeam(t, root, target, string(source))
}

func writeSeam(t *testing.T, root, target, content string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(target))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// seamDiagnostics runs the canonical inspection and returns diagnostic ids.
func seamDiagnostics(root string) map[string]bool {
	found := map[string]bool{}

	for _, diagnostic := range project.InspectArchitecture(root).Diagnostics {
		found[diagnostic.ID] = true
	}

	return found
}

// A fresh candidate application gains an authored audit group declared in one
// blueprint. The generators, canonical checks, sqlc and a real Postgres prove
// the whole seam; negative fixtures prove authored code cannot bypass a gate.
func TestCandidateAuthoredSeamEndToEnd(t *testing.T) {
	if os.Getenv("PLUTO_CLI_CANDIDATE_PROFILE") != "1" {
		t.Skip("explicit local candidate profile disabled")
	}

	for _, name := range []string{"AUTH", "RBAC", "CORE", "JOBS"} {
		if os.Getenv("PLUTO_CLI_CANDIDATE_"+name+"_SOURCE") == "" {
			t.Fatal("missing explicit candidate source", name)
		}
	}

	if os.Getenv("PLUTO_CLI_TEST_DATABASE_URL") == "" {
		t.Fatal("the seam end-to-end test requires PLUTO_CLI_TEST_DATABASE_URL")
	}

	// ARCH612 fails closed without sqlc, and so does this test.
	if _, err := exec.LookPath("sqlc"); err != nil {
		t.Fatal("sqlc is required:", err)
	}

	root := temporary(t)

	args := []string{
		"--no-input", "init", ".",
		"--name", "auditapp",
		"--module", "example.test/auditapp",
		"--env-prefix", "AUDITAPP_",
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

	// Authored schema and queries come first: the blueprint proposes the
	// row-level security and append-only migrations for tables that exist.
	seamFile(t, root, "010_users.sql", "db/migrations/010_users.sql")
	seamFile(
		t,
		root,
		"011_audit_events.sql",
		"db/migrations/011_audit_events.sql",
	)
	seamFile(t, root, "events.sql", "db/queries/audit/events.sql")
	seamFile(t, root, "approvals.sql", "db/queries/approvals/requests.sql")
	systemFileAs(
		t, root, "example.test/auditapp", "orgs.sql", "db/queries/orgs/orgs.sql",
	)
	seamFile(t, root, "blueprint.toml", "audit-blueprint.toml")

	mustInvoke(t, root, "--no-input", "add", "blueprint", "--file",
		filepath.Join(root, "audit-blueprint.toml"))

	for _, path := range []string{
		"internal/storage/postgres/runtime_role_gen.go",
		"internal/adapters/audit/event_store.go",
		"internal/logic/audit/list.go",
		"internal/adapters/approvals/request_store.go",
		"internal/logic/approvals/approve.go",
	} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Fatalf("blueprint did not create %s: %v", path, err)
		}
	}

	for _, name := range []string{
		"rls_users", "rls_orgs", "rls_approval_requests", "rls_audit_events",
		"runtime_role",
	} {
		matches, err := filepath.Glob(
			filepath.Join(root, "db/migrations/*_"+name+".sql"),
		)
		if err != nil || len(matches) != 1 {
			t.Fatalf("expected one %s migration: %v %v", name, matches, err)
		}
	}

	// Scaffolds are authored starting points; replace them with real bodies.
	for fixture, target := range map[string]string{
		"rules.go.txt":        "internal/rules/orgs/rules.go",
		"provision.go.txt":    "internal/logic/orgs/provision.go",
		"count.go.txt":        "internal/logic/orgs/count.go",
		"tenant_store.go.txt": "internal/adapters/orgs/tenant_store.go",
	} {
		systemFileAs(t, root, "example.test/auditapp", fixture, target)
	}

	seamFile(t, root, "rules.go.txt", "internal/rules/audit/rules.go")
	seamFile(t, root, "list.go.txt", "internal/logic/audit/list.go")
	seamFile(t, root, "append.go.txt", "internal/logic/audit/append.go")
	seamFile(t, root, "verify.go.txt", "internal/logic/audit/verify.go")
	seamFile(
		t,
		root,
		"event_store.go.txt",
		"internal/adapters/audit/event_store.go",
	)
	seamFile(
		t,
		root,
		"approvals_request_store.go.txt",
		"internal/adapters/approvals/request_store.go",
	)
	seamFile(
		t,
		root,
		"approvals_approve.go.txt",
		"internal/logic/approvals/approve.go",
	)
	seamFile(t, root, "logic_test.go.txt", "internal/logic/audit/logic_test.go")

	external(t, root, "sqlc", "generate")
	external(t, root, "go", "mod", "tidy")
	mustInvoke(t, root, "fmt")
	external(t, root, "go", "run", "./cmd/openapi")
	external(t, root, "sqlc", "diff")

	if report := project.InspectArchitecture(root); !report.Passed {
		t.Fatalf("seam architecture: %+v", report.Diagnostics)
	}

	mustInvoke(t, root, "vet")
	lintCandidateManaged(t, root)

	// The scope harness mints executions through a real identity gate.
	external(t, root, "go", "test", "-race", "-count=1", "./internal/logic/...")

	source, err := os.ReadFile("testdata/candidate_audit_runtime.go.tmpl")
	if err != nil {
		t.Fatal(err)
	}

	writeSeam(t, root, "internal/app/audit_runtime_test.go", string(source))

	approvals, err := os.ReadFile(
		"testdata/candidate_approvals_runtime.go.tmpl",
	)
	if err != nil {
		t.Fatal(err)
	}

	writeSeam(
		t, root, "internal/app/approvals_runtime_test.go", string(approvals),
	)

	output := external(
		t, root, "go", "test", "-race", "-json", "-count=1",
		"-run", "^(TestAuditSeamEndToEnd|TestApprovalsTxEndToEnd)$",
		"./internal/app",
	)
	assertCandidateCaseNames(
		t, output, "TestAuditSeamEndToEnd", "TestApprovalsTxEndToEnd",
	)

	members, err := os.ReadFile("testdata/candidate_audit_members.go.tmpl")
	if err != nil {
		t.Fatal(err)
	}

	writeSeam(t, root, "internal/app/audit_members_test.go", string(members))

	output = external(
		t, root, "go", "test", "-race", "-json", "-count=1",
		"-run", "^TestAuditMembers", "./internal/app",
	)
	assertCandidateCaseNames(
		t, output,
		"TestAuditMembersActiveState",
		"TestAuditMembersRuntimeRole",
	)

	system, err := os.ReadFile("testdata/candidate_system_runtime.go.tmpl")
	if err != nil {
		t.Fatal(err)
	}

	writeSeam(
		t, root, "internal/app/system_runtime_test.go",
		strings.ReplaceAll(
			string(system), "example.test/sysapp", "example.test/auditapp",
		),
	)

	output = external(
		t, root, "go", "test", "-race", "-json", "-count=1",
		"-run", "^TestSystem", "./internal/app",
	)
	assertCandidateCaseNames(
		t, output,
		"TestSystemProvisionThenTenantRoutes",
		"TestSystemProvisionRateLimit",
		"TestSystemProvisionOnce",
		"TestSystemLookup",
		"TestSystemLookupRateLimit",
		"TestSystemRowLevelBoundaries",
	)

	assertSeamNegatives(t, root)
	assertSystemNegatives(
		t, root, "example.test/auditapp/", migrationPath(t, root, "rls_orgs"),
	)
}

// migrationPath finds a generated migration by its name, whose number depends
// on the declared table order.
func migrationPath(t *testing.T, root, name string) string {
	t.Helper()

	matches, err := filepath.Glob(
		filepath.Join(root, "db/migrations/*_"+name+".sql"),
	)
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected one %s migration: %v %v", name, matches, err)
	}

	return "db/migrations/" + filepath.Base(matches[0])
}

// seamNegative mutates one file, expects the canonical inspection to report
// the named check, then restores the file and expects a pass again.
type seamNegative struct {
	name, check, path string
	mutate            func(original string) string
}

func assertSeamNegatives(t *testing.T, root string) {
	t.Helper()

	module := "example.test/auditapp/"
	appendTo := func(extra string) func(string) string {
		return func(original string) string {
			return original + "\n" + extra
		}
	}

	cases := []seamNegative{
		{
			name:  "logic called from transport",
			check: "ARCH611",
			path:  "internal/transport/bypass.go",
			mutate: func(string) string {
				return "package transport\n\nimport (\n" +
					"\t\"context\"\n\n" +
					"\tlogic \"" + module + "internal/logic/audit\"\n" +
					"\tmodels \"" + module + "internal/models/audit\"\n" +
					"\t\"" + module + "internal/scope\"\n)\n\n" +
					"func bypass(ctx context.Context, e scope.Execution) error {\n" +
					"\treturn logic.Append(ctx, e, logic.AppendPorts{}, " +
					"models.AppendRequest{})\n}\n"
			},
		},
		{
			name:  "pool in adapter",
			check: "ARCH610",
			path:  "internal/adapters/audit/pool.go",
			mutate: func(string) string {
				return "package audit\n\nimport \"github.com/jackc/pgx/v5/pgxpool\"\n\n" +
					"type holder struct{ pool *pgxpool.Pool }\n"
			},
		},
		{
			name:  "dbtx passthrough constructor with raw set_config",
			check: "ARCH611",
			path:  "internal/adapters/audit/raw.go",
			mutate: func(string) string {
				return "package audit\n\nimport (\n\t\"context\"\n\n" +
					"\t\"" + module + "internal/scope\"\n" +
					"\tqueries \"" + module +
					"internal/storage/queries/audit\"\n)\n\n" +
					"func raw(ctx context.Context, tx scope.Tx) error {\n" +
					"\tdb := scope.Queries(tx, func(d queries.DBTX) " +
					"queries.DBTX { return d })\n" +
					"\t_, err := db.Exec(ctx, \"SELECT set_config(" +
					"'app.tenant_id', 'other', true)\")\n\n" +
					"\treturn err\n}\n"
			},
		},
		{
			name:  "adapter chooses its own scope.Queries constructor",
			check: "ARCH603",
			path:  "internal/adapters/audit/choose.go",
			mutate: func(string) string {
				return "package audit\n\nimport (\n" +
					"\t\"" + module + "internal/scope\"\n" +
					"\tqueries \"" + module +
					"internal/storage/queries/audit\"\n)\n\n" +
					"func choose(tx scope.Tx) *queries.Queries {\n" +
					"\treturn scope.Queries(tx, queries.New)\n}\n"
			},
		},
		{
			name:  "scope.Queries called outside the generated helper",
			check: "ARCH603",
			path:  "internal/logic/audit/queries.go",
			mutate: func(string) string {
				return "package audit\n\nimport \"" + module +
					"internal/scope\"\n\nvar _ = scope.Queries[int, int]\n"
			},
		},
		{
			name:  "sqlc WithTx handle",
			check: "ARCH611",
			path:  "internal/adapters/audit/withtx.go",
			mutate: func(string) string {
				return "package audit\n\nimport queries \"" + module +
					"internal/storage/queries/audit\"\n\n" +
					"func withTx() { queries.New(nil).WithTx(nil) }\n"
			},
		},
		{
			name:  "forged scope tenant",
			check: "ARCH604",
			path:  "internal/logic/audit/forge.go",
			mutate: func(string) string {
				return "package audit\n\nimport \"" + module +
					"internal/scope\"\n\nfunc forge() scope.Tenant {\n" +
					"\treturn scope.Tenant{}\n}\n"
			},
		},
		{
			name:  "forged scope execution",
			check: "ARCH604",
			path:  "internal/logic/audit/forge.go",
			mutate: func(string) string {
				return "package audit\n\nimport \"" + module +
					"internal/scope\"\n\nfunc forge() scope.Execution {\n" +
					"\treturn scope.Execution{}\n}\n"
			},
		},
		{
			name:  "scope bind outside the workflow",
			check: "ARCH603",
			path:  "internal/logic/audit/bind.go",
			mutate: func(string) string {
				return "package audit\n\nimport \"" + module +
					"internal/scope\"\n\nvar _ = scope.Bind\n"
			},
		},
		{
			name:  "database handle from scope in logic",
			check: "ARCH603",
			path:  "internal/logic/audit/db.go",
			mutate: func(string) string {
				return "package audit\n\nimport \"" + module +
					"internal/scope\"\n\nvar _ = scope.NewDB\n"
			},
		},
		{
			name:  "pgx in logic",
			check: "ARCH610",
			path:  "internal/logic/audit/pgx.go",
			mutate: func(string) string {
				return "package audit\n\nimport _ \"github.com/jackc/pgx/v5\"\n"
			},
		},
		{
			name:  "io in rules",
			check: "ARCH610",
			path:  "internal/rules/audit/io.go",
			mutate: func(string) string {
				return "package audit\n\nimport _ \"os\"\n"
			},
		},
		{
			name:  "scopetest in production",
			check: "ARCH610",
			path:  "internal/logic/audit/harness.go",
			mutate: func(string) string {
				return "package audit\n\nimport _ \"" + module +
					"internal/scope/scopetest\"\n"
			},
		},
		{
			name:  "package variable in logic",
			check: "ARCH611",
			path:  "internal/logic/audit/state.go",
			mutate: func(string) string {
				return "package audit\n\nvar shared int\n"
			},
		},
		{
			name:   "goroutine in adapter",
			check:  "ARCH611",
			path:   "internal/adapters/audit/event_store.go",
			mutate: appendTo("func spawn() { go func() {}() }\n"),
		},
		{
			name:  "init in rules",
			check: "ARCH611",
			path:  "internal/rules/audit/boot.go",
			mutate: func(string) string {
				return "package audit\n\nfunc init() {}\n"
			},
		},
		{
			name:  "alternate hand handler outside declared roles",
			check: "ARCH602",
			path:  "internal/audit/service.go",
			mutate: func(string) string {
				return "package audit\n\nfunc Service() {}\n"
			},
		},
		{
			name:  "adapter reaches another group's port",
			check: "ARCH610",
			path:  "internal/adapters/approvals/audit_port.go",
			mutate: func(string) string {
				return "package approvals\n\nimport _ \"" + module +
					"internal/ports/audit\"\n"
			},
		},
		{
			name:  "logic of a group without a tx port reads another's models",
			check: "ARCH610",
			path:  "internal/logic/audit/approvals_models.go",
			mutate: func(string) string {
				return "package audit\n\nimport _ \"" + module +
					"internal/models/approvals\"\n"
			},
		},
		{
			name:  "logic reaches another group's logic",
			check: "ARCH610",
			path:  "internal/logic/approvals/audit_logic.go",
			mutate: func(string) string {
				return "package approvals\n\nimport _ \"" + module +
					"internal/logic/audit\"\n"
			},
		},
		{
			name:  "query without tenant argument",
			check: "ARCH612",
			path:  "db/queries/audit/events.sql",
			mutate: func(original string) string {
				return strings.Replace(
					original,
					"WHERE org_id = @org_id AND seq > @after",
					"WHERE seq > @after",
					1,
				)
			},
		},
		{
			name:  "query over undeclared table",
			check: "ARCH612",
			path:  "db/queries/audit/events.sql",
			mutate: appendTo(
				"-- name: Leak :many\nSELECT id FROM users LIMIT 1;\n",
			),
		},
		{
			name:  "missing row-level security migration",
			check: "ARCH612",
			path:  migrationPath(t, root, "rls_audit_events"),
			mutate: func(string) string {
				return "-- removed\n"
			},
		},
	}

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
