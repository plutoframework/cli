package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func boundaryProfileFixture(t *testing.T) BoundaryProfile {
	t.Helper()

	root := t.TempDir()

	profile := BoundaryProfile{
		Mode:       "candidate",
		AuthSource: filepath.Join(root, "pluto-auth"),
		RBACSource: filepath.Join(root, "pluto-rbac"),
		CoreSource: filepath.Join(root, "pluto-core"),
		JobsSource: filepath.Join(root, "pluto-jobs"),
	}
	for _, relative := range []string{
		"pluto-auth",
		"pluto-auth/stores/postgres",
		"pluto-auth/methods/password",

		"pluto-rbac",
		"pluto-rbac/identity",
		"pluto-core",
		"pluto-core/stores/postgres",

		"pluto-jobs",
		"pluto-jobs/stores/postgres",
	} {
		directory := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(
			filepath.Join(directory, "go.mod"),
			[]byte(
				"module github.com/plutoframework/"+relative+"\n\ngo 1.25.5\n",
			),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
	}

	return profile
}

func TestBoundaryProfilePublishedHasNoReplacements(t *testing.T) {
	for _, mode := range []string{"", "published"} {
		profile := BoundaryProfile{Mode: mode}
		if err := ValidateBoundaryProfile(profile, "single"); err != nil {
			t.Fatal(err)
		}

		paths, err := CandidateReplacements(profile)
		if err != nil || len(paths) != 0 {
			t.Fatalf("published replacements=%v err=%v", paths, err)
		}

		profile.AuthSource = t.TempDir()
		if err := ValidateBoundaryProfile(
			profile,
			"multi",
		); err == nil ||
			!strings.Contains(err.Error(), "forbids") {
			t.Fatalf("implicit candidate adoption accepted: %v", err)
		}
	}
}

func TestBoundaryProfileExplicitCandidateModules(t *testing.T) {
	profile := boundaryProfileFixture(t)
	if err := ValidateBoundaryProfile(profile, "multi"); err != nil {
		t.Fatal(err)
	}

	paths, err := CandidateReplacements(profile)
	if err != nil || len(paths) != 9 {
		t.Fatalf("replacements=%v error=%v", paths, err)
	}

	for module, path := range paths {
		if !filepath.IsAbs(path) ||
			!strings.HasPrefix(module, "github.com/plutoframework/") {
			t.Fatalf("invalid replacement %s=%s", module, path)
		}

		data, err := os.ReadFile(filepath.Join(path, "go.mod"))
		if err != nil {
			t.Fatal(err)
		}

		actual, err := boundaryModulePath(data)
		if err != nil || actual != module {
			t.Fatalf("incorrect replacement %s=%s: %v", module, actual, err)
		}
	}

	if err := ValidateBoundaryProfile(
		profile,
		"single",
	); err == nil ||
		!strings.Contains(err.Error(), "multi") {
		t.Fatalf("unsupported single scope accepted: %v", err)
	}
}

func TestBoundaryProfileRejectsUntrustedSourceLayout(t *testing.T) {
	for _, name := range []string{
		"mode",
		"missing",
		"relative",
		"file",
		"wrong root",
		"wrong nested",
		"missing nested",
		"nested escape",
	} {
		t.Run(name, func(t *testing.T) {
			profile := boundaryProfileFixture(t)
			write := func(path, body string) {
				t.Helper()

				if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			switch name {
			case "mode":
				profile.Mode = "automatic"
			case "missing":
				profile.CoreSource = ""
			case "relative":
				profile.CoreSource = "../pluto-core"
			case "file":
				profile.CoreSource = filepath.Join(profile.CoreSource, "go.mod")
			case "wrong root":
				write(
					filepath.Join(profile.AuthSource, "go.mod"),
					"module counterfeit.invalid/auth\n",
				)
			case "wrong nested":
				write(
					filepath.Join(profile.RBACSource, "identity/go.mod"),
					"module github.com/plutoframework/pluto-rbac\n",
				)
			case "missing nested":
				if err := os.Remove(
					filepath.Join(profile.JobsSource, "stores/postgres/go.mod"),
				); err != nil {
					t.Fatal(err)
				}
			case "nested escape":
				path := filepath.Join(profile.RBACSource, "identity")
				if err := os.RemoveAll(path); err != nil {
					t.Fatal(err)
				}

				external := t.TempDir()
				write(
					filepath.Join(external, "go.mod"),
					"module github.com/plutoframework/pluto-rbac/identity\n",
				)

				if err := os.Symlink(external, path); err != nil {
					t.Fatal(err)
				}
			}

			if err := ValidateBoundaryProfile(profile, "multi"); err == nil {
				t.Fatal("invalid candidate source accepted")
			}
		})
	}
}

func TestBoundaryModuleIdentityCommentsAndQuoting(t *testing.T) {
	const expected = "github.com/plutoframework/pluto-core"
	for _, data := range []string{
		"module " + expected + "\ngo 1.25.5\n",
		"// provenance\nmodule \"" + expected + "\" // note\n",
		"/* module counterfeit.invalid/x */\nmodule " + expected + "\n",
	} {
		actual, err := boundaryModulePath([]byte(data))
		if err != nil || actual != expected {
			t.Fatalf("module %q=%q: %v", data, actual, err)
		}
	}

	for _, data := range []string{
		"// module " + expected + "\n",
		"/* module " + expected + " */\n",

		"module\n" + expected + "\n",
		"module " + expected + "\nmodule " + expected + "\n",

		"module github.com/plutoframework/ pluto-core\n",

		"module github.com/plutoframework/ /* gap */ pluto-core\n",

		"module \"" + expected + "\" trailing\n",
		"module \"unterminated\n",
	} {
		if actual, err := boundaryModulePath([]byte(data)); err == nil {
			t.Fatalf("malformed module accepted %q=%q", data, actual)
		}
	}
}
