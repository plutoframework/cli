package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

func TestGeneratedJobs(t *testing.T) {
	for _, multi := range []bool{false, true} {
		name := "single"
		if multi {
			name = "multi"
		}

		t.Run(name, func(t *testing.T) {
			root := temporary(t)

			args := []string{
				"init",
				".",
				"--name",
				"jobs-test",
				"--module",
				"example.com/jobs-test",
				"--no-mise",
				"--no-fnox",
			}
			if multi {
				args = append(args, "--multi-tenant")
			}

			mustInvoke(t, root, args...)

			before := snapshot(t, root)
			mustInvoke(
				t,
				root,
				"add",
				"job",
				"invoices",
				"Send",
				"--kind",
				"invoice.send",
				"--dry-run",
			)
			assertUnchanged(t, before, snapshot(t, root))
			mustInvoke(
				t,
				root,
				"add",
				"job",
				"invoices",
				"Send",
				"--kind",
				"invoice.send",
			)

			path := filepath.Join(root, "internal/jobs/invoices/send.go")

			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			source = append(
				source,
				[]byte(
					"\n// Authored marker must survive subsequent jobs/endpoints.\n",
				)...)
			if err := os.WriteFile(path, source, 0o644); err != nil {
				t.Fatal(err)
			}

			bindingPath := filepath.Join(
				root,
				"internal/background/invoices_send.go",
			)

			binding, err := os.ReadFile(bindingPath)
			if err != nil {
				t.Fatal(err)
			}

			binding = append(
				binding,
				[]byte("\n// Authored dependency binding marker.\n")...)
			if err := os.WriteFile(bindingPath, binding, 0o644); err != nil {
				t.Fatal(err)
			}

			mustInvoke(
				t,
				root,
				"add",
				"job",
				"exports",
				"Build",
				"--kind",
				"export.build",
			)
			mustInvoke(
				t,
				root,
				"add",
				"endpoint",
				"records",
				"List",
				"--method",
				"GET",
				"--path",
				"/api/records",
				"--name",
				"Records",
				"--description",
				"Read records",
			)

			preserved, err := os.ReadFile(path)
			if err != nil || string(preserved) != string(source) {
				t.Fatal("authored handler overwritten", err)
			}

			bindingAfter, err := os.ReadFile(bindingPath)
			if err != nil || string(bindingAfter) != string(binding) {
				t.Fatal("authored constructor overwritten", err)
			}

			_, manifest, err := project.Discover(root)
			if err != nil || len(manifest.Jobs) != 2 {
				t.Fatal("job metadata not persisted", err)
			}

			if err := project.Contracts(root, manifest); err != nil {
				t.Fatal(err)
			}

			before = snapshot(t, root)
			for _, args := range [][]string{
				{"add", "job", "invoices", "Again", "--kind", "invoice.send"},
				{"add", "job", "invoices", "Send", "--kind", "another.kind"},
				{"add", "job", "../escape", "Bad", "--kind", "bad"},
				{"add", "job", "for", "Bad", "--kind", "bad"},
				{"add", "job", "invoices", "Bad", "--kind", "bad\nkind"},
			} {
				if _, err := invoke(t, root, args...); err == nil {
					t.Fatal("unsafe job accepted", args)
				}

				assertUnchanged(t, before, snapshot(t, root))
			}

			fixture, err := os.ReadFile("testdata/jobs.go.tmpl")
			if err != nil {
				t.Fatal(err)
			}

			if err := os.WriteFile(
				filepath.Join(root, "internal/background/integration_test.go"),
				fixture,
				0o644,
			); err != nil {
				t.Fatal(err)
			}

			external(t, root, "go", "test", "-race", "-count=1", "./...")
			external(
				t,
				root,
				"go",
				"build",
				"-o",
				filepath.Join(t.TempDir(), "worker"),
				"./cmd/worker",
			)
			external(t, root, "go", "mod", "verify")

			if _, err := exec.LookPath("golangci-lint"); err == nil {
				mustInvoke(t, root, "fmt")
				mustInvoke(t, root, "vet")
			}

			// Managed handler registration must be checked before the next write.
			registry := filepath.Join(
				root,
				"internal/background/handlers_gen.go",
			)
			replaceSource(
				t,
				registry,
				"newInvoicesSend(dependencies)",
				"newExportsBuild(dependencies)",
			)

			before = snapshot(t, root)
			if _, err := invoke(
				t,
				root,
				"add",
				"job",
				"reports",
				"Run",
				"--kind",
				"report.run",
			); err == nil {
				t.Fatal("modified registry accepted")
			}

			assertUnchanged(t, before, snapshot(t, root))

			// Verify Docker publishes both binaries without auto-running an empty worker.
			compose, err := os.ReadFile(filepath.Join(root, "compose.yaml"))
			if err != nil ||
				!strings.Contains(string(compose), "profiles: [\"worker\"]") {
				t.Fatal("worker profile missing", err)
			}
		})
	}
}
