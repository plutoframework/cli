package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

func frontendApp(t *testing.T, local bool) string {
	t.Helper()

	root := temporary(t)

	args := []string{
		"init",
		".",
		"--name",
		"orbital",
		"--no-mise",
		"--no-fnox",
		"--no-docker",
	}
	if local {
		args = append(args, "--local-credentials")
	}

	mustInvoke(t, root, args...)

	return root
}

func TestFrontendPackagePreflight(t *testing.T) {
	for _, kind := range []string{"null-package", "null-dependencies", "dev-conflict"} {
		t.Run(kind, func(t *testing.T) {
			root := frontendApp(t, true)
			mustInvoke(
				t,
				root,
				"frontend",
				"init",
				"--ui",
				"shadcn",
				"--no-install",
			)

			path := filepath.Join(root, "web/package.json")
			data, _ := os.ReadFile(path)

			var pkg map[string]any
			if err := json.Unmarshal(data, &pkg); err != nil {
				t.Fatal(err)
			}

			switch kind {
			case "null-package":
				pkg = nil
			case "null-dependencies":
				pkg["dependencies"] = nil
			case "dev-conflict":
				pkg["devDependencies"].(map[string]any)["class-variance-authority"] = "0.1.0"
			}

			data, err := json.MarshalIndent(pkg, "", "  ")
			if err != nil {
				t.Fatal(err)
			}

			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}

			before := snapshot(t, root)
			if _, err := invoke(
				t,
				root,
				"frontend",
				"add",
				"component",
				"launch",
				"--kind",
				"button",
				"--no-install",
			); err == nil {
				t.Fatal("invalid package accepted")
			}

			assertUnchanged(t, before, snapshot(t, root))
		})
	}
}

func TestFrontendProfiles(t *testing.T) {
	for _, profile := range []string{"native", "heroui-v3", "shadcn"} {
		t.Run(profile, func(t *testing.T) {
			root := frontendApp(t, true)
			before := snapshot(t, root)

			preview := mustInvoke(
				t,
				root,
				"frontend",
				"init",
				"--ui",
				profile,
				"--auth",
				"--dry-run",
			)
			if !strings.Contains(
				preview,
				"web/src/features/session/forms/AuthForm.tsx",
			) {
				t.Fatal(preview)
			}

			assertUnchanged(t, before, snapshot(t, root))
			mustInvoke(
				t,
				root,
				"frontend",
				"init",
				"--ui",
				profile,
				"--no-install",
			)
			mustInvoke(t, root, "frontend", "add", "auth", "--no-install")

			form := filepath.Join(
				root,
				"web/src/features/session/forms/AuthForm.tsx",
			)

			content, err := os.ReadFile(form)
			if err != nil {
				t.Fatal(err)
			}

			switch profile {
			case "heroui-v3":
				if !strings.Contains(string(content), "@heroui/react") {
					t.Fatal("HeroUI imports missing")
				}
			case "shadcn":
				if !strings.Contains(string(content), "@/shared/ui/button") {
					t.Fatal("local shadcn imports missing")
				}
			case "native":
				if strings.Contains(string(content), "@heroui") ||
					strings.Contains(string(content), "@/shared/ui/button") {
					t.Fatal("unexpected component dependency")
				}
			}

			content = append(
				content,
				[]byte("\n// An application-owned customization.\n")...)
			if err := os.WriteFile(form, content, 0o644); err != nil {
				t.Fatal(err)
			}

			for _, kind := range []string{"button", "input", "card", "dialog", "form"} {
				mustInvoke(
					t,
					root,
					"frontend",
					"add",
					"component",
					"review-"+kind,
					"--kind",
					kind,
					"--no-install",
				)
			}

			actual, _ := os.ReadFile(form)
			if string(actual) != string(content) {
				t.Fatal("overwrote authored auth form")
			}

			_, m, err := project.Discover(root)
			if err != nil || m.Frontend.UI != profile || !m.Frontend.Auth ||
				len(m.Frontend.Components) != 5 {
				t.Fatalf("manifest: %+v %v", m.Frontend, err)
			}

			before = snapshot(t, root)
			if _, err := invoke(
				t,
				root,
				"frontend",
				"add",
				"auth",
				"--no-install",
			); err == nil {
				t.Fatal("auth rerun overwrote files")
			}

			if _, err := invoke(
				t,
				root,
				"frontend",
				"add",
				"component",
				"review-form",
				"--no-install",
			); err == nil {
				t.Fatal("duplicate component accepted")
			}

			assertUnchanged(t, before, snapshot(t, root))
		})
	}
}

func TestFrontendPreflight(t *testing.T) {
	for name, args := range map[string][]string{
		"traversal":       {"frontend", "init", "--directory", "../escape", "--no-install"},
		"reserved":        {"frontend", "init", "--directory", "internal", "--no-install"},
		"unknown-ui":      {"frontend", "init", "--ui", "unknown", "--no-install"},
		"auth-capability": {"frontend", "init", "--auth", "--no-install"},
	} {
		t.Run(name, func(t *testing.T) {
			root := frontendApp(t, false)

			before := snapshot(t, root)
			if _, err := invoke(t, root, args...); err == nil {
				t.Fatal("invalid frontend accepted")
			}

			assertUnchanged(t, before, snapshot(t, root))
		})
	}

	t.Run("late-file-conflict", func(t *testing.T) {
		root := frontendApp(t, true)

		path := filepath.Join(root, "web/src/main.tsx")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(
			path,
			[]byte("existing application"),
			0o644,
		); err != nil {
			t.Fatal(err)
		}

		before := snapshot(t, root)
		if _, err := invoke(
			t,
			root,
			"frontend",
			"init",
			"--auth",
			"--no-install",
		); err == nil {
			t.Fatal("existing file overwritten")
		}

		assertUnchanged(t, before, snapshot(t, root))
	})
	t.Run("symlink", func(t *testing.T) {
		root := frontendApp(t, true)

		outside := temporary(t)
		if err := os.Symlink(outside, filepath.Join(root, "web")); err != nil {
			t.Fatal(err)
		}

		if _, err := invoke(
			t,
			root,
			"frontend",
			"init",
			"--no-install",
		); err == nil {
			t.Fatal("followed symlink")
		}

		entries, _ := os.ReadDir(outside)
		if len(entries) != 0 {
			t.Fatal("wrote outside project")
		}
	})
}

func TestFrontendProfileMigrationAndDependencyConflict(t *testing.T) {
	root := frontendApp(t, true)
	mustInvoke(t, root, "frontend", "init", "--ui", "shadcn", "--no-install")

	path := filepath.Join(root, "web/package.json")
	data, _ := os.ReadFile(path)

	data = []byte(
		strings.Replace(
			string(data),
			`"react": "19.3.1"`,
			`"class-variance-authority": "0.1.0", "react": "19.3.1"`,
			1,
		),
	)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	before := snapshot(t, root)
	if _, err := invoke(
		t,
		root,
		"frontend",
		"add",
		"component",
		"launch",
		"--kind",
		"button",
		"--no-install",
	); err == nil ||
		!strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("dependency conflict: %v", err)
	}

	assertUnchanged(t, before, snapshot(t, root))

	path = filepath.Join(root, "pluto.toml")

	data, _ = os.ReadFile(path)
	if err := os.WriteFile(
		path,
		[]byte(
			strings.Replace(string(data), `ui = 'shadcn'`, `ui = 'native'`, 1),
		),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	before = snapshot(t, root)
	if _, err := invoke(
		t,
		root,
		"frontend",
		"add",
		"component",
		"launch",
		"--no-install",
	); err == nil || !strings.Contains(err.Error(), "profile differs") {
		t.Fatal("silent profile migration")
	}

	assertUnchanged(t, before, snapshot(t, root))
}
