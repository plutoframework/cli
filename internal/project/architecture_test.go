package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchitectureInspection(t *testing.T) {
	for _, tc := range []struct {
		name, source, dependency string
		bad                      bool
	}{
		{"data entry", "internal/storage/store.go", "internal/background", true},
		{"helper bypass", "internal/storage/store.go", "internal/helper", true},
		{"operation transport", "internal/operations/run.go", "net/http", true},
		{"domain effect", "internal/domain/rule.go", "os", true},
		{"data contract", "internal/storage/store.go", "internal/models", false},
		{"composition", "cmd/server/main.go", "internal/background", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			write := func(path, data string) {
				t.Helper()

				if err := os.MkdirAll(
					filepath.Dir(filepath.Join(root, path)),
					0o755,
				); err != nil {
					t.Fatal(err)
				}

				if err := os.WriteFile(
					filepath.Join(root, path),
					[]byte(data),
					0o644,
				); err != nil {
					t.Fatal(err)
				}
			}
			write("go.mod", "module example.test/app\n")
			write(
				"pluto.toml",
				fmt.Sprintf("schema_version = %d\n", SchemaVersion),
			)
			write("scripts/check.sh", "pluto vet\n")

			dep := tc.dependency
			if strings.HasPrefix(dep, "internal/") {
				dep = "example.test/app/" + dep
			}

			write(tc.source, "package fixture\nimport _ \""+dep+"\"\n")
			write(
				"internal/background/job.go",
				"package background\nfunc Run() {}\n",
			)
			write(
				"internal/helper/helper.go",
				"package helper\nimport _ \"example.test/app/internal/background\"\n",
			)
			write(
				"internal/models/wire.go",
				"package models\ntype Request struct { Name string }\n",
			)

			report := InspectArchitecture(root)
			found := false

			for _, d := range report.Diagnostics {
				if d.ID == "ARCH101" &&
					(d.File == tc.source || tc.name == "helper bypass" && d.File == "internal/helper/helper.go") {
					found = true
				}
			}

			if found != tc.bad {
				t.Fatalf(
					"boundary violation=%v want %v: %+v",
					found,
					tc.bad,
					report.Diagnostics,
				)
			}
		})
	}
}

func TestArchitectureLegacyContinuesAndIsReadOnly(t *testing.T) {
	root := t.TempDir()

	manifest := []byte("schema_version = 1\n")
	if err := os.WriteFile(
		filepath.Join(root, "pluto.toml"),
		manifest,
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	report := InspectArchitecture(root)

	ids := map[string]bool{}
	for _, d := range report.Diagnostics {
		ids[d.ID] = true
	}

	if !ids["ARCH002"] || !ids["ARCH003"] || !ids["ARCH007"] || report.Passed {
		t.Fatalf("missing independent blockers: %+v", report)
	}

	after, err := os.ReadFile(filepath.Join(root, "pluto.toml"))
	if err != nil || string(after) != string(manifest) {
		t.Fatal("inspection modified legacy manifest")
	}
}

func TestArchitectureBuildVariantsAndUnresolvedImport(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(root, "go.mod"),
		[]byte("module example.test/app\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "variant.go"),
		[]byte(
			"//go:build imaginary\n\npackage app\nimport _ \"example.test/app/missing\"\n",
		),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	report := InspectArchitecture(root)
	found := false

	for _, d := range report.Diagnostics {
		if d.ID == "ARCH006" && d.File == "variant.go" && d.Line == 4 {
			found = true
		}
	}

	if !found ||
		!strings.Contains(
			strings.Join(report.Unassessed, " "),
			"build constraints not evaluated",
		) {
		t.Fatalf("missing variant/import accounting: %+v", report)
	}
}

func TestArchitectureInvocationRequiresCommand(t *testing.T) {
	for _, s := range []string{"# pluto vet", "echo pluto vet", "printf 'pluto architecture check'"} {
		if architectureInvocation(s) {
			t.Fatalf("accepted inert invocation: %s", s)
		}
	}

	for _, s := range []string{"pluto vet", "exec pluto vet", "pluto architecture check --format json"} {
		if !architectureInvocation(s) {
			t.Fatalf("missed invocation: %s", s)
		}
	}
}

func TestArchitectureSharedRoleGapIsExplicit(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"access", "config"} {
		dir := filepath.Join(root, "internal", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(
			filepath.Join(dir, "shared.go"),
			[]byte("package "+name+"\nfunc Shared() {}\n"),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
	}

	report := InspectArchitecture(root)
	for _, name := range []string{"access", "config"} {
		if !strings.Contains(
			strings.Join(report.Unassessed, "\n"),
			"package: internal/"+name,
		) {
			t.Fatalf("missing shared role gap: %+v", report.Unassessed)
		}
	}
}
