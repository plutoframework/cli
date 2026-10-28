package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/plutoframework/cli/internal/project"
)

func TestGeneratedCaches(t *testing.T) {
	for _, mode := range []string{"none", "memory", "valkey"} {
		t.Run(mode, func(t *testing.T) {
			root := temporary(t)

			args := []string{
				"init",
				".",
				"--name",
				"cache-test",
				"--module",
				"example.com/cache-test",
				"--env-prefix",
				"CACHE_TEST_",
				"--cache",
				mode,
				"--no-mise",
				"--no-fnox",
			}
			if mode == "memory" {
				args = append(args, "--multi-tenant")
			}

			if mode == "valkey" {
				args = append(args, "--local-credentials")
			}

			mustInvoke(t, root, args...)

			_, manifest, err := project.Discover(root)
			if err != nil || manifest.Capabilities.Cache != mode {
				t.Fatal("cache choice not persisted", err)
			}

			source, err := os.ReadFile("testdata/cache.go.tmpl")
			if err != nil {
				t.Fatal(err)
			}

			tmpl, err := template.New("cache").Parse(string(source))
			if err != nil {
				t.Fatal(err)
			}

			var output bytes.Buffer
			if err := tmpl.Execute(
				&output,
				struct{ Mode string }{mode},
			); err != nil {
				t.Fatal(err)
			}

			if err := os.WriteFile(
				filepath.Join(root, "internal/caching/cache_test.go"),
				output.Bytes(),
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
				filepath.Join(t.TempDir(), "api"),
				"./cmd/api",
			)
			external(t, root, "go", "mod", "verify")

			module, err := os.ReadFile(filepath.Join(root, "go.mod"))
			if err != nil {
				t.Fatal(err)
			}

			if strings.Contains(
				string(module),
				"valkey-go",
			) != (mode == "valkey") {
				t.Fatal("optional dependency leaked")
			}

			compose, err := os.ReadFile(filepath.Join(root, "compose.yaml"))
			if err != nil {
				t.Fatal(err)
			}

			if strings.Contains(
				string(compose),
				"valkey/valkey:",
			) != (mode == "valkey") {
				t.Fatal("cache service leaked/missing")
			}

			if _, err := exec.LookPath("docker"); err == nil {
				external(t, root, "docker", "compose", "config", "--quiet")
			}

			if _, err := exec.LookPath("golangci-lint"); err == nil {
				mustInvoke(t, root, "fmt")
				mustInvoke(t, root, "vet")
			}
		})
	}
}

func TestInitRejectsUnknownCache(t *testing.T) {
	root := temporary(t)
	before := snapshot(t, root)

	_, err := invoke(
		t,
		root,
		"init",
		".",
		"--name",
		"cache-test",
		"--cache",
		"redis",
	)
	if err == nil || !strings.Contains(err.Error(), "cache must be") {
		t.Fatal("invalid backend not rejected by validation", err)
	}

	assertUnchanged(t, before, snapshot(t, root))
}
