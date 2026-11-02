package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCandidateModuleBindingsMatchDeclaredProfile(t *testing.T) {
	profile := boundaryProfileFixture(t)
	m := Manifest{Boundary: profile}

	metadata, err := Encode(m)
	if err != nil {
		t.Fatal(err)
	}

	module, err := ConfigureBoundaryModules(
		profile,
		false,
		[]byte("module example.test/candidate\n\ngo 1.25.5\n"),
	)
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	write := func(source []byte) {
		t.Helper()

		if err := os.WriteFile(
			filepath.Join(root, "go.mod"),
			source,
			0o644,
		); err != nil {
			t.Fatal(err)
		}
	}
	write(module)

	if diagnostics := inspectCandidateModules(
		root,
		metadata,
	); len(
		diagnostics,
	) != 0 {
		t.Fatal(diagnostics)
	}

	write(
		[]byte(
			strings.Replace(
				string(module),
				profile.AuthSource,
				filepath.Join(root, "different-auth"),
				1,
			),
		),
	)

	diagnostics := inspectCandidateModules(root, metadata)
	if len(diagnostics) != 1 || diagnostics[0].ID != "ARCH607" ||
		diagnostics[0].File != "go.mod" {
		t.Fatalf(
			"replacement contradicted declared authority profile: %+v",
			diagnostics,
		)
	}

	if err := os.Remove(filepath.Join(root, "go.mod")); err != nil {
		t.Fatal(err)
	}

	if diagnostics := inspectCandidateModules(
		root,
		metadata,
	); len(diagnostics) != 1 ||
		diagnostics[0].File != "pluto.toml" {
		t.Fatal(diagnostics)
	}
}
