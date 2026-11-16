package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

// deviceApp initializes the audit application with the seam blueprint plus a
// declared [auth.device] and every authored file it needs, then runs the
// generators, sqlc and the canonical checks.
func deviceApp(t *testing.T) string {
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
		t.Fatal(
			"the device end-to-end test requires PLUTO_CLI_TEST_DATABASE_URL",
		)
	}

	if _, err := exec.LookPath("sqlc"); err != nil {
		t.Fatal("sqlc is required:", err)
	}

	root := temporary(t)

	args := []string{
		"--no-input", "init", ".",
		"--name", "auditapp",
		"--module", "example.test/auditapp",
		"--env-prefix", "AUDITAPP_",
		"--multi-tenant", "--local-credentials",
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

	seamFile(t, root, "010_users.sql", "db/migrations/010_users.sql")
	seamFile(
		t, root, "011_audit_events.sql", "db/migrations/011_audit_events.sql",
	)
	seamFile(t, root, "events.sql", "db/queries/audit/events.sql")
	seamFile(t, root, "approvals.sql", "db/queries/approvals/requests.sql")
	systemFileAs(
		t,
		root,
		"example.test/auditapp",
		"orgs.sql",
		"db/queries/orgs/orgs.sql",
	)

	blueprint, err := os.ReadFile(
		filepath.Join("testdata", "seams", "blueprint.toml"),
	)
	if err != nil {
		t.Fatal(err)
	}

	device, err := os.ReadFile(
		filepath.Join("testdata", "seams", "device.toml"),
	)
	if err != nil {
		t.Fatal(err)
	}

	writeSeam(t, root, "audit-blueprint.toml", string(blueprint)+string(device))

	mustInvoke(t, root, "--no-input", "add", "blueprint", "--file",
		filepath.Join(root, "audit-blueprint.toml"))

	for fixture, target := range map[string]string{
		"rules.go.txt": "internal/rules/orgs/rules.go",
		"count.go.txt": "internal/logic/orgs/count.go",
	} {
		systemFileAs(t, root, "example.test/auditapp", fixture, target)
	}

	seamFile(t, root, "provision.go.txt", "internal/logic/orgs/provision.go")
	seamFile(
		t,
		root,
		"tenant_store.go.txt",
		"internal/adapters/orgs/tenant_store.go",
	)
	seamFile(t, root, "watch.go.txt", "internal/logic/audit/watch.go")
	seamFile(
		t, root, "record_auth_event.go.txt",
		"internal/logic/audit/recordauthevent.go",
	)

	seamFile(t, root, "rules.go.txt", "internal/rules/audit/rules.go")
	seamFile(t, root, "list.go.txt", "internal/logic/audit/list.go")
	seamFile(t, root, "append_device.go.txt", "internal/logic/audit/append.go")
	seamFile(t, root, "verify.go.txt", "internal/logic/audit/verify.go")
	seamFile(
		t, root, "event_store.go.txt", "internal/adapters/audit/event_store.go",
	)
	seamFile(
		t, root, "approvals_request_store.go.txt",
		"internal/adapters/approvals/request_store.go",
	)
	seamFile(
		t, root, "approvals_approve.go.txt",
		"internal/logic/approvals/approve.go",
	)

	external(t, root, "sqlc", "generate")
	external(t, root, "go", "mod", "tidy")
	mustInvoke(t, root, "fmt")
	external(t, root, "go", "run", "./cmd/openapi")
	external(t, root, "sqlc", "diff")

	if report := project.InspectArchitecture(root); !report.Passed {
		t.Fatalf("device architecture: %+v", report.Diagnostics)
	}

	mustInvoke(t, root, "vet")
	lintCandidateManaged(t, root)

	return root
}

// A fresh candidate application declares [auth.device]; the generators, the
// canonical checks and a real Postgres prove the full RFC 8628 flow, scope
// narrowing, refresh rotation with reuse detection, revocation and polling
// limits through the real HTTP handler, and negative fixtures prove authored
// code cannot forge delegation metadata or bypass the scope check.
func TestCandidateDeviceEndToEnd(t *testing.T) {
	root := deviceApp(t)

	// The managed output names the declared scopes and nothing else.
	delegation, err := os.ReadFile(
		filepath.Join(root, "internal/access/delegation_gen.go"))
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		`"audit.read": {`, `"audit.write": {`, `"members.count": {`,
	} {
		if !strings.Contains(string(delegation), want) {
			t.Fatalf("delegation_gen.go lacks %s:\n%s", want, delegation)
		}
	}

	for _, path := range []string{
		"internal/app/candidate_device_gen.go",
		"internal/paths/device.go",
	} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Fatalf("device generation did not create %s: %v", path, err)
		}
	}

	matches, err := filepath.Glob(
		filepath.Join(root, "db/migrations/*_device_grants.sql"),
	)
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected one device migration: %v %v", matches, err)
	}

	openapi, err := os.ReadFile(filepath.Join(root, "api/openapi.json"))
	if err == nil && !strings.Contains(string(openapi), "/auth/device/token") {
		t.Fatal("OpenAPI does not describe the device endpoints")
	}

	audit, err := os.ReadFile(
		filepath.Join("testdata", "candidate_audit_runtime.go.tmpl"))
	if err != nil {
		t.Fatal(err)
	}

	writeSeam(t, root, "internal/app/audit_runtime_test.go", string(audit))

	runtime, err := os.ReadFile(
		filepath.Join("testdata", "candidate_device_runtime.go.tmpl"))
	if err != nil {
		t.Fatal(err)
	}

	writeSeam(t, root, "internal/app/device_runtime_test.go", string(runtime))

	stream, err := os.ReadFile(
		filepath.Join("testdata", "candidate_audit_stream.go.tmpl"))
	if err != nil {
		t.Fatal(err)
	}

	writeSeam(t, root, "internal/app/audit_stream_test.go", string(stream))

	output := external(
		t, root, "go", "test", "-race", "-json", "-count=1",
		"-run", "^TestDevice(EndToEnd|Stream)$", "./internal/app",
	)
	assertCandidateCaseNames(
		t, output, "TestDeviceEndToEnd", "TestDeviceStream")

	assertDeviceNegatives(t, root)
}

func assertDeviceNegatives(t *testing.T, root string) {
	t.Helper()

	module := "example.test/auditapp/"
	gate := "internal/operations/audit/append_gate_gen.go"

	type negative struct {
		name, check, path string
		mutate            func(string) string
	}

	cases := []negative{
		{
			name:  "authored code imports the device grant",
			check: "ARCH610",
			path:  "internal/logic/audit/device.go",
			mutate: func(string) string {
				return "package audit\n\nimport _ " +
					"\"github.com/plutoframework/pluto-auth/device\"\n"
			},
		},
		{
			name:  "authored code binds an actor",
			check: "ARCH603",
			path:  "internal/logic/audit/actor.go",
			mutate: func(string) string {
				return "package audit\n\nimport \"" + module +
					"internal/scope\"\n\nvar _ = scope.BindActor\n"
			},
		},
		{
			name:  "authored code records its own actor",
			check: "ARCH603",
			path:  "internal/logic/audit/context.go",
			mutate: func(string) string {
				return "package audit\n\nimport \"" + module +
					"internal/scope\"\n\nvar _ = scope.ContextWithActor\n"
			},
		},
		{
			name:  "authored code forges an actor",
			check: "ARCH604",
			path:  "internal/logic/audit/forge.go",
			mutate: func(string) string {
				return "package audit\n\nimport \"" + module +
					"internal/scope\"\n\nfunc forge() scope.Actor {\n" +
					"\treturn scope.Actor{}\n}\n"
			},
		},
		{
			name:  "scope check removed from a generated gate",
			check: "ARCH202",
			path:  gate,
			mutate: func(original string) string {
				return strings.Replace(
					original, "caller.Permits(", "caller.Identity() == nil && "+
						"caller.Permits(", 1)
			},
		},
		{
			name:  "delegation scopes widened in managed output",
			check: "ARCH202",
			path:  "internal/access/delegation_gen.go",
			mutate: func(original string) string {
				return strings.Replace(
					original, `"audit.read": {`,
					`"audit.read": {
		"audit.write",`, 1)
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
