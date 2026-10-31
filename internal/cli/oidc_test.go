package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

func TestOIDCProfileRequiresAccountBackend(t *testing.T) {
	root := temporary(t)

	output, err := invoke(
		t,
		root,
		"init",
		".",
		"--name",
		"oidc-test",
		"--oidc",
		"--no-mise",
		"--no-fnox",
		"--no-docker",
	)
	if err == nil {
		t.Fatalf("accepted OIDC without account backend: %s", output)
	}

	if _, err := os.Stat(
		filepath.Join(root, "pluto.toml"),
	); !os.IsNotExist(
		err,
	) {
		t.Fatal("wrote rejected profile")
	}
}

func TestGeneratedOIDCProfile(t *testing.T) {
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
				"oidc-test",
				"--module",
				"example.com/oidc-test",
				"--oidc",
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
			if err != nil || !manifest.Capabilities.OIDC {
				t.Fatal("OIDC choice not saved", err)
			}

			for _, file := range []string{"cmd/api/oidc.go", "internal/config/oidc.go", "internal/paths/oidc.go", "db/migrations/007_external_accounts.sql"} {
				if _, err := os.Stat(filepath.Join(root, file)); err != nil {
					t.Fatal(err)
				}
			}

			data, err := os.ReadFile(
				filepath.Join(root, "internal/config/oidc.go"),
			)
			if err != nil {
				t.Fatal(err)
			}

			external(t, root, "go", "test", "-race", "./...")
			external(t, root, "go", "build", "./cmd/api")
			external(t, root, "go", "mod", "verify")

			if !strings.Contains(string(data), "OIDC_ENCRYPTION_KEY") {
				t.Fatal("missing encryption key boundary")
			}
		})
	}
}
