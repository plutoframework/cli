package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"text/template"

	"github.com/plutoframework/cli/internal/project"
)

func TestGeneratedLocalApplications(t *testing.T) {
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
				"local-test",
				"--module",
				"example.com/local-test",
				"--env-prefix",
				"LOCAL_",
				"--local-credentials",
				"--no-mise",
				"--no-fnox",
				"--no-docker",
			}
			if multi {
				args = append(args, "--multi-tenant")
			}

			mustInvoke(t, root, args...)

			_, manifest, err := project.Discover(root)
			if err != nil || !manifest.Capabilities.LocalCredentials {
				t.Fatal("credential choice not persisted", err)
			}

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
				"List records",
				"--description",
				"List only after trusted membership.",
			)

			source, err := os.ReadFile("testdata/local_lifecycle.go.tmpl")
			if err != nil {
				t.Fatal(err)
			}

			tmpl, err := template.New("test").Parse(string(source))
			if err != nil {
				t.Fatal(err)
			}

			var output bytes.Buffer
			if err := tmpl.Execute(
				&output,
				struct{ Multi bool }{multi},
			); err != nil {
				t.Fatal(err)
			}

			if err := os.WriteFile(
				filepath.Join(root, "internal/app/local_lifecycle_test.go"),
				output.Bytes(),
				0o644,
			); err != nil {
				t.Fatal(err)
			}

			external(t, root, "go", "test", "-race", "-count=1", "./...")
			external(t, root, "go", "build", "./cmd/api")
			external(t, root, "go", "mod", "verify")
			t.Run("style-and-vet", func(t *testing.T) {
				if _, err := exec.LookPath("golangci-lint"); err != nil {
					t.Skip("put pinned golangci-lint on PATH")
				}

				mustInvoke(t, root, "fmt")
				mustInvoke(t, root, "vet")
			})

			before := snapshot(t, root)
			if _, err := invoke(
				t,
				root,
				"add",
				"endpoint",
				"auth",
				"Signin",
				"--method",
				"POST",
				"--path",
				"/auth/signin",
				"--name",
				"Unsafe duplicate",
				"--description",
				"Must not replace the lifecycle.",
			); err == nil {
				t.Fatal("auth namespace not reserved")
			}

			assertUnchanged(t, before, snapshot(t, root))
		})
	}
}
